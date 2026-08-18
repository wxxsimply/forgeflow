package auth

import (
	"testing"
	"time"
)

func TestArgon2idPasswordRoundTripAndUpgradeDetection(t *testing.T) {
	params := PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := HashPassword("correct horse battery staple", params)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}
	valid, err = VerifyPassword(hash, "incorrect-password")
	if err != nil || valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}
	upgraded := params
	upgraded.Iterations = 2
	if !NeedsPasswordRehash(hash, upgraded) || NeedsPasswordRehash(hash, params) {
		t.Fatal("password parameter upgrade was not detected")
	}
}

func TestMemoryLimiterDoesNotPermanentlyLock(t *testing.T) {
	limiter := NewMemoryLimiter(2, time.Minute)
	now := time.Unix(100, 0)
	first := limiter.Allow("account:a", now)
	second := limiter.Allow("account:a", now)
	if !first.Allowed || !second.Allowed {
		t.Fatal("initial attempts denied")
	}
	blocked := limiter.Allow("account:a", now)
	if blocked.Allowed || blocked.RetryAfter <= 0 {
		t.Fatalf("blocked=%+v", blocked)
	}
	if !limiter.Allow("account:a", now.Add(time.Minute)).Allowed {
		t.Fatal("limiter did not recover after its window")
	}
}

func TestRoleCapabilities(t *testing.T) {
	for _, role := range []Role{RoleAdmin, RoleOperator, RoleViewer} {
		if !role.Valid() {
			t.Fatalf("role %q is invalid", role)
		}
	}
	if !RoleAdmin.CanWriteRuns() || !RoleAdmin.CanApprove() || !RoleOperator.CanWriteRuns() || !RoleOperator.CanApprove() {
		t.Fatal("admin/operator capabilities are incomplete")
	}
	if RoleViewer.CanWriteRuns() || RoleViewer.CanApprove() {
		t.Fatal("viewer received a mutation capability")
	}
}
