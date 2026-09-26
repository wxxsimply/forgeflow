package auth

import (
	"context"
	"errors"
	"testing"

	"forgeflow/internal/apperror"
)

type registrationStore struct {
	Store
	count     int
	created   []UserCredential
	createErr error
}

func (s *registrationStore) CountUsers(context.Context) (int, error) { return s.count, nil }
func (s *registrationStore) CreateUser(_ context.Context, user UserCredential) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, user)
	return nil
}

func TestRegisterCreatesOnlyAnOrdinaryAccount(t *testing.T) {
	store := &registrationStore{count: 1}
	service, err := NewService(store, Options{PasswordParams: PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}})
	if err != nil {
		t.Fatal(err)
	}
	user, err := service.Register(context.Background(), "  New.User@Example.COM  ", "a strong passphrase for signup")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "new.user@example.com" || user.Role != RoleOperator || user.Status != "active" || user.ID == "" || len(store.created) != 1 {
		t.Fatalf("unexpected registration: %+v", user)
	}
	if store.created[0].PasswordHash == "a strong passphrase for signup" {
		t.Fatal("password was stored in plaintext")
	}
	valid, err := VerifyPassword(store.created[0].PasswordHash, "a strong passphrase for signup")
	if err != nil || !valid {
		t.Fatalf("password hash valid=%v err=%v", valid, err)
	}
}

func TestRegisterRejectsInvalidInputAndUnavailableSetup(t *testing.T) {
	store := &registrationStore{}
	service, err := NewService(store, Options{PasswordParams: PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(context.Background(), "new@example.com", "a strong passphrase for signup"); !apperror.IsCode(err, apperror.CodeConflict) {
		t.Fatalf("setup guard: %v", err)
	}
	store.count = 1
	for _, input := range []struct{ email, password string }{
		{"not-an-email", "a strong passphrase for signup"},
		{"a@example.com", "too short"},
		{"a@example.com", "中文密码很短"},
		{"bad address@example.com", "a strong passphrase for signup"},
	} {
		if _, err := service.Register(context.Background(), input.email, input.password); !apperror.IsCode(err, apperror.CodeValidation) {
			t.Fatalf("invalid input %q: %v", input.email, err)
		}
	}
	if len(store.created) != 0 {
		t.Fatal("invalid registration created a user")
	}
	store.createErr = ErrEmailExists
	if _, err := service.Register(context.Background(), "new@example.com", "a strong passphrase for signup"); !apperror.IsCode(err, apperror.CodeConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
	store.createErr = errors.New("database unavailable")
	if _, err := service.Register(context.Background(), "new@example.com", "a strong passphrase for signup"); apperror.IsCode(err, apperror.CodeConflict) {
		t.Fatalf("unrelated store error became a conflict: %v", err)
	}
}
