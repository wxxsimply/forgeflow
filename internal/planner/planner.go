package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/domain"
	"forgeflow/internal/model"
	"forgeflow/internal/repository"
)

const defaultRepositoryContextBytes = 64 * 1024

type Input struct {
	Task              string
	RepositoryPath    string
	BaseRevision      string
	RepositoryContext string
	Budget            domain.RunBudget
}

type Result struct {
	Plan       domain.ExecutionPlan
	Invocation *domain.ModelInvocation
}

type Planner interface {
	CreatePlan(context.Context, Input) (Result, error)
}

type Options struct {
	Provider                  model.Provider
	Inspector                 repository.RepositoryInspector
	PromptLoader              *PromptLoader
	APIKey                    string
	ModelProvider             string
	OpenAIBaseURL             string
	OpenAIOrganization        string
	OpenAIProject             string
	OpenAIMaxRetries          int
	Model                     string
	PromptVersion             string
	ReasoningEffort           string
	MaxOutputTokens           int
	Timeout                   time.Duration
	Pricing                   model.Pricing
	MaxRepositoryContextBytes int
}

type Agent struct {
	provider                  model.Provider
	inspector                 repository.RepositoryInspector
	prompts                   *PromptLoader
	model                     string
	promptVersion             string
	reasoningEffort           string
	maxOutputTokens           int
	timeout                   time.Duration
	pricing                   model.Pricing
	maxRepositoryContextBytes int
}

func New(mode string, optional ...Options) (Planner, error) {
	var options Options
	if len(optional) > 1 {
		return nil, apperror.New(apperror.CodeValidation, "planner accepts at most one options value")
	}
	if len(optional) == 1 {
		options = optional[0]
	}
	switch mode {
	case "", "mock":
		return Mock{}, nil
	case "openai":
		if options.Provider == nil {
			provider, err := model.NewOpenAIProvider(model.OpenAIConfig{
				APIKey: options.APIKey, BaseURL: options.OpenAIBaseURL,
				ProviderName: options.ModelProvider,
				Organization: options.OpenAIOrganization, Project: options.OpenAIProject,
				MaxRetries: options.OpenAIMaxRetries,
			})
			if err != nil {
				return nil, mapProviderError(err)
			}
			options.Provider = provider
		}
		return NewAgent(options)
	default:
		return nil, apperror.New(apperror.CodeValidation, "planner mode must be mock or openai")
	}
}

func NewAgent(options Options) (*Agent, error) {
	if options.Provider == nil {
		return nil, apperror.New(apperror.CodeValidation, "planner model provider is required")
	}
	if options.PromptLoader == nil {
		options.PromptLoader = NewPromptLoader(nil)
	}
	if strings.TrimSpace(options.Model) == "" {
		options.Model = "gpt-5.6"
	}
	if strings.TrimSpace(options.PromptVersion) == "" {
		options.PromptVersion = "planner/v1"
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = 4_000
	}
	if options.MaxOutputTokens > 128_000 {
		return nil, apperror.New(apperror.CodeValidation, "planner max output tokens cannot exceed 128000")
	}
	if options.Timeout <= 0 {
		options.Timeout = 120 * time.Second
	}
	if options.MaxRepositoryContextBytes <= 0 {
		options.MaxRepositoryContextBytes = defaultRepositoryContextBytes
	}
	if options.MaxRepositoryContextBytes > 1024*1024 {
		return nil, apperror.New(apperror.CodeValidation, "planner repository context cannot exceed 1 MiB")
	}
	if err := options.Pricing.Validate(); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidation, "planner.pricing", "planner pricing is invalid")
	}
	if _, err := options.PromptLoader.Load(options.PromptVersion); err != nil {
		return nil, err
	}
	return &Agent{
		provider: options.Provider, inspector: options.Inspector, prompts: options.PromptLoader,
		model: options.Model, promptVersion: options.PromptVersion, reasoningEffort: options.ReasoningEffort,
		maxOutputTokens: options.MaxOutputTokens, timeout: options.Timeout, pricing: options.Pricing,
		maxRepositoryContextBytes: options.MaxRepositoryContextBytes,
	}, nil
}

func (a *Agent) CreatePlan(ctx context.Context, input Input) (Result, error) {
	if strings.TrimSpace(input.Task) == "" {
		return Result{}, apperror.New(apperror.CodeValidation, "planner task is required")
	}
	if len(input.Task) > 20_000 {
		return Result{}, apperror.New(apperror.CodeValidation, "planner task exceeds 20000 characters")
	}
	if allowed, reason := input.Budget.ModelCallAllowed(); !allowed {
		return Result{}, apperror.New(apperror.CodeBudget, reason)
	}
	baseRevision := strings.TrimSpace(input.BaseRevision)
	if baseRevision == "" {
		baseRevision = "HEAD"
	}
	repositoryContext := input.RepositoryContext
	if repositoryContext == "" && a.inspector != nil {
		summary, err := a.inspector.Inspect(ctx, domain.RepositoryRef{Path: input.RepositoryPath, BaseRevision: baseRevision})
		if err != nil {
			return Result{}, err
		}
		repositoryContext = buildRepositoryContext(summary, a.maxRepositoryContextBytes)
	}
	if repositoryContext == "" {
		repositoryContext = "No repository context was provided. Treat file scope and test commands as assumptions."
	}
	prompt, err := a.prompts.Load(a.promptVersion)
	if err != nil {
		return Result{}, err
	}
	userPrompt, err := prompt.RenderUser(PromptData{
		Task: input.Task, RepositoryPath: input.RepositoryPath,
		BaseRevision: baseRevision, RepositoryContext: repositoryContext,
	})
	if err != nil {
		return Result{}, err
	}

	callContext, cancel := context.WithTimeout(ctx, a.timeout)
	startedAt := time.Now().UTC()
	response, providerErr := model.GenerateObserved(callContext, a.provider, model.Request{
		Model: a.model, Instructions: prompt.System, Input: userPrompt,
		MaxOutputTokens: a.maxOutputTokens, ReasoningEffort: a.reasoningEffort,
		ResponseFormat: model.ResponseFormat{
			Name: "execution_plan", Description: "A bounded ForgeFlow software implementation plan",
			Schema: ExecutionPlanSchema(), Strict: true,
		},
	}, "planner", "planner", a.pricing)
	cancel()
	invocation := domain.ModelInvocation{
		Agent: "planner", AgentVersion: "v1", NodeID: "planner",
		Provider: a.provider.Name(), Model: response.Model, ResponseID: response.ID,
		PromptVersion: prompt.Version, PromptSHA256: prompt.SHA256,
		Status: response.Status, InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
		CachedInputTokens: response.Usage.CachedInputTokens, ReasoningTokens: response.Usage.ReasoningTokens,
		LatencyMilliseconds: time.Since(startedAt).Milliseconds(), EstimatedCostUSD: a.pricing.Estimate(response.Usage),
		PricingConfigured: a.pricing.Configured(), CreatedAt: startedAt,
	}
	if invocation.Model == "" {
		invocation.Model = a.model
	}
	if providerErr != nil {
		var typedProviderError *model.Error
		if errors.As(providerErr, &typedProviderError) && typedProviderError.Kind == model.ErrorRefusal {
			invocation.Status = "refused"
		} else if errors.As(providerErr, &typedProviderError) && typedProviderError.Kind == model.ErrorInvalidOutput {
			invocation.Status = "invalid_output"
		} else if invocation.Status == "" {
			invocation.Status = "failed"
		}
		return Result{Invocation: &invocation}, mapProviderError(providerErr)
	}
	plan, err := DecodeExecutionPlan(response.OutputText)
	if err != nil {
		invocation.Status = "invalid_output"
		return Result{Invocation: &invocation}, err
	}
	return Result{Plan: plan, Invocation: &invocation}, nil
}

func (a *Agent) PlanningTimeout() time.Duration { return a.timeout }

func Timeout(planAgent Planner) time.Duration {
	type timeoutPlanner interface {
		PlanningTimeout() time.Duration
	}
	if configured, ok := planAgent.(timeoutPlanner); ok && configured.PlanningTimeout() > 0 {
		return configured.PlanningTimeout()
	}
	return 30 * time.Second
}

func mapProviderError(err error) error {
	var providerError *model.Error
	if !errors.As(err, &providerError) {
		return apperror.Wrap(err, apperror.CodeInternal, "planner.provider", "model provider failed")
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
	return apperror.Wrap(err, code, "planner.provider", providerError.Message)
}

func buildRepositoryContext(summary domain.RepositorySummary, maxBytes int) string {
	var output strings.Builder
	fmt.Fprintf(&output, "root: %s\nbase_commit: %s\nhead_commit: %s\nclean: %t\n", summary.Root, summary.BaseCommit, summary.HeadCommit, summary.Clean)
	for _, document := range summary.Documents {
		section := fmt.Sprintf("\n--- %s (%s, sha256=%s) ---\n%s\n", document.Path, document.Kind, document.SHA256, document.Content)
		if output.Len()+len(section) > maxBytes {
			remaining := maxBytes - output.Len()
			if remaining > 0 {
				output.WriteString(strings.ToValidUTF8(section[:min(remaining, len(section))], "�"))
			}
			output.WriteString("\n[repository context truncated]\n")
			break
		}
		output.WriteString(section)
	}
	return output.String()
}

var _ Planner = (*Agent)(nil)
