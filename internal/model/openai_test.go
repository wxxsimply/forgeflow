package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIProviderRequiresAPIKey(t *testing.T) {
	_, err := NewOpenAIProvider(OpenAIConfig{})
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.Kind != ErrorAuthentication {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIProviderSendsResponsesStructuredOutputRequest(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/responses" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("authorization header was not set")
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
          "id":"resp_123","model":"gpt-test","status":"completed",
          "output":[{"type":"message","content":[{"type":"output_text","text":"{\"summary\":\"ok\"}"}]}],
          "usage":{"input_tokens":120,"output_tokens":30,"total_tokens":150,"input_tokens_details":{"cached_tokens":20},"output_tokens_details":{"reasoning_tokens":10}}
        }`))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIConfig{APIKey: "test-secret", BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Generate(context.Background(), validModelRequest())
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.ID != "resp_123" || response.OutputText != `{"summary":"ok"}` || response.Usage.CachedInputTokens != 20 || response.Usage.ReasoningTokens != 10 {
		t.Fatalf("response = %+v", response)
	}
	if captured["store"] != false || captured["model"] != "gpt-test" {
		t.Fatalf("request = %+v", captured)
	}
	text, ok := captured["text"].(map[string]any)
	if !ok {
		t.Fatalf("text = %#v", captured["text"])
	}
	format, ok := text["format"].(map[string]any)
	if !ok || format["type"] != "json_schema" || format["strict"] != true {
		t.Fatalf("format = %#v", text["format"])
	}
}

func TestOpenAIProviderRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"id":"ok","model":"gpt-test","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}],"usage":{}}`))
	}))
	defer server.Close()
	provider, err := NewOpenAIProvider(OpenAIConfig{
		APIKey: "test", BaseURL: server.URL, HTTPClient: server.Client(), MaxRetries: 1, RetryBaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Generate(context.Background(), validModelRequest()); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestOpenAIProviderClassifiesErrorsWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"message":"invalid credential never-log-this-key"}}`))
	}))
	defer server.Close()
	const secret = "never-log-this-key"
	provider, err := NewOpenAIProvider(OpenAIConfig{APIKey: secret, BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Generate(context.Background(), validModelRequest())
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.Kind != ErrorAuthentication {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("provider error leaked the API key")
	}
}

func TestOpenAIProviderDetectsRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"id":"refused","model":"gpt-test","status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"cannot comply"}]}],"usage":{}}`))
	}))
	defer server.Close()
	provider, err := NewOpenAIProvider(OpenAIConfig{APIKey: "test", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Generate(context.Background(), validModelRequest())
	var providerError *Error
	if !errors.As(err, &providerError) || providerError.Kind != ErrorRefusal {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIProviderDoesNotFollowRedirects(t *testing.T) {
	var redirectedCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedCalls.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider, err := NewOpenAIProvider(OpenAIConfig{APIKey: "test", BaseURL: source.URL, HTTPClient: source.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Generate(context.Background(), validModelRequest()); err == nil {
		t.Fatal("Generate() followed or accepted a redirect")
	}
	if redirectedCalls.Load() != 0 {
		t.Fatal("provider sent prompt data to a redirect target")
	}
}

func validModelRequest() Request {
	return Request{
		Model: "gpt-test", Instructions: "Return JSON", Input: "fixture", MaxOutputTokens: 100,
		ResponseFormat: ResponseFormat{Name: "fixture", Schema: json.RawMessage(`{"type":"object"}`), Strict: true},
	}
}
