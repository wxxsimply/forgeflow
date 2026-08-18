package reviewer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/assessment"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/repository"
)

type Reviewer interface {
	Name() string
	Version() string
	Execute(context.Context, assessment.Input) (Result, error)
}

type Result struct {
	Review     domain.ReviewResult
	Invocation *domain.ModelInvocation
}

type Fake struct {
	mu      sync.Mutex
	Results []Result
	Errors  []error
	Inputs  []assessment.Input
	calls   int
}

func (*Fake) Name() string    { return "reviewer" }
func (*Fake) Version() string { return "fake/v1" }

func (f *Fake) Execute(_ context.Context, input assessment.Input) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Inputs = append(f.Inputs, input)
	index := f.calls
	f.calls++
	if index < len(f.Errors) && f.Errors[index] != nil {
		return Result{}, f.Errors[index]
	}
	if index >= len(f.Results) {
		return Result{}, apperror.New(apperror.CodeModelOutput, "fake reviewer has no configured result")
	}
	return f.Results[index], nil
}

func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type Options struct {
	Provider        model.Provider
	ContextBuilder  *assessment.ContextBuilder
	PromptLoader    *PromptLoader
	Model           string
	PromptVersion   string
	ReasoningEffort string
	MaxOutputTokens int
	Timeout         time.Duration
	Pricing         model.Pricing
}

type Agent struct {
	provider        model.Provider
	contextBuilder  *assessment.ContextBuilder
	prompts         *PromptLoader
	model           string
	promptVersion   string
	reasoningEffort string
	maxOutputTokens int
	timeout         time.Duration
	pricing         model.Pricing
}

func NewAgent(options Options) (*Agent, error) {
	if options.Provider == nil {
		return nil, apperror.New(apperror.CodeValidation, "reviewer model provider is required")
	}
	if options.ContextBuilder == nil {
		options.ContextBuilder = assessment.NewContextBuilder(repository.DefaultLimits(), 512*1024)
	}
	if options.PromptLoader == nil {
		options.PromptLoader = NewPromptLoader(nil)
	}
	if options.Model == "" {
		options.Model = "gpt-5.6"
	}
	if options.PromptVersion == "" {
		options.PromptVersion = "reviewer/v1"
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = 8_000
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Minute
	}
	if options.MaxOutputTokens > 128_000 || options.Timeout > 10*time.Minute {
		return nil, apperror.New(apperror.CodeValidation, "reviewer output or timeout limit is invalid")
	}
	if err := options.Pricing.Validate(); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidation, "reviewer.pricing", "reviewer pricing is invalid")
	}
	if _, err := options.PromptLoader.Load(options.PromptVersion); err != nil {
		return nil, err
	}
	return &Agent{
		provider: options.Provider, contextBuilder: options.ContextBuilder, prompts: options.PromptLoader,
		model: options.Model, promptVersion: options.PromptVersion, reasoningEffort: options.ReasoningEffort,
		maxOutputTokens: options.MaxOutputTokens, timeout: options.Timeout, pricing: options.Pricing,
	}, nil
}

func (*Agent) Name() string             { return "reviewer" }
func (*Agent) Version() string          { return "v1" }
func (a *Agent) Timeout() time.Duration { return a.timeout }

func (a *Agent) Execute(ctx context.Context, input assessment.Input) (Result, error) {
	if allowed, reason := input.Budget.ModelCallAllowed(); !allowed {
		return Result{}, apperror.New(apperror.CodeBudget, reason)
	}
	bundle, err := a.contextBuilder.Build(ctx, input)
	if err != nil {
		return Result{}, err
	}
	prompt, err := a.prompts.Load(a.promptVersion)
	if err != nil {
		return Result{}, err
	}
	userPrompt, err := prompt.Render(bundle)
	if err != nil {
		return Result{}, err
	}
	callContext, cancel := context.WithTimeout(ctx, a.timeout)
	startedAt := time.Now().UTC()
	response, providerErr := model.GenerateObserved(callContext, a.provider, model.Request{
		Model: a.model, Instructions: prompt.System, Input: userPrompt,
		MaxOutputTokens: a.maxOutputTokens, ReasoningEffort: a.reasoningEffort,
		ResponseFormat: model.ResponseFormat{Name: "review_result", Description: "Independent code review findings", Schema: ReviewResultSchema(), Strict: true},
	}, "reviewer", "reviewer", a.pricing)
	cancel()
	invocation := invocationFor(input, a, prompt, response, startedAt)
	if providerErr != nil {
		invocation.Status = "failed"
		return Result{Invocation: &invocation}, mapProviderError(providerErr)
	}
	review, err := DecodeReviewResult(response.OutputText)
	if err != nil {
		invocation.Status = "invalid_output"
		return Result{Invocation: &invocation}, err
	}
	if err := validateFindingScope(review, input.Diff.ChangedFiles); err != nil {
		invocation.Status = "invalid_output"
		return Result{Invocation: &invocation}, err
	}
	review.Findings = mergeFindings(DeterministicFindings(input), review.Findings)
	return Result{Review: review, Invocation: &invocation}, nil
}

func DeterministicFindings(input assessment.Input) []domain.ReviewFinding {
	if !input.Test.Truncated || len(input.Diff.ChangedFiles) == 0 {
		return nil
	}
	return []domain.ReviewFinding{{
		ID: "REV-DET-TRUNCATED-TEST", Severity: domain.SeverityMedium, File: input.Diff.ChangedFiles[0],
		Title: "Test evidence was truncated", Evidence: "The recorded test tool result has truncated=true.",
		FailureMode:    "Important failure output may be missing from review evidence.",
		Recommendation: "Re-run a narrower test command that produces complete bounded evidence.", Confirmed: true,
	}}
}

func validateFindingScope(result domain.ReviewResult, changedFiles []string) error {
	allowed := make(map[string]struct{}, len(changedFiles))
	for _, file := range changedFiles {
		allowed[file] = struct{}{}
	}
	for _, finding := range result.Findings {
		if _, exists := allowed[finding.File]; !exists {
			return apperror.New(apperror.CodeModelOutput, "review finding references a file outside the final diff")
		}
	}
	return nil
}

func mergeFindings(deterministic, generated []domain.ReviewFinding) []domain.ReviewFinding {
	seen := make(map[string]struct{}, len(deterministic)+len(generated))
	result := make([]domain.ReviewFinding, 0, len(deterministic)+len(generated))
	for _, finding := range append(deterministic, generated...) {
		if _, exists := seen[finding.ID]; exists {
			continue
		}
		seen[finding.ID] = struct{}{}
		result = append(result, finding)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

//go:embed prompts/*/*
var embeddedPrompts embed.FS

var promptVersionPattern = regexp.MustCompile(`^reviewer/v[1-9][0-9]*$`)

type Prompt struct{ Version, System, UserTemplate, SHA256 string }
type PromptLoader struct{ files fs.FS }

func NewPromptLoader(files fs.FS) *PromptLoader {
	if files == nil {
		files = embeddedPrompts
	}
	return &PromptLoader{files: files}
}

func (l *PromptLoader) Load(version string) (Prompt, error) {
	if !promptVersionPattern.MatchString(version) {
		return Prompt{}, apperror.New(apperror.CodeValidation, "reviewer prompt version must look like reviewer/v1")
	}
	system, err := fs.ReadFile(l.files, "prompts/"+version+"/system.txt")
	if err != nil {
		return Prompt{}, apperror.Wrap(err, apperror.CodeNotFound, "reviewer.prompt.system", "reviewer system prompt was not found")
	}
	user, err := fs.ReadFile(l.files, "prompts/"+version+"/user.tmpl")
	if err != nil {
		return Prompt{}, apperror.Wrap(err, apperror.CodeNotFound, "reviewer.prompt.user", "reviewer user prompt was not found")
	}
	digest := sha256.Sum256(append(append([]byte(nil), system...), user...))
	return Prompt{Version: version, System: strings.TrimSpace(string(system)), UserTemplate: string(user), SHA256: hex.EncodeToString(digest[:])}, nil
}

func (p Prompt) Render(bundle assessment.Bundle) (string, error) {
	tmpl, err := template.New(p.Version).Funcs(template.FuncMap{"jsonValue": func(value any) (string, error) { data, err := json.Marshal(value); return string(data), err }}).Option("missingkey=error").Parse(p.UserTemplate)
	if err != nil {
		return "", apperror.Wrap(err, apperror.CodeInternal, "reviewer.prompt.parse", "reviewer prompt is invalid")
	}
	var output strings.Builder
	if err := tmpl.Execute(&output, bundle); err != nil {
		return "", apperror.Wrap(err, apperror.CodeInternal, "reviewer.prompt.render", "reviewer prompt could not be rendered")
	}
	if output.Len() > 1024*1024 {
		return "", apperror.New(apperror.CodeBudget, "reviewer prompt exceeds size limit")
	}
	return output.String(), nil
}

var reviewResultSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["summary","findings"],"properties":{"summary":{"type":"string","minLength":1,"maxLength":4000},"findings":{"type":"array","maxItems":200,"items":{"type":"object","additionalProperties":false,"required":["id","severity","file","line","title","evidence","failureMode","recommendation","confirmed"],"properties":{"id":{"type":"string"},"severity":{"type":"string","enum":["info","low","medium","high","blocking"]},"file":{"type":"string"},"line":{"type":"integer","minimum":0},"title":{"type":"string"},"evidence":{"type":"string"},"failureMode":{"type":"string"},"recommendation":{"type":"string"},"confirmed":{"type":"boolean"}}}}}}`)

func ReviewResultSchema() json.RawMessage { return append(json.RawMessage(nil), reviewResultSchema...) }

type strictReview struct {
	Summary  *string                 `json:"summary"`
	Findings *[]domain.ReviewFinding `json:"findings"`
}

func DecodeReviewResult(output string) (domain.ReviewResult, error) {
	if strings.TrimSpace(output) == "" || len(output) > 1024*1024 {
		return domain.ReviewResult{}, apperror.New(apperror.CodeModelOutput, "reviewer returned empty or oversized output")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(output))
	decoder.DisallowUnknownFields()
	var decoded strictReview
	if err := decoder.Decode(&decoded); err != nil {
		return domain.ReviewResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "reviewer.decode", "reviewer output is not strict JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return domain.ReviewResult{}, apperror.New(apperror.CodeModelOutput, "reviewer output contains trailing data")
	}
	if decoded.Summary == nil || decoded.Findings == nil {
		return domain.ReviewResult{}, apperror.New(apperror.CodeModelOutput, "reviewer output is missing fields")
	}
	result := domain.ReviewResult{Summary: *decoded.Summary, Findings: *decoded.Findings}
	if err := result.Validate(); err != nil {
		return domain.ReviewResult{}, apperror.Wrap(err, apperror.CodeModelOutput, "reviewer.validate", "reviewer output failed validation")
	}
	return result, nil
}

func invocationFor(input assessment.Input, agent *Agent, prompt Prompt, response model.Response, started time.Time) domain.ModelInvocation {
	modelName := response.Model
	if modelName == "" {
		modelName = agent.model
	}
	return domain.ModelInvocation{Agent: "reviewer", AgentVersion: agent.Version(), NodeID: input.NodeID, Provider: agent.provider.Name(), Model: modelName, ResponseID: response.ID, PromptVersion: prompt.Version, PromptSHA256: prompt.SHA256, Status: response.Status, InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens, CachedInputTokens: response.Usage.CachedInputTokens, ReasoningTokens: response.Usage.ReasoningTokens, LatencyMilliseconds: time.Since(started).Milliseconds(), EstimatedCostUSD: agent.pricing.Estimate(response.Usage), PricingConfigured: agent.pricing.Configured(), CreatedAt: started}
}

func mapProviderError(err error) error {
	var providerError *model.Error
	if !errors.As(err, &providerError) {
		return apperror.Wrap(err, apperror.CodeInternal, "reviewer.provider", "model provider failed")
	}
	code := apperror.CodeInternal
	switch providerError.Kind {
	case model.ErrorAuthentication:
		code = apperror.CodeUnauthorized
	case model.ErrorRateLimit, model.ErrorTransient:
		code = apperror.CodeTransient
	case model.ErrorInvalidRequest:
		code = apperror.CodeValidation
	case model.ErrorRefusal:
		code = apperror.CodePolicyDenied
	case model.ErrorInvalidOutput:
		code = apperror.CodeModelOutput
	}
	return apperror.Wrap(err, code, "reviewer.provider", providerError.Message)
}

var _ Reviewer = (*Agent)(nil)
var _ Reviewer = (*Fake)(nil)
