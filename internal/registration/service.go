package registration

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"forgeflow/internal/apperror"
	"forgeflow/internal/auth"
)

const CodeTTL = 10 * time.Minute
const ResendInterval = time.Minute

var codePattern = regexp.MustCompile(`^[0-9]{8}$`)

type Store interface {
	Issue(context.Context, string, []byte, time.Time, time.Time, time.Time) (bool, error)
	Consume(context.Context, string, []byte, time.Time) (bool, error)
	Revoke(context.Context, string, []byte) error
}

type Sender interface {
	Send(context.Context, string, string) error
}

type AccountCreator interface {
	Register(context.Context, string, string) (auth.User, error)
}

type Service struct {
	accounts AccountCreator
	store    Store
	sender   Sender
	key      []byte
	now      func() time.Time
}

func New(accounts AccountCreator, store Store, sender Sender, key []byte) (*Service, error) {
	if accounts == nil || store == nil || sender == nil || len(key) != 32 {
		return nil, fmt.Errorf("registration requires accounts, code store, sender, and a 32-byte key")
	}
	return &Service{accounts: accounts, store: store, sender: sender, key: append([]byte(nil), key...), now: time.Now}, nil
}

func (s *Service) RequestCode(ctx context.Context, email string) error {
	email = auth.NormalizeEmail(email)
	if !validEmail(email) {
		return apperror.New(apperror.CodeValidation, "email is invalid")
	}
	random, err := rand.Int(rand.Reader, big.NewInt(100_000_000))
	if err != nil {
		return apperror.New(apperror.CodeTransient, "registration code is unavailable")
	}
	code := fmt.Sprintf("%08d", random.Int64())
	now := s.now().UTC()
	hash := s.hash(email, code)
	issued, err := s.store.Issue(ctx, email, hash, now, now.Add(CodeTTL), now.Add(ResendInterval))
	if err != nil {
		return apperror.New(apperror.CodeTransient, "registration code is unavailable")
	}
	if !issued {
		return nil // Keep the response identical during the resend cooldown.
	}
	if err := s.sender.Send(ctx, email, code); err != nil {
		_ = s.store.Revoke(ctx, email, hash)
		return apperror.New(apperror.CodeTransient, "registration email could not be sent")
	}
	return nil
}

func (s *Service) Register(ctx context.Context, email, password, code string) (auth.User, error) {
	email = auth.NormalizeEmail(email)
	if !validEmail(email) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return auth.User{}, apperror.New(apperror.CodeValidation, "email or password is invalid")
	}
	if !codePattern.MatchString(code) {
		return auth.User{}, apperror.New(apperror.CodeValidation, "verification code is invalid or expired")
	}
	verified, err := s.store.Consume(ctx, email, s.hash(email, code), s.now().UTC())
	if err != nil {
		return auth.User{}, apperror.New(apperror.CodeTransient, "registration verification is unavailable")
	}
	if !verified {
		return auth.User{}, apperror.New(apperror.CodeValidation, "verification code is invalid or expired")
	}
	return s.accounts.Register(ctx, email, password)
}

func (s *Service) hash(email, code string) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(email + "\x00" + code))
	return mac.Sum(nil)
}

func validEmail(email string) bool {
	if len(email) < 3 || len(email) > 320 || strings.ContainsAny(email, " \t\r\n\x00") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && strings.Contains(email, "@")
}
