package fixture

import "testing"

func TestPublicSmoke(t *testing.T) {
	if !IsTransitionAllowed(StatusPending, StatusRunning) {
		t.Fatal("pending runs must be startable")
	}
	if !CheckCSRF("POST /runs", "public-test-token") {
		t.Fatal("valid CSRF token rejected")
	}
	if !ShouldRetry(ProviderError{Timeout: true}) || !ShouldRetry(ProviderError{Status: 429}) {
		t.Fatal("provider retry baseline is invalid")
	}
}
