package httpapi_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"forgeflow/internal/auth"
	"forgeflow/internal/controlplane"
	"forgeflow/internal/domain"
)

func TestPublicRegistrationLoginAndOwnerIsolation(t *testing.T) {
	f := newFixtureWithOptions(t, auth.NewMemoryLimiter(100, time.Minute), fixtureOptions{
		registrationLimiter: auth.NewMemoryLimiter(100, time.Minute),
	})
	response := postRegistration(t, f, `{"email":"  New.User@Example.COM  ","password":"a strong passphrase for signup"}`, "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", response.StatusCode, read(response))
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "forgeflow_session" {
			t.Fatal("registration created a session without login")
		}
	}
	var registered auth.User
	decodeResponse(t, response, &registered)
	if registered.Email != "new.user@example.com" || registered.Role != auth.RoleOperator || registered.Status != "active" {
		t.Fatalf("unexpected account: %+v", registered)
	}
	var storedHash string
	if err := f.db.QueryRow(`SELECT password_hash FROM users WHERE id=$1`, registered.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == "a strong passphrase for signup" || !strings.HasPrefix(storedHash, "$argon2id$") {
		t.Fatal("registered password was not stored as Argon2id")
	}
	first := f.login(t, "NEW.USER@example.com", "a strong passphrase for signup", "")
	response = f.request(t, first, http.MethodPost, "/api/v1/repositories", `{"name":"my demo","localPath":"."}`, true, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("operator repository status=%d body=%s", response.StatusCode, read(response))
	}
	var repo controlplane.Repository
	decodeResponse(t, response, &repo)
	if repo.OwnerID != first.userID {
		t.Fatal("registered account did not own its repository")
	}
	response = f.request(t, first, http.MethodPost, "/api/v1/runs", `{"repositoryId":"`+repo.ID+`","task":"try the mock planner"}`, true, map[string]string{"Idempotency-Key": domain.NewID()})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("operator run status=%d body=%s", response.StatusCode, read(response))
	}
	_ = read(response)

	response = postRegistration(t, f, `{"email":"second@example.com","password":"another strong signup password"}`, "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("second register status=%d body=%s", response.StatusCode, read(response))
	}
	_ = read(response)
	second := f.login(t, "second@example.com", "another strong signup password", "")
	response = f.request(t, second, http.MethodGet, "/api/v1/repositories/"+repo.ID, "", false, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner repository status=%d body=%s", response.StatusCode, read(response))
	}
	_ = read(response)
	response = f.request(t, second, http.MethodPost, "/api/v1/runs", `{"repositoryId":"`+repo.ID+`","task":"cross-owner run"}`, true, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-owner run status=%d body=%s", response.StatusCode, read(response))
	}
	_ = read(response)
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM users WHERE role='admin'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin count=%d err=%v", count, err)
	}
}

func TestPublicRegistrationRejectsInvalidDuplicateAndCrossOrigin(t *testing.T) {
	f := newFixtureWithOptions(t, auth.NewMemoryLimiter(100, time.Minute), fixtureOptions{
		registrationLimiter: auth.NewMemoryLimiter(100, time.Minute),
	})
	checks := []struct {
		body, origin string
		status       int
	}{
		{`{"email":"bad","password":"a strong passphrase for signup"}`, "", http.StatusBadRequest},
		{`{"email":"new@example.com","password":"short"}`, "", http.StatusBadRequest},
		{`{"email":"new@example.com","password":"a strong passphrase for signup","role":"admin"}`, "", http.StatusBadRequest},
		{`{"email":"new@example.com","password":"a strong passphrase for signup"}`, "https://attacker.example", http.StatusForbidden},
		{`{"email":"ADMIN@example.com","password":"a strong passphrase for signup"}`, "", http.StatusConflict},
	}
	for _, check := range checks {
		response := postRegistration(t, f, check.body, check.origin)
		if response.StatusCode != check.status {
			t.Fatalf("body=%s origin=%s status=%d want=%d response=%s", check.body, check.origin, response.StatusCode, check.status, read(response))
		}
		_ = read(response)
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid registrations changed user count=%d err=%v", count, err)
	}
}

func TestPublicRegistrationRateLimit(t *testing.T) {
	f := newFixtureWithOptions(t, auth.NewMemoryLimiter(100, time.Minute), fixtureOptions{
		registrationLimiter: auth.NewMemoryLimiter(1, time.Hour),
	})
	first := postRegistration(t, f, `{"email":"bad","password":"short"}`, "")
	if first.StatusCode != http.StatusBadRequest {
		t.Fatalf("first status=%d body=%s", first.StatusCode, read(first))
	}
	_ = read(first)
	second := postRegistration(t, f, `{"email":"new@example.com","password":"a strong passphrase for signup"}`, "")
	if second.StatusCode != http.StatusTooManyRequests || second.Header.Get("Retry-After") == "" {
		t.Fatalf("second status=%d retry=%q body=%s", second.StatusCode, second.Header.Get("Retry-After"), read(second))
	}
	_ = read(second)
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM users WHERE normalized_email='new@example.com'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rate-limited request created an account")
	}
}

func postRegistration(t *testing.T, f *apiFixture, body, origin string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/register", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
