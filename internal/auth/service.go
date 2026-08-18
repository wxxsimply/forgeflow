package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
)

type Options struct {
	SessionTTL     time.Duration
	IdleTTL        time.Duration
	PasswordParams PasswordParams
	AccountLimiter Limiter
	SourceLimiter  Limiter
	Now            func() time.Time
}

type Service struct {
	store     Store
	options   Options
	dummyHash string
}

type LoginResult struct {
	Principal Principal `json:"principal"`
	Token     string    `json:"-"`
	CSRFToken string    `json:"csrfToken"`
}

func NewService(store Store, options Options) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("auth store is required")
	}
	if options.SessionTTL <= 0 {
		options.SessionTTL = 24 * time.Hour
	}
	if options.IdleTTL <= 0 {
		options.IdleTTL = 30 * time.Minute
	}
	if options.PasswordParams.Memory == 0 {
		options.PasswordParams = DefaultPasswordParams()
	}
	if options.AccountLimiter == nil {
		options.AccountLimiter = NewMemoryLimiter(5, time.Minute)
	}
	if options.SourceLimiter == nil {
		options.SourceLimiter = NewMemoryLimiter(20, time.Minute)
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	dummy, err := HashPassword("forgeflow-invalid-password", options.PasswordParams)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, options: options, dummyHash: dummy}, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context, email, password string) (User, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return User{}, err
	}
	if count != 0 {
		return User{}, apperror.New(apperror.CodeConflict, "bootstrap is only allowed before the first user exists")
	}
	normalized := NormalizeEmail(email)
	if !validEmail(normalized) {
		return User{}, apperror.New(apperror.CodeValidation, "email is invalid")
	}
	hash, err := HashPassword(password, s.options.PasswordParams)
	if err != nil {
		return User{}, apperror.New(apperror.CodeValidation, err.Error())
	}
	user := User{ID: domain.NewID(), Email: normalized, Role: RoleAdmin, Status: "active", CreatedAt: s.options.Now()}
	if err := s.store.CreateUser(ctx, UserCredential{User: user, PasswordHash: hash}); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password, sourceIP, userAgent string, oldToken string) (LoginResult, time.Duration, error) {
	now := s.options.Now()
	normalized := NormalizeEmail(email)
	if len(normalized) > 320 || len(password) == 0 || len(password) > 1024 {
		return LoginResult{}, 0, apperror.New(apperror.CodeUnauthorized, "email or password is incorrect")
	}
	accountKey, sourceKey := "account:"+normalized, "source:"+sourceIP
	accountLimit, sourceLimit := s.options.AccountLimiter.Allow(accountKey, now), s.options.SourceLimiter.Allow(sourceKey, now)
	if !accountLimit.Allowed || !sourceLimit.Allowed {
		retry := accountLimit.RetryAfter
		if sourceLimit.RetryAfter > retry {
			retry = sourceLimit.RetryAfter
		}
		return LoginResult{}, retry, apperror.New(apperror.CodeRateLimited, "too many login attempts; try again later")
	}
	credential, findErr := s.store.FindUserByEmail(ctx, normalized)
	hash := s.dummyHash
	if findErr == nil {
		hash = credential.PasswordHash
	}
	valid, verifyErr := VerifyPassword(hash, password)
	if verifyErr != nil || findErr != nil || !valid || credential.Status != "active" {
		return LoginResult{}, 0, apperror.New(apperror.CodeUnauthorized, "email or password is incorrect")
	}
	if NeedsPasswordRehash(credential.PasswordHash, s.options.PasswordParams) {
		if upgraded, hashErr := HashPassword(password, s.options.PasswordParams); hashErr == nil {
			_ = s.store.UpdatePasswordHash(ctx, credential.ID, upgraded)
		}
	}
	if oldToken != "" {
		_ = s.store.RevokeToken(ctx, digest(oldToken))
	}
	token, tokenHash, err := newSecret()
	if err != nil {
		return LoginResult{}, 0, err
	}
	csrf, csrfHash, err := newSecret()
	if err != nil {
		return LoginResult{}, 0, err
	}
	session := Session{ID: domain.NewID(), UserID: credential.ID, TokenHash: tokenHash, CSRFHash: csrfHash,
		SourceIP: sourceIP, UserAgent: userAgent, CreatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(s.options.SessionTTL), IdleExpiresAt: now.Add(s.options.IdleTTL)}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return LoginResult{}, 0, err
	}
	s.options.AccountLimiter.Reset(accountKey)
	return LoginResult{Principal: Principal{User: credential.User, Session: session}, Token: token, CSRFToken: csrf}, 0, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, apperror.New(apperror.CodeUnauthorized, "authentication required")
	}
	session, err := s.store.FindSessionByTokenHash(ctx, digest(token))
	if err != nil {
		return Principal{}, apperror.New(apperror.CodeUnauthorized, "authentication required")
	}
	now := s.options.Now()
	if session.RevokedAt != nil || !now.Before(session.ExpiresAt) || !now.Before(session.IdleExpiresAt) {
		_ = s.store.RevokeSession(ctx, session.UserID, session.ID)
		return Principal{}, apperror.New(apperror.CodeUnauthorized, "authentication required")
	}
	user, err := s.store.FindUserByID(ctx, session.UserID)
	if err != nil || user.Status != "active" {
		return Principal{}, apperror.New(apperror.CodeUnauthorized, "authentication required")
	}
	if now.Sub(session.LastSeenAt) >= time.Minute {
		session.LastSeenAt = now
		session.IdleExpiresAt = now.Add(s.options.IdleTTL)
		_ = s.store.TouchSession(ctx, session.ID, session.LastSeenAt, session.IdleExpiresAt)
	}
	return Principal{User: user.User, Session: session}, nil
}

func (s *Service) ValidateCSRF(principal Principal, headerToken, cookieToken string) error {
	if headerToken == "" || cookieToken == "" || subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookieToken)) != 1 || subtle.ConstantTimeCompare(digest(headerToken), principal.Session.CSRFHash) != 1 {
		return apperror.New(apperror.CodeForbidden, "CSRF validation failed")
	}
	return nil
}

func (s *Service) Sessions(ctx context.Context, userID string) ([]Session, error) {
	return s.store.ListSessions(ctx, userID)
}
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	return s.store.RevokeSession(ctx, userID, sessionID)
}
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.RevokeToken(ctx, digest(token))
}

func newSecret() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, digest(token), nil
}
func digest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func validEmail(value string) bool {
	if len(value) < 3 || len(value) > 320 {
		return false
	}
	for i, c := range value {
		if c == '@' && i > 0 && i < len(value)-1 {
			return true
		}
	}
	return false
}
