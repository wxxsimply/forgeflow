package model

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"forgeflow/internal/observability"
)

type Provider interface {
	Name() string
	Generate(context.Context, Request) (Response, error)
}

func GenerateObserved(ctx context.Context, provider Provider, request Request, agent, nodeID string, pricing Pricing) (Response, error) {
	started := time.Now()
	ctx, span := observability.StartModelSpan(ctx, provider.Name(), request.Model, agent, nodeID)
	response, err := provider.Generate(ctx, request)
	status := response.Status
	if status == "" {
		status = "failed"
	}
	observability.DefaultMetrics().Model(provider.Name(), request.Model, status, response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.CachedInputTokens, pricing.Estimate(response.Usage), time.Since(started))
	observability.EndSpan(span, err, status)
	return response, err
}

type Request struct {
	Model           string
	Instructions    string
	Input           string
	ResponseFormat  ResponseFormat
	MaxOutputTokens int
	ReasoningEffort string
}

type ResponseFormat struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Strict      bool
}

type Usage struct {
	InputTokens           int `json:"inputTokens"`
	OutputTokens          int `json:"outputTokens"`
	TotalTokens           int `json:"totalTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
	ReasoningTokens       int `json:"reasoningTokens"`
}

type Response struct {
	ID         string
	Model      string
	Status     string
	OutputText string
	Refusal    string
	Usage      Usage
}

type ErrorKind string

const (
	ErrorAuthentication ErrorKind = "authentication"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorInvalidRequest ErrorKind = "invalid_request"
	ErrorTransient      ErrorKind = "transient"
	ErrorRefusal        ErrorKind = "refusal"
	ErrorInvalidOutput  ErrorKind = "invalid_output"
)

type Error struct {
	Kind       ErrorKind
	StatusCode int
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Pricing struct {
	InputUSDPerMillionTokens  float64
	OutputUSDPerMillionTokens float64
}

func (p Pricing) Validate() error {
	if p.InputUSDPerMillionTokens < 0 || p.OutputUSDPerMillionTokens < 0 {
		return fmt.Errorf("model token prices cannot be negative")
	}
	return nil
}

func (p Pricing) Configured() bool {
	return p.InputUSDPerMillionTokens > 0 || p.OutputUSDPerMillionTokens > 0
}

func (p Pricing) Estimate(usage Usage) float64 {
	return float64(usage.InputTokens)/1_000_000*p.InputUSDPerMillionTokens +
		float64(usage.OutputTokens)/1_000_000*p.OutputUSDPerMillionTokens
}

type FakeProvider struct {
	mu        sync.Mutex
	Responses []Response
	Errors    []error
	Requests  []Request
	calls     int
}

func (f *FakeProvider) Name() string { return "fake" }

func (f *FakeProvider) Generate(_ context.Context, request Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, request)
	index := f.calls
	f.calls++
	if index < len(f.Errors) && f.Errors[index] != nil {
		return Response{}, f.Errors[index]
	}
	if index >= len(f.Responses) {
		return Response{}, &Error{Kind: ErrorInvalidOutput, Message: "fake provider has no configured response"}
	}
	return f.Responses[index], nil
}

func (f *FakeProvider) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var _ Provider = (*FakeProvider)(nil)
