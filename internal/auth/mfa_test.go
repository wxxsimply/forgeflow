package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"forgeflow/internal/apperror"
)

func TestTOTPMatchesRFC6238Vector(t *testing.T) {
	secret := []byte("12345678901234567890")
	if got := totpCode(secret, 59/30, 8); got != "94287082" {
		t.Fatalf("TOTP=%s want 94287082", got)
	}
}

func TestAdministratorMFAEnrollmentLoginReplayAndRecovery(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0).UTC()
	key := []byte("0123456789abcdef0123456789abcdef")
	password := "correct horse battery staple"
	testPasswordParams := PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := HashPassword(password, testPasswordParams)
	if err != nil {
		t.Fatal(err)
	}
	store := newMFAStore(UserCredential{User: User{ID: "00000000-0000-4000-8000-000000000001", Email: "admin@example.com", Role: RoleAdmin, Status: "active", CreatedAt: now}, PasswordHash: hash})
	current := Session{ID: "00000000-0000-4000-8000-000000000002", UserID: store.user.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour)}
	other := Session{ID: "00000000-0000-4000-8000-000000000003", UserID: store.user.ID, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour)}
	store.sessions[current.ID], store.sessions[other.ID] = current, other
	service, err := NewService(store, Options{PasswordParams: testPasswordParams, SessionTTL: time.Hour, IdleTTL: time.Hour, AdminMFARequired: true, MFAEncryptionKey: key, AccountLimiter: NewMemoryLimiter(100, time.Minute), SourceLimiter: NewMemoryLimiter(100, time.Minute), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.BeginMFAEnrollment(ctx, store.user.ID, "wrong password"); !IsUnauthorized(err) {
		t.Fatalf("wrong password error=%v", err)
	}
	enrollment, err := service.BeginMFAEnrollment(ctx, store.user.ID, password)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := mfaBase32.DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(store.user.MFAPendingSecretCiphertext, secret) {
		t.Fatal("pending MFA secret was stored in plaintext")
	}
	code := totpCode(secret, now.Unix()/totpPeriodSeconds, 6)
	confirmation, err := service.ConfirmMFAEnrollment(ctx, store.user.ID, current.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(confirmation.RecoveryCodes) != recoveryCodeCount || store.user.MFAEnabledAt == nil || store.sessions[current.ID].MFAVerifiedAt == nil || store.sessions[other.ID].RevokedAt == nil {
		t.Fatalf("MFA confirmation did not elevate current and revoke other sessions: %+v", confirmation)
	}
	if bytes.Contains(store.user.MFASecretCiphertext, secret) {
		t.Fatal("active MFA secret was stored in plaintext")
	}

	if _, _, err := service.Login(ctx, store.user.Email, password, code, "127.0.0.1", "test", ""); !IsUnauthorized(err) {
		t.Fatalf("replayed TOTP error=%v", err)
	}
	now = now.Add(30 * time.Second)
	nextCode := totpCode(secret, now.Unix()/totpPeriodSeconds, 6)
	if _, _, err := service.Login(ctx, store.user.Email, password, nextCode, "127.0.0.1", "test", ""); err != nil {
		t.Fatalf("next TOTP login: %v", err)
	}
	if _, _, err := service.Login(ctx, store.user.Email, password, confirmation.RecoveryCodes[0], "127.0.0.2", "test", ""); err != nil {
		t.Fatalf("recovery login: %v", err)
	}
	if _, _, err := service.Login(ctx, store.user.Email, password, confirmation.RecoveryCodes[0], "127.0.0.3", "test", ""); !IsUnauthorized(err) {
		t.Fatalf("reused recovery code error=%v", err)
	}
}

func TestMFAEncryptionBindsCiphertextToUser(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	ciphertext, err := sealMFASecret(key, "user-a", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openMFASecret(key, "user-b", ciphertext); err == nil {
		t.Fatal("ciphertext decrypted for another user")
	}
}

func IsUnauthorized(err error) bool { return apperror.IsCode(err, apperror.CodeUnauthorized) }

type mfaStore struct {
	user     UserCredential
	sessions map[string]Session
}

func newMFAStore(user UserCredential) *mfaStore {
	return &mfaStore{user: user, sessions: map[string]Session{}}
}
func (s *mfaStore) CountUsers(context.Context) (int, error) { return 1, nil }
func (s *mfaStore) CreateUser(context.Context, UserCredential) error {
	return errors.New("not implemented")
}
func (s *mfaStore) FindUserByEmail(_ context.Context, email string) (UserCredential, error) {
	if NormalizeEmail(email) != NormalizeEmail(s.user.Email) {
		return UserCredential{}, ErrNotFound
	}
	return s.user, nil
}
func (s *mfaStore) FindUserByID(_ context.Context, id string) (UserCredential, error) {
	if id != s.user.ID {
		return UserCredential{}, ErrNotFound
	}
	return s.user, nil
}
func (s *mfaStore) UpdatePasswordHash(_ context.Context, _ string, hash string) error {
	s.user.PasswordHash = hash
	return nil
}
func (s *mfaStore) SetMFAPending(_ context.Context, id string, value []byte, expires time.Time) error {
	if id != s.user.ID || s.user.MFAEnabledAt != nil {
		return ErrNotFound
	}
	s.user.MFAPendingSecretCiphertext = append([]byte(nil), value...)
	s.user.MFAPendingExpiresAt = &expires
	return nil
}
func (s *mfaStore) EnableMFA(_ context.Context, userID, sessionID string, expected []byte, hashes []string, step int64, now time.Time) error {
	if userID != s.user.ID || !bytes.Equal(expected, s.user.MFAPendingSecretCiphertext) || s.user.MFAPendingExpiresAt == nil || !now.Before(*s.user.MFAPendingExpiresAt) {
		return ErrNotFound
	}
	s.user.MFASecretCiphertext = append([]byte(nil), s.user.MFAPendingSecretCiphertext...)
	s.user.MFAPendingSecretCiphertext = nil
	s.user.MFAPendingExpiresAt = nil
	s.user.MFAEnabledAt = &now
	s.user.MFARecoveryCodeHashes = append([]string(nil), hashes...)
	s.user.MFALastUsedStep = step
	for id, session := range s.sessions {
		if id == sessionID {
			session.MFAVerifiedAt = &now
		} else {
			session.RevokedAt = &now
		}
		s.sessions[id] = session
	}
	return nil
}
func (s *mfaStore) UseMFATimestep(_ context.Context, id string, step int64) (bool, error) {
	if id != s.user.ID || step <= s.user.MFALastUsedStep {
		return false, nil
	}
	s.user.MFALastUsedStep = step
	return true, nil
}
func (s *mfaStore) ConsumeMFARecoveryCode(_ context.Context, id, hash string) (bool, error) {
	if id != s.user.ID {
		return false, nil
	}
	for index, candidate := range s.user.MFARecoveryCodeHashes {
		if candidate == hash {
			s.user.MFARecoveryCodeHashes = append(s.user.MFARecoveryCodeHashes[:index], s.user.MFARecoveryCodeHashes[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (s *mfaStore) CreateSession(_ context.Context, session Session) error {
	s.sessions[session.ID] = session
	return nil
}
func (s *mfaStore) FindSessionByTokenHash(context.Context, []byte) (Session, error) {
	return Session{}, ErrNotFound
}
func (s *mfaStore) ListSessions(context.Context, string) ([]Session, error)          { return nil, nil }
func (s *mfaStore) TouchSession(context.Context, string, time.Time, time.Time) error { return nil }
func (s *mfaStore) RevokeSession(context.Context, string, string) error              { return nil }
func (s *mfaStore) RevokeToken(context.Context, []byte) error                        { return nil }
