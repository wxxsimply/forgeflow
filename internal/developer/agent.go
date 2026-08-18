package developer

import (
	"context"
	"errors"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/repository"
)

type Options struct {
	Provider        model.Provider
	PromptLoader    *PromptLoader
	ContextBuilder  *ContextBuilder
	Model           string
	PromptVersion   string
	ReasoningEffort string
	MaxOutputTokens int
	Timeout         time.Duration
	Pricing         model.Pricing
}

type Agent struct {
	provider        model.Provider
	prompts         *PromptLoader
	contextBuilder  *ContextBuilder
	model           string
	promptVersion   string
	reasoningEffort string
	maxOutputTokens int
	timeout         time.Duration
	pricing         model.Pricing
}

func NewAgent(options Options) (*Agent, error) {
	if options.Provider == nil {
		return nil, apperror.New(apperror.CodeValidation, "developer model provider is required")
	}
	if options.PromptLoader == nil {
		options.PromptLoader = NewPromptLoader(nil)
	}
	if options.ContextBuilder == nil {
		options.ContextBuilder = NewContextBuilder(repository.DefaultLimits(), 128*1024)
	}
	if strings.TrimSpace(options.Model) == "" {
		options.Model = "gpt-5.6"
	}
	if strings.TrimSpace(options.PromptVersion) == "" {
		options.PromptVersion = "developer/v1"
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = 16_000
	}
	if options.MaxOutputTokens > 128_000 {
		return nil, apperror.New(apperror.CodeValidation, "developer max output tokens cannot exceed 128000")
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Minute
	}
	if err := options.Pricing.Validate(); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidation, "developer.pricing", "developer pricing is invalid")
	}
	if _, err := options.PromptLoader.Load(options.PromptVersion); err != nil {
		return nil, err
	}
	return &Agent{
		provider: options.Provider, prompts: options.PromptLoader, contextBuilder: options.ContextBuilder,
		model: options.Model, promptVersion: options.PromptVersion, reasoningEffort: options.ReasoningEffort,
		maxOutputTokens: options.MaxOutputTokens, timeout: options.Timeout, pricing: options.Pricing,
	}, nil
}

func (*Agent) Name() string    { return "developer" }
func (*Agent) Version() string { return "v1" }

func (a *Agent) Execute(ctx context.Context, input Input) (Result, error) {
	if strings.TrimSpace(input.Task) == "" {
		return Result{}, apperror.New(apperror.CodeValidation, "developer task is required")
	}
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
	userPrompt, err := prompt.RenderUser(bundle)
	if err != nil {
		return Result{}, err
	}
	callContext, cancel := context.WithTimeout(ctx, a.timeout)
	startedAt := time.Now().UTC()
	response, providerErr := model.GenerateObserved(callContext, a.provider, model.Request{
		Model: a.model, Instructions: prompt.System, Input: userPrompt,
		MaxOutputTokens: a.maxOutputTokens, ReasoningEffort: a.reasoningEffort,
		ResponseFormat: model.ResponseFormat{
			Name: "implementation_result", Description: "A bounded ForgeFlow implementation patch and evidence summary",
			Schema: ImplementationResultSchema(), Strict: true,
		},
	}, "developer", "developer", a.pricing)
	cancel()
	invocation := domain.ModelInvocation{
		Agent: "developer", AgentVersion: a.Version(), NodeID: input.NodeID,
		Provider: a.provider.Name(), Model: response.Model, ResponseID: response.ID,
		PromptVersion: prompt.Version, PromptSHA256: prompt.SHA256, Status: response.Status,
		InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
		CachedInputTokens: response.Usage.CachedInputTokens, ReasoningTokens: response.Usage.ReasoningTokens,
		LatencyMilliseconds: time.Since(startedAt).Milliseconds(), EstimatedCostUSD: a.pricing.Estimate(response.Usage),
		PricingConfigured: a.pricing.Configured(), CreatedAt: startedAt,
	}
	if invocation.Model == "" {
		invocation.Model = a.model
	}
	if providerErr != nil {
		if invocation.Status == "" {
			invocation.Status = "failed"
		}
		return Result{Invocation: &invocation}, mapProviderError(providerErr)
	}
	implementation, err := DecodeImplementationResult(response.OutputText)
	if err != nil {
		invocation.Status = "invalid_output"
		return Result{Invocation: &invocation}, err
	}
	approved := make(map[string]struct{}, len(input.Plan.FilesLikelyAffected))
	for _, path := range input.Plan.FilesLikelyAffected {
		approved[path] = struct{}{}
	}
	for _, path := range implementation.ChangedFiles {
		if _, exists := approved[path]; !exists {
			invocation.Status = "invalid_output"
			return Result{Invocation: &invocation}, apperror.New(apperror.CodeModelOutput, "developer declared a file outside the approved plan")
		}
	}
	return Result{Implementation: implementation, Invocation: &invocation}, nil
}

func (a *Agent) Timeout() time.Duration { return a.timeout }

func mapProviderError(err error) error {
	var providerError *model.Error
	if !errors.As(err, &providerError) {
		return apperror.Wrap(err, apperror.CodeInternal, "developer.provider", "model provider failed")
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
	return apperror.Wrap(err, code, "developer.provider", providerError.Message)
}

var _ Developer = (*Agent)(nil)
