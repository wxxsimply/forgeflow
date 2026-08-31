package evalexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"forgeflow/internal/domain"
	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/model"
	"forgeflow/internal/repository"
)

const (
	PolicyVersion = "eval-policy/v1"
	ToolVersion   = "eval-tools/v1"
)

type Options struct {
	Provider          model.Provider
	Pricing           UsagePricing
	FixtureRepository string
	GraderRepository  string
	WorkspaceRoot     string
	Model             string
	ReasoningEffort   string
	MaxOutputTokens   int
	CallTimeout       time.Duration
	CommandTimeout    time.Duration
	ContextMaxBytes   int
}

type core struct {
	options    Options
	workspaces repository.WorkspaceManager
}

func newCore(options Options) (*core, error) {
	if options.Provider == nil {
		return nil, fmt.Errorf("eval model provider is required")
	}
	for name, value := range map[string]string{"fixture repository": options.FixtureRepository, "grader repository": options.GraderRepository, "workspace root": options.WorkspaceRoot, "model": options.Model} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	if err := options.Pricing.Validate(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("eval pricing is invalid: %w", err)
	}
	if options.MaxOutputTokens <= 0 {
		options.MaxOutputTokens = 16_000
	}
	if options.CallTimeout <= 0 {
		options.CallTimeout = 5 * time.Minute
	}
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = 10 * time.Minute
	}
	if options.ContextMaxBytes <= 0 {
		options.ContextMaxBytes = 256 * 1024
	}
	if options.ContextMaxBytes > 1024*1024 {
		return nil, fmt.Errorf("eval context cannot exceed 1 MiB")
	}
	manager, err := repository.NewGitWorkspaceManager(options.WorkspaceRoot, repository.DefaultLimits())
	if err != nil {
		return nil, err
	}
	return &core{options: options, workspaces: manager}, nil
}

type SingleAgentExecutor struct{ core *core }
type PlannerDeveloperExecutor struct{ core *core }
type ForgeFlowExecutor struct{ core *core }

func NewSingleAgent(options Options) (*SingleAgentExecutor, error) {
	value, err := newCore(options)
	return &SingleAgentExecutor{core: value}, err
}
func NewPlannerDeveloper(options Options) (*PlannerDeveloperExecutor, error) {
	value, err := newCore(options)
	return &PlannerDeveloperExecutor{core: value}, err
}
func NewForgeFlow(options Options) (*ForgeFlowExecutor, error) {
	value, err := newCore(options)
	return &ForgeFlowExecutor{core: value}, err
}

func (e *SingleAgentExecutor) Execute(ctx context.Context, evalCase fulleval.Case, _ fulleval.Mode) (fulleval.Observation, error) {
	return e.core.execute(ctx, evalCase, fulleval.ModeSingleAgent)
}
func (e *PlannerDeveloperExecutor) Execute(ctx context.Context, evalCase fulleval.Case, _ fulleval.Mode) (fulleval.Observation, error) {
	return e.core.execute(ctx, evalCase, fulleval.ModePlannerDeveloper)
}
func (e *ForgeFlowExecutor) Execute(ctx context.Context, evalCase fulleval.Case, _ fulleval.Mode) (fulleval.Observation, error) {
	return e.core.execute(ctx, evalCase, fulleval.ModeForgeFlow)
}

type Mux struct {
	Single           *SingleAgentExecutor
	PlannerDeveloper *PlannerDeveloperExecutor
	ForgeFlow        *ForgeFlowExecutor
}

func NewMux(options Options) (*Mux, error) {
	single, err := NewSingleAgent(options)
	if err != nil {
		return nil, err
	}
	plannerDeveloper, err := NewPlannerDeveloper(options)
	if err != nil {
		return nil, err
	}
	forgeFlow, err := NewForgeFlow(options)
	if err != nil {
		return nil, err
	}
	return &Mux{Single: single, PlannerDeveloper: plannerDeveloper, ForgeFlow: forgeFlow}, nil
}

func (m *Mux) Execute(ctx context.Context, evalCase fulleval.Case, mode fulleval.Mode) (fulleval.Observation, error) {
	switch mode {
	case fulleval.ModeSingleAgent:
		return m.Single.Execute(ctx, evalCase, mode)
	case fulleval.ModePlannerDeveloper:
		return m.PlannerDeveloper.Execute(ctx, evalCase, mode)
	case fulleval.ModeForgeFlow:
		return m.ForgeFlow.Execute(ctx, evalCase, mode)
	default:
		return fulleval.Observation{}, fmt.Errorf("unsupported eval mode %q", mode)
	}
}

type meter struct {
	provider model.Provider
	pricing  UsagePricing
	usage    model.Usage
	requests int
	cost     float64
	now      func() time.Time
}

func (m *meter) call(ctx context.Context, request model.Request, agent string, timeout time.Duration) (model.Response, error) {
	now := m.now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := m.pricing.CanStart(now(), timeout); err != nil {
		return model.Response{}, err
	}
	callContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	observabilityPricing := model.Pricing{InputUSDPerMillionTokens: m.pricing.InputUSDPerMillionTokens, OutputUSDPerMillionTokens: m.pricing.OutputUSDPerMillionTokens}
	response, err := model.GenerateObserved(callContext, m.provider, request, agent, "eval", observabilityPricing)
	m.requests++
	m.usage.InputTokens += response.Usage.InputTokens
	m.usage.OutputTokens += response.Usage.OutputTokens
	m.usage.TotalTokens += response.Usage.TotalTokens
	m.usage.CachedInputTokens += response.Usage.CachedInputTokens
	m.usage.CacheWriteInputTokens += response.Usage.CacheWriteInputTokens
	m.usage.ReasoningTokens += response.Usage.ReasoningTokens
	cost, pricingErr := m.pricing.Estimate(response.Usage)
	if pricingErr != nil {
		if err != nil {
			return response, errors.Join(err, pricingErr)
		}
		return response, pricingErr
	}
	m.cost += cost
	return response, err
}

type solution struct {
	Decision     fulleval.Decision `json:"decision"`
	Rationale    string            `json:"rationale"`
	ChangedFiles []string          `json:"changedFiles"`
	Patch        string            `json:"patch"`
}

type plan struct {
	Decision            fulleval.Decision `json:"decision"`
	Rationale           string            `json:"rationale"`
	FilesLikelyAffected []string          `json:"filesLikelyAffected"`
	Steps               []string          `json:"steps"`
}

type implementation struct {
	Summary      string   `json:"summary"`
	ChangedFiles []string `json:"changedFiles"`
	Patch        string   `json:"patch"`
}

type assessment struct {
	Approved         bool     `json:"approved"`
	RequiresApproval bool     `json:"requiresApproval"`
	Findings         []string `json:"findings"`
}

func (c *core) execute(ctx context.Context, evalCase fulleval.Case, mode fulleval.Mode) (observation fulleval.Observation, infrastructureErr error) {
	started := time.Now()
	observation = fulleval.Observation{CaseID: evalCase.ID, Outcome: "failed", ChangedFiles: []string{}, HiddenTestResults: map[string]bool{}, Iterations: 0}
	for _, name := range evalCase.HiddenTests {
		observation.HiddenTestResults[name] = false
	}
	usage := &meter{provider: c.options.Provider, pricing: c.options.Pricing}
	defer func() {
		duration := time.Since(started).Milliseconds()
		cost := usage.cost
		observation.DurationMS = &duration
		observation.CostUSD = &cost
		observation.ModelRequests = usage.requests
		observation.InputTokens = usage.usage.InputTokens
		observation.OutputTokens = usage.usage.OutputTokens
		observation.CachedInputTokens = usage.usage.CachedInputTokens
		observation.CacheWriteInputTokens = usage.usage.CacheWriteInputTokens
		observation.ReasoningTokens = usage.usage.ReasoningTokens
	}()

	workspace, err := c.workspaces.Prepare(ctx, domain.RepositoryRef{Path: c.options.FixtureRepository, BaseRevision: evalCase.FixtureCommit})
	if err != nil {
		return observation, err
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if cleanupErr := c.workspaces.Cleanup(cleanupContext, workspace); cleanupErr != nil {
			observation.Outcome = "failed"
			observation.FailureCode = "workspace_cleanup_failed"
			observation.FailureMessage = redact(cleanupErr.Error(), workspace.Path, c.options)
		}
	}()

	snapshot, err := repositorySnapshot(ctx, workspace.Path, c.options.ContextMaxBytes)
	if err != nil {
		return observation, err
	}
	var chosen solution
	switch mode {
	case fulleval.ModeSingleAgent:
		chosen, err = c.single(ctx, usage, evalCase, snapshot)
	case fulleval.ModePlannerDeveloper, fulleval.ModeForgeFlow:
		chosen, err = c.planned(ctx, usage, evalCase, snapshot)
	default:
		return observation, fmt.Errorf("unsupported eval mode %q", mode)
	}
	if err != nil {
		terminalFailure(&observation, err, workspace.Path, c.options)
		return observation, nil
	}
	observation.Decision = chosen.Decision
	if chosen.Decision != fulleval.DecisionImplement {
		observation.Outcome = "completed"
		if chosen.Decision == fulleval.DecisionRequireApproval || chosen.Decision == fulleval.DecisionClarify {
			observation.HumanIntervention = true
		}
		if chosen.Decision == fulleval.DecisionRequireApproval {
			observation.Outcome = "approval_required"
		}
		if strings.TrimSpace(chosen.Patch) != "" || len(chosen.ChangedFiles) > 0 {
			observation.Outcome = "failed"
			observation.FailureCode = "invalid_decision_output"
			observation.FailureMessage = "non-implementation decision included repository changes"
		}
		c.grade(ctx, evalCase, workspace.Path, &observation)
		return observation, nil
	}

	if err := validateChangeSet(chosen, evalCase); err != nil {
		terminalFailure(&observation, err, workspace.Path, c.options)
		return observation, nil
	}
	if err := applyPatch(ctx, workspace.Path, chosen.Patch); err != nil {
		terminalFailure(&observation, err, workspace.Path, c.options)
		return observation, nil
	}
	observation.PatchApplicable = true
	observation.Iterations = 1
	if err := c.refreshDiff(ctx, workspace, &observation, evalCase); err != nil {
		terminalFailure(&observation, err, workspace.Path, c.options)
		return observation, nil
	}

	testOutput, testErr := runCommand(ctx, workspace.Path, evalCase.ValidationCommand, c.options.CommandTimeout)
	observation.ExplicitTestsPassed = testErr == nil
	observation.BuildPassed = testErr == nil

	assessmentBlocked := false
	if mode == fulleval.ModeForgeFlow {
		for observation.Iterations <= evalCase.Budget.MaxIterations {
			diff, diffErr := c.workspaces.Diff(ctx, workspace)
			if diffErr != nil {
				terminalFailure(&observation, diffErr, workspace.Path, c.options)
				return observation, nil
			}
			review, reviewErr := c.assess(ctx, usage, "reviewer", evalCase, diff.Patch, testOutput, reviewerSchema)
			security, securityErr := c.assess(ctx, usage, "security", evalCase, diff.Patch, testOutput, securitySchema)
			if reviewErr != nil || securityErr != nil {
				terminalFailure(&observation, errors.Join(reviewErr, securityErr), workspace.Path, c.options)
				return observation, nil
			}
			if security.RequiresApproval {
				observation.HumanIntervention = true
				observation.Outcome = "approval_required"
				break
			}
			assessmentBlocked = !review.Approved || !security.Approved
			if testErr == nil && review.Approved && security.Approved {
				assessmentBlocked = false
				break
			}
			if observation.Iterations >= evalCase.Budget.MaxIterations {
				break
			}
			currentSnapshot, snapshotErr := repositorySnapshot(ctx, workspace.Path, c.options.ContextMaxBytes)
			if snapshotErr != nil {
				terminalFailure(&observation, snapshotErr, workspace.Path, c.options)
				return observation, nil
			}
			repaired, repairErr := c.develop(ctx, usage, evalCase, plan{Decision: fulleval.DecisionImplement, Rationale: "repair deterministic failures", FilesLikelyAffected: observation.ChangedFiles, Steps: append(review.Findings, security.Findings...)}, currentSnapshot, testOutput)
			if repairErr != nil {
				terminalFailure(&observation, repairErr, workspace.Path, c.options)
				return observation, nil
			}
			if err := validateChangeSet(repaired, evalCase); err != nil {
				terminalFailure(&observation, err, workspace.Path, c.options)
				return observation, nil
			}
			if err := applyPatch(ctx, workspace.Path, repaired.Patch); err != nil {
				terminalFailure(&observation, err, workspace.Path, c.options)
				return observation, nil
			}
			observation.Iterations++
			if err := c.refreshDiff(ctx, workspace, &observation, evalCase); err != nil {
				terminalFailure(&observation, err, workspace.Path, c.options)
				return observation, nil
			}
			testOutput, testErr = runCommand(ctx, workspace.Path, evalCase.ValidationCommand, c.options.CommandTimeout)
			observation.ExplicitTestsPassed = testErr == nil
			observation.BuildPassed = testErr == nil
		}
	}

	if observation.Outcome != "approval_required" {
		observation.Outcome = "completed"
	}
	if testErr != nil {
		observation.Outcome = "failed"
		observation.FailureCode = "explicit_tests_failed"
		observation.FailureMessage = redact(testErr.Error(), workspace.Path, c.options)
	} else if assessmentBlocked && observation.Outcome != "approval_required" {
		observation.Outcome = "failed"
		observation.FailureCode = "assessment_failed"
		observation.FailureMessage = "review or security findings remained after the repair budget was exhausted"
	}
	c.grade(ctx, evalCase, workspace.Path, &observation)
	return observation, nil
}
