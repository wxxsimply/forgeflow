package registration

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/auth"
)

type memoryCodes struct {
	email       string
	hash        []byte
	expires     time.Time
	resendAfter time.Time
	issues      int
}

func (s *memoryCodes) Issue(_ context.Context, email string, hash []byte, now, expires, resendAfter time.Time) (bool, error) {
	if s.email == email && now.Before(s.resendAfter) {
		return false, nil
	}
	s.email, s.hash, s.expires, s.resendAfter = email, append([]byte(nil), hash...), expires, resendAfter
	s.issues++
	return true, nil
}
func (s *memoryCodes) Consume(_ context.Context, email string, hash []byte, now time.Time) (bool, error) {
	if s.email != email || !now.Before(s.expires) || !bytes.Equal(s.hash, hash) {
		return false, nil
	}
	s.email, s.hash = "", nil
	return true, nil
}
func (s *memoryCodes) Revoke(_ context.Context, email string, hash []byte) error {
	if s.email == email && bytes.Equal(s.hash, hash) {
		s.email, s.hash = "", nil
	}
	return nil
}

type memorySender struct {
	email, code string
	calls       int
	err         error
}

func (s *memorySender) Send(_ context.Context, email, code string) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	s.email, s.code = email, code
	return nil
}

type memoryAccounts struct{ calls int }

func (a *memoryAccounts) Register(_ context.Context, email, _ string) (auth.User, error) {
	a.calls++
	return auth.User{Email: email}, nil
}

func TestRequestAndRegisterRequireOneTimeCode(t *testing.T) {
	store, sender, accounts := &memoryCodes{}, &memorySender{}, &memoryAccounts{}
	service, err := New(accounts, store, sender, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if err := service.RequestCode(context.Background(), " User@Example.com "); err != nil {
		t.Fatal(err)
	}
	if sender.email != "user@example.com" || len(sender.code) != 8 || bytes.Contains(store.hash, []byte(sender.code)) {
		t.Fatal("code was not normalized, eight digits, or stored only as a hash")
	}
	if err := service.RequestCode(context.Background(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || store.issues != 1 {
		t.Fatal("resend cooldown did not suppress a duplicate email")
	}
	if _, err := service.Register(context.Background(), sender.email, "a strong passphrase for signup", "0000000x"); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("malformed code: %v", err)
	}
	wrongCode := "0" + sender.code[1:]
	if sender.code[0] == '0' {
		wrongCode = "1" + sender.code[1:]
	}
	if _, err := service.Register(context.Background(), sender.email, "a strong passphrase for signup", wrongCode); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("wrong code: %v", err)
	}
	if accounts.calls != 0 {
		t.Fatal("unverified account was created")
	}
	user, err := service.Register(context.Background(), "USER@example.com", "a strong passphrase for signup", sender.code)
	if err != nil || user.Email != sender.email {
		t.Fatalf("verified account: %+v %v", user, err)
	}
	if _, err := service.Register(context.Background(), sender.email, "a strong passphrase for signup", sender.code); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("reused code: %v", err)
	}
	if accounts.calls != 1 {
		t.Fatalf("accounts created=%d", accounts.calls)
	}
}

func TestCodeExpiryAndDeliveryFailure(t *testing.T) {
	store, sender, accounts := &memoryCodes{}, &memorySender{}, &memoryAccounts{}
	service, err := New(accounts, store, sender, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	sender.err = errors.New("smtp offline")
	if err := service.RequestCode(context.Background(), "user@example.com"); !apperror.IsCode(err, apperror.CodeTransient) {
		t.Fatalf("delivery failure: %v", err)
	}
	if store.email != "" {
		t.Fatal("failed delivery left a usable code")
	}
	sender.err = nil
	if err := service.RequestCode(context.Background(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(CodeTTL)
	if _, err := service.Register(context.Background(), "user@example.com", "a strong passphrase for signup", sender.code); !apperror.IsCode(err, apperror.CodeValidation) {
		t.Fatalf("expired code: %v", err)
	}
	if accounts.calls != 0 {
		t.Fatal("expired code created an account")
	}
}

func TestSenderRejectsUnsafeConfiguration(t *testing.T) {
	for _, input := range []struct {
		host                 string
		port                 int
		from, user, password string
	}{
		{"", 465, "sender@example.com", "sender@example.com", "secret"},
		{"smtp.example.com", 25, "sender@example.com", "sender@example.com", "secret"},
		{"smtp.example.com", 465, "sender@example.com\r\nBcc: victim@example.com", "sender@example.com", "secret"},
		{"smtp.example.com", 587, "sender@example.com", "sender@example.com", ""},
	} {
		if _, err := NewSMTPSender(input.host, input.port, input.from, input.user, input.password); err == nil {
			t.Fatalf("unsafe SMTP config accepted: %+v", input)
		}
	}
}
