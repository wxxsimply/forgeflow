package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOpenAIBaseURL    = "https://api.openai.com/v1"
	defaultMaxResponseBytes = 4 * 1024 * 1024
	defaultRetryBaseDelay   = 250 * time.Millisecond
	maximumServerRetryDelay = 5 * time.Second
)

type OpenAIConfig struct {
	APIKey           string
	BaseURL          string
	ProviderName     string
	Organization     string
	Project          string
	HTTPClient       *http.Client
	MaxRetries       int
	RetryBaseDelay   time.Duration
	MaxResponseBytes int64
}

type OpenAIProvider struct {
	apiKey           string
	endpoint         string
	name             string
	organization     string
	project          string
	client           *http.Client
	maxRetries       int
	retryBaseDelay   time.Duration
	maxResponseBytes int64
}

func NewOpenAIProvider(configuration OpenAIConfig) (*OpenAIProvider, error) {
	if strings.TrimSpace(configuration.APIKey) == "" {
		return nil, &Error{Kind: ErrorAuthentication, Message: "OPENAI_API_KEY is required for the OpenAI-compatible provider"}
	}
	providerName := strings.TrimSpace(configuration.ProviderName)
	if providerName == "" {
		providerName = "openai"
	}
	if providerName != "openai" && providerName != "deepseek" {
		return nil, &Error{Kind: ErrorInvalidRequest, Message: "provider name must be openai or deepseek"}
	}
	baseURL := strings.TrimSpace(configuration.BaseURL)
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, &Error{Kind: ErrorInvalidRequest, Message: "OpenAI base URL is invalid", Err: err}
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, &Error{Kind: ErrorInvalidRequest, Message: "OpenAI base URL must use HTTPS unless it targets loopback"}
	}
	if configuration.MaxRetries < 0 || configuration.MaxRetries > 5 {
		return nil, &Error{Kind: ErrorInvalidRequest, Message: "OpenAI max retries must be between 0 and 5"}
	}
	client := configuration.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	if clientCopy.CheckRedirect == nil {
		clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	}
	retryDelay := configuration.RetryBaseDelay
	if retryDelay <= 0 {
		retryDelay = defaultRetryBaseDelay
	}
	maxResponseBytes := configuration.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	return &OpenAIProvider{
		apiKey: strings.TrimSpace(configuration.APIKey), endpoint: strings.TrimRight(baseURL, "/") + "/responses",
		name:         providerName,
		organization: configuration.Organization, project: configuration.Project, client: &clientCopy,
		maxRetries: configuration.MaxRetries, retryBaseDelay: retryDelay, maxResponseBytes: maxResponseBytes,
	}, nil
}

func (p *OpenAIProvider) Name() string { return p.name }

func (p *OpenAIProvider) Generate(ctx context.Context, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	requestBody := openAIRequest{
		Model: request.Model, Instructions: request.Instructions, Input: request.Input,
		MaxOutputTokens: request.MaxOutputTokens,
		Reasoning:       openAIReasoning{Effort: request.ReasoningEffort},
		Text: openAIText{Format: openAITextFormat{
			Type: "json_schema", Name: request.ResponseFormat.Name, Description: request.ResponseFormat.Description,
			Schema: request.ResponseFormat.Schema,
		}},
	}
	if p.name == "openai" {
		store := false
		strict := request.ResponseFormat.Strict
		requestBody.Store = &store
		requestBody.Text.Format.Strict = &strict
	} else {
		requestBody.Text.Format.Description = ""
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return Response{}, &Error{Kind: ErrorInvalidRequest, Message: "OpenAI request could not be encoded", Err: err}
	}

	responseBody, statusCode, err := p.send(ctx, body)
	if err != nil {
		return Response{}, err
	}
	if statusCode < 200 || statusCode >= 300 {
		return Response{}, decodeOpenAIError(statusCode, responseBody, p.apiKey)
	}
	var decoded openAIResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Response{}, &Error{Kind: ErrorInvalidOutput, StatusCode: statusCode, Message: "OpenAI returned malformed JSON", Err: err}
	}
	if decoded.Error != nil {
		return Response{}, &Error{Kind: ErrorInvalidOutput, StatusCode: statusCode, Message: redactSecret(cleanProviderMessage(decoded.Error.Message), p.apiKey)}
	}
	result := Response{
		ID: decoded.ID, Model: decoded.Model, Status: decoded.Status,
		Usage: Usage{
			InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens,
			TotalTokens: decoded.Usage.TotalTokens, CachedInputTokens: decoded.Usage.InputTokensDetails.CachedTokens,
			CacheWriteInputTokens: decoded.Usage.InputTokensDetails.CacheWriteTokens,
			ReasoningTokens:       decoded.Usage.OutputTokensDetails.ReasoningTokens,
		},
	}
	var output strings.Builder
	for _, item := range decoded.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			switch content.Type {
			case "output_text":
				output.WriteString(content.Text)
			case "refusal":
				result.Refusal = content.Refusal
			}
		}
	}
	result.OutputText = output.String()
	if result.Refusal != "" {
		return result, &Error{Kind: ErrorRefusal, Message: "OpenAI refused to generate the requested plan"}
	}
	if result.Status != "completed" {
		return result, &Error{Kind: ErrorInvalidOutput, Message: "OpenAI response did not complete successfully"}
	}
	if strings.TrimSpace(result.OutputText) == "" {
		return result, &Error{Kind: ErrorInvalidOutput, Message: "OpenAI response contained no output text"}
	}
	return result, nil
}

func (p *OpenAIProvider) send(ctx context.Context, body []byte) ([]byte, int, error) {
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, 0, &Error{Kind: ErrorInvalidRequest, Message: "OpenAI request could not be created", Err: err}
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if p.organization != "" {
			req.Header.Set("OpenAI-Organization", p.organization)
		}
		if p.project != "" {
			req.Header.Set("OpenAI-Project", p.project)
		}
		response, requestErr := p.client.Do(req)
		if requestErr != nil {
			if ctx.Err() != nil {
				return nil, 0, &Error{Kind: ErrorTransient, Message: "OpenAI request was cancelled or timed out", Err: ctx.Err()}
			}
			if attempt < p.maxRetries && waitForProviderRetry(ctx, retryDelay(attempt, "", p.retryBaseDelay)) {
				continue
			}
			return nil, 0, &Error{Kind: ErrorTransient, Message: "OpenAI request failed", Err: requestErr}
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, p.maxResponseBytes+1))
		response.Body.Close()
		if readErr != nil {
			return nil, response.StatusCode, &Error{Kind: ErrorTransient, StatusCode: response.StatusCode, Message: "OpenAI response could not be read", Err: readErr}
		}
		if int64(len(responseBody)) > p.maxResponseBytes {
			return nil, response.StatusCode, &Error{Kind: ErrorInvalidOutput, StatusCode: response.StatusCode, Message: "OpenAI response exceeded the configured size limit"}
		}
		if retryableStatus(response.StatusCode) && attempt < p.maxRetries {
			if waitForProviderRetry(ctx, retryDelay(attempt, response.Header.Get("Retry-After"), p.retryBaseDelay)) {
				continue
			}
			return nil, response.StatusCode, &Error{Kind: ErrorTransient, StatusCode: response.StatusCode, Message: "OpenAI retry was cancelled", Err: ctx.Err()}
		}
		return responseBody, response.StatusCode, nil
	}
	return nil, 0, &Error{Kind: ErrorTransient, Message: "OpenAI retry limit exhausted"}
}

type openAIRequest struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions"`
	Input           string          `json:"input"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	Store           *bool           `json:"store,omitempty"`
	Reasoning       openAIReasoning `json:"reasoning,omitempty"`
	Text            openAIText      `json:"text"`
}

type openAIReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type openAIText struct {
	Format openAITextFormat `json:"format"`
}

type openAITextFormat struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      *bool           `json:"strict,omitempty"`
}

type openAIResponse struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Refusal string `json:"refusal"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		TotalTokens        int `json:"total_tokens"`
		InputTokensDetails struct {
			CachedTokens     int `json:"cached_tokens"`
			CacheWriteTokens int `json:"cache_write_tokens"`
		} `json:"input_tokens_details"`
		OutputTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Instructions) == "" || strings.TrimSpace(request.Input) == "" {
		return &Error{Kind: ErrorInvalidRequest, Message: "model, instructions, and input are required"}
	}
	if request.MaxOutputTokens <= 0 || request.MaxOutputTokens > 128_000 {
		return &Error{Kind: ErrorInvalidRequest, Message: "max output tokens must be between 1 and 128000"}
	}
	if request.ResponseFormat.Name == "" || len(request.ResponseFormat.Schema) == 0 || !json.Valid(request.ResponseFormat.Schema) {
		return &Error{Kind: ErrorInvalidRequest, Message: "a valid JSON response schema is required"}
	}
	return nil
}

func decodeOpenAIError(statusCode int, body []byte, secret string) error {
	kind := ErrorInvalidRequest
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		kind = ErrorAuthentication
	case statusCode == http.StatusTooManyRequests:
		kind = ErrorRateLimit
	case retryableStatus(statusCode):
		kind = ErrorTransient
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	message := "OpenAI API request failed"
	if json.Unmarshal(body, &envelope) == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		message = redactSecret(cleanProviderMessage(envelope.Error.Message), secret)
	}
	return &Error{Kind: kind, StatusCode: statusCode, Message: message}
}

func redactSecret(message, secret string) string {
	if secret == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[redacted]")
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusConflict ||
		statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func retryDelay(attempt int, retryAfter string, base time.Duration) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > maximumServerRetryDelay {
			return maximumServerRetryDelay
		}
		return delay
	}
	delay := base * time.Duration(1<<attempt)
	if delay > maximumServerRetryDelay {
		return maximumServerRetryDelay
	}
	return delay
}

func waitForProviderRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func cleanProviderMessage(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(message, "\r", " "), "\n", " "))
	if len(message) > 500 {
		message = message[:500] + "…"
	}
	return message
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

var _ Provider = (*OpenAIProvider)(nil)

func (p *OpenAIProvider) String() string {
	return fmt.Sprintf("OpenAIProvider(name=%s, endpoint=%s)", p.name, p.endpoint)
}
