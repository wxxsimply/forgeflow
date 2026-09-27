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
	code := requestCode(t, f, "  New.User@Example.COM  ", "")
	response := postRegistration(t, f, `{"email":"  New.User@Example.COM  ","password":"a strong passphrase for signup","code":"`+code+`"}`, "")
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

	code = requestCode(t, f, "second@example.com", "")
	response = postRegistration(t, f, `{"email":"second@example.com","password":"another strong signup password","code":"`+code+`"}`, "")
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
		allowedOrigins:      []string{"https://forgeflow.example"},
	})
	checks := []struct {
		body, origin string
		status       int
	}{
		{`{"email":"bad","password":"a strong passphrase for signup"}`, "", http.StatusBadRequest},
		{`{"email":"new@example.com","password":"short"}`, "", http.StatusBadRequest},
		{`{"email":"new@example.com","password":"a strong passphrase for signup","role":"admin"}`, "", http.StatusBadRequest},
		{`{"email":"new@example.com","password":"a strong passphrase for signup"}`, "https://attacker.example", http.StatusForbidden},
		{`{"email":"ADMIN@example.com","password":"a strong passphrase for signup"}`, "", http.StatusBadRequest},
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
	blockedRequest, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/register/code", bytes.NewBufferString(`{"email":"blocked@example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	blockedRequest.Header.Set("Content-Type", "application/json")
	blockedRequest.Header.Set("Origin", "https://attacker.example")
	blockedResponse, err := http.DefaultClient.Do(blockedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if blockedResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin code status=%d", blockedResponse.StatusCode)
	}
	_ = read(blockedResponse)
	if f.codeSender.code("blocked@example.com") != "" {
		t.Fatal("cross-origin request sent a code")
	}
	code := requestCode(t, f, "new@example.com", "https://forgeflow.example")
	var firstHash []byte
	if err := f.db.QueryRow(`SELECT code_hash FROM registration_codes WHERE normalized_email='new@example.com'`).Scan(&firstHash); err != nil {
		t.Fatal(err)
	}
	if resent := requestCode(t, f, "new@example.com", "https://forgeflow.example"); resent != code {
		t.Fatal("cooldown changed the delivered code")
	}
	var secondHash []byte
	if err := f.db.QueryRow(`SELECT code_hash FROM registration_codes WHERE normalized_email='new@example.com'`).Scan(&secondHash); err != nil || !bytes.Equal(firstHash, secondHash) {
		t.Fatalf("cooldown replaced the stored code: %v", err)
	}
	allowed := postRegistration(t, f, `{"email":"new@example.com","password":"a strong passphrase for signup","code":"`+code+`"}`, "https://forgeflow.example")
	if allowed.StatusCode != http.StatusCreated {
		t.Fatalf("allowed origin registration status=%d body=%s", allowed.StatusCode, read(allowed))
	}
	_ = read(allowed)
	code = requestCode(t, f, "ADMIN@example.com", "https://forgeflow.example")
	duplicate := postRegistration(t, f, `{"email":"ADMIN@example.com","password":"a strong passphrase for signup","code":"`+code+`"}`, "https://forgeflow.example")
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.StatusCode, read(duplicate))
	}
	_ = read(duplicate)
}

func TestRegistrationCodeCannotBeSkippedReusedOrGuessed(t *testing.T) {
	f := newFixtureWithOptions(t, auth.NewMemoryLimiter(100, time.Minute), fixtureOptions{registrationLimiter: auth.NewMemoryLimiter(100, time.Minute)})
	const email = "verified@example.com"
	const password = "a strong passphrase for signup"
	code := requestCode(t, f, email, "")
	if code == "" || len(code) != 8 {
		t.Fatal("no eight-digit code was sent")
	}
	wrongCode := "0" + code[1:]
	if code[0] == '0' {
		wrongCode = "1" + code[1:]
	}
	without := postRegistration(t, f, `{"email":"`+email+`","password":"`+password+`"}`, "")
	if without.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing code status=%d", without.StatusCode)
	}
	_ = read(without)
	wrong := postRegistration(t, f, `{"email":"`+email+`","password":"`+password+`","code":"`+wrongCode+`"}`, "")
	if wrong.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong code status=%d", wrong.StatusCode)
	}
	_ = read(wrong)
	good := postRegistration(t, f, `{"email":"`+email+`","password":"`+password+`","code":"`+code+`"}`, "")
	if good.StatusCode != http.StatusCreated {
		t.Fatalf("verified status=%d body=%s", good.StatusCode, read(good))
	}
	_ = read(good)
	reused := postRegistration(t, f, `{"email":"`+email+`","password":"`+password+`","code":"`+code+`"}`, "")
	if reused.StatusCode != http.StatusBadRequest {
		t.Fatalf("reused code status=%d", reused.StatusCode)
	}
	_ = read(reused)
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM users WHERE normalized_email=$1`, email).Scan(&count); err != nil || count != 1 {
		t.Fatalf("user count=%d err=%v", count, err)
	}
}

func TestRegistrationCodeExpiresAndLocksAfterFiveWrongAttempts(t *testing.T) {
	f := newFixtureWithOptions(t, auth.NewMemoryLimiter(100, time.Minute), fixtureOptions{registrationLimiter: auth.NewMemoryLimiter(100, time.Minute)})
	code := requestCode(t, f, "expired@example.com", "")
	if _, err := f.db.Exec(`UPDATE registration_codes SET resend_after=now()-interval '2 seconds', expires_at=now()-interval '1 second' WHERE normalized_email='expired@example.com'`); err != nil {
		t.Fatal(err)
	}
	response := postRegistration(t, f, `{"email":"expired@example.com","password":"a strong passphrase for signup","code":"`+code+`"}`, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired code status=%d", response.StatusCode)
	}
	_ = read(response)
	code = requestCode(t, f, "locked@example.com", "")
	wrongCode := "0" + code[1:]
	if code[0] == '0' {
		wrongCode = "1" + code[1:]
	}
	for i := 0; i < 5; i++ {
		response = postRegistration(t, f, `{"email":"locked@example.com","password":"a strong passphrase for signup","code":"`+wrongCode+`"}`, "")
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("wrong attempt %d status=%d", i, response.StatusCode)
		}
		_ = read(response)
	}
	response = postRegistration(t, f, `{"email":"locked@example.com","password":"a strong passphrase for signup","code":"`+code+`"}`, "")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("locked code status=%d", response.StatusCode)
	}
	_ = read(response)
}

func TestRegistrationFailsClosedWithoutMailConfiguration(t *testing.T) {
	f := newFixtureWithOptions(t, auth.NewMemoryLimiter(100, time.Minute), fixtureOptions{disableRegistration: true})
	for _, path := range []string{"/api/v1/auth/register/code", "/api/v1/auth/register"} {
		req, err := http.NewRequest(http.MethodPost, f.server.URL+path, bytes.NewBufferString(`{"email":"new@example.com","password":"a strong passphrase for signup","code":"12345678"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("unconfigured %s status=%d", path, response.StatusCode)
		}
		_ = read(response)
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected user count=%d err=%v", count, err)
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

func requestCode(t *testing.T, f *apiFixture, email, origin string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/v1/auth/register/code", bytes.NewBufferString(`{"email":"`+email+`"}`))
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
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("request code status=%d body=%s", response.StatusCode, read(response))
	}
	_ = read(response)
	return f.codeSender.code(strings.ToLower(strings.TrimSpace(email)))
}
