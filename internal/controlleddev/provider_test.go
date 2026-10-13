package controlleddev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forgeflow/internal/model"
)

type stubProvider struct{ calls int }

func (s *stubProvider) Name() string { return "deepseek" }
func (s *stubProvider) Generate(context.Context, model.Request) (model.Response, error) {
	s.calls++
	return model.Response{Status: "completed", OutputText: "{}"}, nil
}

func TestProviderChecksAccountBalanceBeforePaidCall(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		allowed    bool
	}{
		{"five yuan", `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"5.00"}]}`, 200, true},
		{"over five yuan", `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"5.01"}]}`, 200, false},
		{"usd balance", `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"1.00"}]}`, 200, false},
		{"unavailable", `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"1.00"}]}`, 200, false},
		{"invalid", `{`, 200, false},
		{"upstream error", `{}`, 503, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			balanceCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				balanceCalls++
				if r.Method != http.MethodGet || r.URL.Path != "/user/balance" || r.Header.Get("Authorization") != "Bearer secret-test-key" {
					t.Errorf("unexpected balance request")
				}
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			inner := &stubProvider{}
			provider, err := NewProvider(inner, "secret-test-key")
			if err != nil {
				t.Fatal(err)
			}
			provider.balanceURL = server.URL + "/user/balance"
			provider.client = server.Client()
			_, err = provider.Generate(context.Background(), model.Request{Model: "deepseek-flash", Input: "fixture", MaxOutputTokens: 100})
			if (err == nil) != test.allowed || inner.calls != boolInt(test.allowed) || balanceCalls != 1 {
				t.Fatalf("allowed=%v err=%v paid calls=%d balance calls=%d", test.allowed, err, inner.calls, balanceCalls)
			}
			if err != nil && strings.Contains(err.Error(), "secret-test-key") {
				t.Fatal("credential leaked in error")
			}
		})
	}
}

func TestProviderRejectsOversizedRequestsBeforeBalanceCall(t *testing.T) {
	inner := &stubProvider{}
	provider, err := NewProvider(inner, "secret-test-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []model.Request{
		{Model: "wrong", Input: "fixture", MaxOutputTokens: 100},
		{Model: "deepseek-flash", Input: strings.Repeat("x", MaxPromptBytes+1), MaxOutputTokens: 100},
		{Model: "deepseek-flash", Input: "fixture", MaxOutputTokens: MaxOutputTokens + 1},
	} {
		if _, err := provider.Generate(context.Background(), request); err == nil {
			t.Fatal("invalid request was accepted")
		}
	}
	if inner.calls != 0 {
		t.Fatal("invalid request reached the paid provider")
	}
}

func TestBalancePreflightDoesNotFollowRedirects(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	inner := &stubProvider{}
	provider, err := NewProvider(inner, "secret-test-key")
	if err != nil {
		t.Fatal(err)
	}
	provider.balanceURL = redirect.URL + "/user/balance"
	if _, err := provider.Generate(context.Background(), model.Request{Model: "deepseek-flash", Input: "fixture", MaxOutputTokens: 100}); err == nil {
		t.Fatal("redirected balance preflight was accepted")
	}
	if targetCalls != 0 || inner.calls != 0 {
		t.Fatal("balance redirect reached another endpoint or the paid provider")
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
