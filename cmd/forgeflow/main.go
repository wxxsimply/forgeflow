package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/application"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/config"
	"forgeflow/internal/domain"
	fulleval "forgeflow/internal/eval"
	"forgeflow/internal/evalexec"
	"forgeflow/internal/model"
	"forgeflow/internal/observability"
	"forgeflow/internal/planner"
	pg "forgeflow/internal/postgres"
	repoharness "forgeflow/internal/repository"
	"forgeflow/migrations"
)

func main() {
	configuration, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ForgeFlow configuration error:", err)
		os.Exit(2)
	}
	logger, err := observability.NewLogger(os.Stderr, configuration.LogLevel, configuration.Environment)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ForgeFlow logger error:", err)
		os.Exit(2)
	}
	slog.SetDefault(logger)

	if err := run(context.Background(), os.Args[1:], configuration); err != nil {
		code := apperror.CodeOf(err)
		logger.Error("command failed", "error_code", code, "error", err)
		fmt.Fprintf(os.Stderr, "ForgeFlow error [%s]: %s\n", code, apperror.MessageOf(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, configuration config.Config) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		printHelp()
		return nil
	}
	switch args[0] {
	case "inspect":
		return runInspect(ctx, args[1:])
	case "eval":
		return runEval(ctx, args[1:], configuration)
	case "plan":
		return runPlan(ctx, args[1:], configuration)
	case "show":
		return runShow(ctx, args[1:], configuration)
	case "approve":
		return runApproval(ctx, args[1:], true, configuration)
	case "reject":
		return runApproval(ctx, args[1:], false, configuration)
	case "cancel":
		return runCancel(ctx, args[1:], configuration)
	case "pause":
		return runPause(ctx, args[1:], configuration)
	case "resume":
		return runResume(ctx, args[1:], configuration)
	case "db":
		return runDatabase(ctx, args[1:], configuration)
	default:
		return apperror.New(apperror.CodeValidation, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func runPause(ctx context.Context, args []string, configuration config.Config) error {
	set := flag.NewFlagSet("pause", flag.ContinueOnError)
	runID := set.String("run", "", "run id")
	reason := set.String("reason", "", "pause reason")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return apperror.New(apperror.CodeValidation, "--run is required")
	}
	service, err := newService("mock", configuration)
	if err != nil {
		return err
	}
	state, err := service.Pause(ctx, *runID, "cli", *reason)
	if err != nil {
		return err
	}
	return printJSON(state)
}

func runResume(ctx context.Context, args []string, configuration config.Config) error {
	set := flag.NewFlagSet("resume", flag.ContinueOnError)
	runID := set.String("run", "", "run id")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return apperror.New(apperror.CodeValidation, "--run is required")
	}
	service, err := newService("mock", configuration)
	if err != nil {
		return err
	}
	state, err := service.Resume(ctx, *runID)
	if err != nil {
		return err
	}
	return printJSON(state)
}

func runDatabase(ctx context.Context, args []string, configuration config.Config) error {
	if len(args) != 1 || (args[0] != "migrate" && args[0] != "check") {
		return apperror.New(apperror.CodeValidation, "db requires exactly one command: migrate or check")
	}
	if !configuration.PostgresEnabled {
		return apperror.New(apperror.CodeValidation, "PostgreSQL is disabled")
	}
	db, err := openPostgres(ctx, configuration)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeTransient, "cli.db.open", "could not connect to PostgreSQL")
	}
	defer db.Close()
	if args[0] == "migrate" {
		if err := migrations.Apply(ctx, db); err != nil {
			return apperror.Wrap(err, apperror.CodeInternal, "cli.db.migrate", "database migration failed")
		}
	}
	if err := pg.CheckSchema(ctx, db); err != nil {
		return apperror.Wrap(err, apperror.CodeConflict, "cli.db.check", "database schema is not current")
	}
	version, err := migrations.CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"schemaVersion": version, "status": "ready"})
}

func runEval(ctx context.Context, args []string, configuration config.Config) error {
	if len(args) > 0 && args[0] == "execute" {
		return runEvalExecute(ctx, args[1:], configuration)
	}
	set := flag.NewFlagSet("eval", flag.ContinueOnError)
	suite := set.String("suite", "planner/v1", "eval suite")
	evidencePath := set.String("evidence", "", "software/v1 evidence JSON")
	format := set.String("format", "json", "report format: json or markdown")
	output := set.String("output", "", "optional report output path")
	validateOnly := set.Bool("validate-only", false, "validate the fixed dataset without grading evidence")
	fixtureRepository := set.String("fixture-repository", "", "verify every fixture commit in this Git repository")
	limit := set.Int("limit", 0, "number of cases checked in validation mode (0 means all)")
	currentPath := set.String("promote-current", "", "current report JSON for promotion")
	candidatePath := set.String("promote-candidate", "", "candidate report JSON for promotion")
	humanApproved := set.Bool("approve", false, "record explicit human approval after regression checks pass")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *currentPath != "" || *candidatePath != "" {
		return runPromotion(*currentPath, *candidatePath, *humanApproved)
	}
	if *suite == fulleval.SoftwareV1 {
		dataset, err := fulleval.Load(*suite)
		if err != nil {
			return apperror.Wrap(err, apperror.CodeValidation, "eval.dataset", "eval dataset is invalid")
		}
		if *validateOnly {
			checked := len(dataset.Cases)
			if *limit < 0 || *limit > checked {
				return apperror.New(apperror.CodeValidation, "--limit must be between 0 and 30")
			}
			if *limit > 0 {
				checked = *limit
			}
			fixturesVerified := false
			if *fixtureRepository != "" {
				fixtureDataset := dataset
				fixtureDataset.Cases = fixtureDataset.Cases[:checked]
				if err := fulleval.VerifyFixtureCommits(ctx, fixtureDataset, *fixtureRepository); err != nil {
					return apperror.Wrap(err, apperror.CodeValidation, "eval.fixtures", "eval fixture repository is invalid")
				}
				fixturesVerified = true
			}
			return printJSON(map[string]any{"dataset": dataset.Name, "version": dataset.Version, "totalCases": len(dataset.Cases), "checkedCases": checked, "fixturesVerified": fixturesVerified, "status": "valid"})
		}
		if *evidencePath == "" {
			return apperror.New(apperror.CodeValidation, "--evidence is required for software/v1 unless --validate-only is used")
		}
		data, err := os.ReadFile(*evidencePath)
		if err != nil {
			return apperror.Wrap(err, apperror.CodeValidation, "eval.evidence.read", "eval evidence could not be read")
		}
		evidence, err := fulleval.DecodeEvidence(data)
		if err != nil {
			return apperror.Wrap(err, apperror.CodeValidation, "eval.evidence.decode", "eval evidence is invalid")
		}
		report, err := fulleval.BuildComparison(dataset, evidence, time.Now())
		if err != nil {
			return apperror.Wrap(err, apperror.CodeValidation, "eval.report", "eval report could not be generated")
		}
		var encoded []byte
		switch *format {
		case "json":
			encoded, err = json.MarshalIndent(report, "", "  ")
		case "markdown":
			encoded = []byte(report.Markdown())
		default:
			return apperror.New(apperror.CodeValidation, "--format must be json or markdown")
		}
		if err != nil {
			return err
		}
		encoded = append(encoded, '\n')
		if *output != "" {
			return os.WriteFile(*output, encoded, 0o600)
		}
		_, err = os.Stdout.Write(encoded)
		return err
	}
	report, err := planner.RunEvalSuite(*suite)
	if err != nil {
		return err
	}
	if err := printJSON(report); err != nil {
		return err
	}
	if len(report.Failures) > 0 {
		return apperror.New(apperror.CodeModelOutput, "planner eval suite has failures")
	}
	return nil
}

func runEvalExecute(ctx context.Context, args []string, configuration config.Config) error {
	set := flag.NewFlagSet("eval execute", flag.ContinueOnError)
	suite := set.String("suite", fulleval.SoftwareV1, "fixed eval suite")
	fixtureRepository := set.String("fixture-repository", "", "local immutable fixture repository")
	graderRepository := set.String("grader-repository", "", "local private grader repository")
	modesValue := set.String("modes", "single_agent,planner_developer,forgeflow", "comma-separated baseline modes")
	output := set.String("output", filepath.Join(".forgeflow", "evals", "evidence.json"), "private evidence output")
	workspaceRoot := set.String("workspace-root", filepath.Join(".forgeflow", "evals", "workspaces"), "temporary eval worktree root")
	providerName := set.String("provider", "", "model provider identity: openai or deepseek")
	modelName := set.String("model", configuration.DeveloperModel, "same model used by all baselines")
	reasoningEffort := set.String("reasoning-effort", configuration.DeveloperReasoningEffort, "same reasoning effort used by all baselines")
	maxOutputTokens := set.Int("max-output-tokens", configuration.DeveloperMaxOutputTokens, "maximum output tokens per model request")
	callTimeout := set.Duration("call-timeout", configuration.DeveloperTimeout, "timeout per model request")
	commandTimeout := set.Duration("command-timeout", 10*time.Minute, "timeout per explicit or hidden test command")
	contextMaxBytes := set.Int("context-max-bytes", configuration.DeveloperContextMaxBytes, "maximum repository snapshot bytes")
	inputPrice := set.Float64("input-usd-per-million", configuration.PlannerInputUSDPerMTok, "real input token price in USD per million")
	cachedInputPrice := set.Float64("cached-input-usd-per-million", 0, "real cached-input token price in USD per million")
	cacheWritePrice := set.Float64("cache-write-input-usd-per-million", 0, "real cache-write input token price in USD per million")
	outputPrice := set.Float64("output-usd-per-million", configuration.PlannerOutputUSDPerMTok, "real output token price in USD per million")
	pricingMode := set.String("pricing-mode", "", "required pricing mode: cache_hit_miss or cache_read_write")
	pricingSource := set.String("pricing-source", "", "HTTPS pricing source recorded in evidence")
	pricingValidUntil := set.String("pricing-valid-until", "", "RFC3339 deadline before the selected prices change")
	limit := set.Int("limit", 0, "maximum newly executed cases across all modes; 0 runs all remaining")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *suite != fulleval.SoftwareV1 {
		return apperror.New(apperror.CodeValidation, "eval execute currently supports only software/v1")
	}
	if strings.TrimSpace(*fixtureRepository) == "" || strings.TrimSpace(*graderRepository) == "" {
		return apperror.New(apperror.CodeValidation, "--fixture-repository and --grader-repository are required")
	}
	if strings.TrimSpace(configuration.OpenAIAPIKey) == "" {
		return apperror.New(apperror.CodeUnauthorized, "OPENAI_API_KEY is required for the OpenAI-compatible model endpoint")
	}
	if !slices.Contains([]string{"openai", "deepseek"}, strings.TrimSpace(*providerName)) {
		return apperror.New(apperror.CodeValidation, "--provider must be openai or deepseek")
	}
	validUntil, err := time.Parse(time.RFC3339, strings.TrimSpace(*pricingValidUntil))
	if err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "eval.execute.pricing_deadline", "--pricing-valid-until must be an RFC3339 timestamp")
	}
	pricing := evalexec.UsagePricing{
		Mode: evalexec.PricingMode(strings.TrimSpace(*pricingMode)), InputUSDPerMillionTokens: *inputPrice,
		CachedUSDPerMillionTokens: *cachedInputPrice, CacheWriteUSDPerMillion: *cacheWritePrice,
		OutputUSDPerMillionTokens: *outputPrice, Source: strings.TrimSpace(*pricingSource), ValidUntil: validUntil.UTC(),
	}
	if err := pricing.Validate(time.Now().UTC()); err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "eval.execute.pricing", "eval pricing configuration is invalid")
	}
	if *limit < 0 {
		return apperror.New(apperror.CodeValidation, "--limit cannot be negative")
	}
	dataset, err := fulleval.Load(*suite)
	if err != nil {
		return err
	}
	if err := fulleval.VerifyFixtureCommits(ctx, dataset, *fixtureRepository); err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "eval.execute.fixtures", "fixture repository failed preflight")
	}
	rootCommit, err := cleanGitCommit(ctx, ".")
	if err != nil {
		return apperror.Wrap(err, apperror.CodeConflict, "eval.execute.source", "commit the exact eval implementation before running paid baselines")
	}
	fixtureHead, err := gitCommit(ctx, *fixtureRepository)
	if err != nil {
		return err
	}
	graderCommit, err := cleanGitCommit(ctx, *graderRepository)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeConflict, "eval.execute.grader", "private grader must be a clean exact commit")
	}
	modes, err := parseEvalModes(*modesValue)
	if err != nil {
		return err
	}
	provider, err := model.NewOpenAIProvider(model.OpenAIConfig{
		APIKey: configuration.OpenAIAPIKey, BaseURL: configuration.OpenAIBaseURL,
		Organization: configuration.OpenAIOrganization, Project: configuration.OpenAIProject,
		MaxRetries: configuration.OpenAIMaxRetries, ProviderName: strings.TrimSpace(*providerName),
	})
	if err != nil {
		return err
	}
	absoluteFixture, _ := filepath.Abs(*fixtureRepository)
	absoluteGrader, _ := filepath.Abs(*graderRepository)
	absoluteWorkspaces, _ := filepath.Abs(*workspaceRoot)
	absoluteOutput, _ := filepath.Abs(*output)
	executor, err := evalexec.NewMux(evalexec.Options{
		Provider: provider, Pricing: pricing,
		FixtureRepository: absoluteFixture, GraderRepository: absoluteGrader, WorkspaceRoot: absoluteWorkspaces,
		Model: *modelName, ReasoningEffort: *reasoningEffort, MaxOutputTokens: *maxOutputTokens,
		CallTimeout: *callTimeout, CommandTimeout: *commandTimeout, ContextMaxBytes: *contextMaxBytes,
	})
	if err != nil {
		return err
	}
	configurations := make([]fulleval.Configuration, 0, len(modes))
	for _, mode := range modes {
		configurations = append(configurations, evalConfiguration(mode, strings.TrimSpace(*providerName), *modelName, rootCommit, fixtureHead, graderCommit, pricing))
	}
	file, err := fulleval.RunResumable(ctx, fulleval.ResumableOptions{
		Dataset: dataset, Configurations: configurations, Executor: executor,
		Recorder: fulleval.FileRecorder{Path: absoluteOutput}, MaxNewCases: *limit,
		OnCompleted: func(mode fulleval.Mode, caseID string, completed, total int) {
			fmt.Fprintf(os.Stderr, "eval %s %s recorded (%d/%d)\n", mode, caseID, completed, total)
		},
	})
	if err != nil {
		return err
	}
	complete := len(file.Runs) == len(modes)
	observations := 0
	for _, run := range file.Runs {
		observations += len(run.Observations)
		if len(run.Observations) != len(dataset.Cases) {
			complete = false
		}
	}
	return printJSON(map[string]any{"dataset": dataset.Name, "modes": modes, "observations": observations, "complete": complete, "evidence": absoluteOutput})
}

func parseEvalModes(value string) ([]fulleval.Mode, error) {
	result := []fulleval.Mode{}
	seen := map[fulleval.Mode]bool{}
	for _, item := range strings.Split(value, ",") {
		mode := fulleval.Mode(strings.TrimSpace(item))
		if !slices.Contains([]fulleval.Mode{fulleval.ModeSingleAgent, fulleval.ModePlannerDeveloper, fulleval.ModeForgeFlow}, mode) {
			return nil, apperror.New(apperror.CodeValidation, fmt.Sprintf("invalid eval mode %q", mode))
		}
		if seen[mode] {
			return nil, apperror.New(apperror.CodeValidation, fmt.Sprintf("duplicate eval mode %q", mode))
		}
		seen[mode] = true
		result = append(result, mode)
	}
	return result, nil
}

func evalConfiguration(mode fulleval.Mode, providerName, modelName, gitSHA, fixtureSHA, graderSHA string, pricing evalexec.UsagePricing) fulleval.Configuration {
	agents := []string{"single_agent"}
	prompts := map[string]string{"single_agent": "eval/single-agent/v1"}
	if mode == fulleval.ModePlannerDeveloper {
		agents = []string{"planner", "developer"}
		prompts = map[string]string{"planner": "eval/planner/v1", "developer": "eval/developer/v1"}
	} else if mode == fulleval.ModeForgeFlow {
		agents = []string{"planner", "developer", "reviewer", "security"}
		prompts = map[string]string{"planner": "eval/planner/v1", "developer": "eval/developer/v1", "reviewer": "eval/reviewer/v1", "security": "eval/security/v1", "judge": "eval/judge/v1"}
	}
	models := map[string]string{}
	for _, agent := range agents {
		models[agent] = modelName
	}
	prices := map[string]float64{"input": pricing.InputUSDPerMillionTokens, "cachedInput": pricing.CachedUSDPerMillionTokens, "output": pricing.OutputUSDPerMillionTokens}
	if pricing.CacheWriteUSDPerMillion > 0 {
		prices["cacheWriteInput"] = pricing.CacheWriteUSDPerMillion
	}
	return fulleval.Configuration{
		Mode: mode, ModelVersions: models, PromptVersions: prompts,
		PolicyVersion: evalexec.PolicyVersion, ToolVersions: map[string]string{"worktree": evalexec.ToolVersion, "apply_patch": evalexec.ToolVersion, "run_test": evalexec.ToolVersion, "hidden_grader": evalexec.ToolVersion},
		GitCommit: gitSHA, FixtureRepositoryCommit: fixtureSHA, GraderCommit: graderSHA,
		ExecutionEnvironment: runtime.GOOS + "/" + runtime.GOARCH + " " + runtime.Version(),
		ModelProvider:        providerName, PricingMode: string(pricing.Mode), PricingSource: pricing.Source,
		PricingValidUntil: pricing.ValidUntil.UTC().Format(time.RFC3339), PricingUSDPerMTok: prices,
	}
}

func cleanGitCommit(ctx context.Context, directory string) (string, error) {
	command := exec.CommandContext(ctx, "git", "-C", directory, "status", "--porcelain")
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(output)) != "" {
		return "", fmt.Errorf("git worktree is not clean")
	}
	return gitCommit(ctx, directory)
}

func gitCommit(ctx context.Context, directory string) (string, error) {
	command := exec.CommandContext(ctx, "git", "-C", directory, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve Git commit for %s: %w", directory, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func runPromotion(currentPath, candidatePath string, humanApproved bool) error {
	if currentPath == "" || candidatePath == "" {
		return apperror.New(apperror.CodeValidation, "both --promote-current and --promote-candidate are required")
	}
	load := func(path string) (fulleval.Report, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return fulleval.Report{}, err
		}
		var envelope struct {
			SchemaVersion string `json:"schemaVersion"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fulleval.Report{}, err
		}
		switch envelope.SchemaVersion {
		case "forgeflow.eval.report/v1":
			var report fulleval.Report
			if err := json.Unmarshal(data, &report); err != nil {
				return fulleval.Report{}, err
			}
			return report, nil
		case "forgeflow.eval.comparison/v1":
			var comparison fulleval.ComparisonReport
			if err := json.Unmarshal(data, &comparison); err != nil {
				return fulleval.Report{}, err
			}
			for _, report := range comparison.Reports {
				if report.Configuration.Mode == fulleval.ModeForgeFlow {
					return report, nil
				}
			}
			return fulleval.Report{}, fmt.Errorf("comparison report has no forgeflow mode")
		default:
			return fulleval.Report{}, fmt.Errorf("unsupported report schema %q", envelope.SchemaVersion)
		}
	}
	current, err := load(currentPath)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "eval.promotion.current", "current report is invalid")
	}
	candidate, err := load(candidatePath)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "eval.promotion.candidate", "candidate report is invalid")
	}
	decision := fulleval.CheckPromotion(current, candidate, fulleval.DefaultPromotionThresholds())
	if err := printJSON(decision); err != nil {
		return err
	}
	if !decision.Allowed {
		return apperror.New(apperror.CodeConflict, "prompt/model promotion blocked by eval regression gate")
	}
	if !humanApproved {
		return apperror.New(apperror.CodeApprovalNeeded, "regression gate passed; explicit --approve is still required")
	}
	return nil
}

func runInspect(ctx context.Context, args []string) error {
	set := flag.NewFlagSet("inspect", flag.ContinueOnError)
	repositoryPath := set.String("repository", ".", "target repository path")
	base := set.String("base", "HEAD", "base revision")
	if err := set.Parse(args); err != nil {
		return err
	}
	inspector := repoharness.NewGitInspector(repoharness.DefaultLimits())
	summary, err := inspector.Inspect(ctx, domain.RepositoryRef{Path: *repositoryPath, BaseRevision: *base})
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func runPlan(ctx context.Context, args []string, configuration config.Config) error {
	set := flag.NewFlagSet("plan", flag.ContinueOnError)
	task := set.String("task", "", "software delivery task")
	repository := set.String("repository", ".", "target repository path")
	base := set.String("base", "HEAD", "base revision")
	mode := set.String("mode", configuration.PlannerMode, "planner mode")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return apperror.New(apperror.CodeValidation, "--task is required")
	}
	absoluteRepository, err := filepath.Abs(*repository)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeValidation, "cli.plan.repository", "repository path is invalid")
	}
	service, err := newService(*mode, configuration)
	if err != nil {
		return err
	}
	state, err := service.Create(ctx, application.CreateInput{
		Task: *task, RepositoryPath: absoluteRepository, BaseRevision: *base,
	})
	if err != nil {
		return err
	}
	return printJSON(state)
}

func runShow(ctx context.Context, args []string, configuration config.Config) error {
	set := flag.NewFlagSet("show", flag.ContinueOnError)
	runID := set.String("run", "", "run id")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return apperror.New(apperror.CodeValidation, "--run is required")
	}
	service, err := newService("mock", configuration)
	if err != nil {
		return err
	}
	state, err := service.Get(ctx, *runID)
	if err != nil {
		return err
	}
	return printJSON(state)
}

func runApproval(ctx context.Context, args []string, approve bool, configuration config.Config) error {
	set := flag.NewFlagSet("approval", flag.ContinueOnError)
	runID := set.String("run", "", "run id")
	comment := set.String("comment", "", "approval comment")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return apperror.New(apperror.CodeValidation, "--run is required")
	}
	service, err := newService("mock", configuration)
	if err != nil {
		return err
	}
	state, err := service.ResolveApproval(ctx, *runID, approve, *comment)
	if err != nil {
		return err
	}
	return printJSON(state)
}

func runCancel(ctx context.Context, args []string, configuration config.Config) error {
	set := flag.NewFlagSet("cancel", flag.ContinueOnError)
	runID := set.String("run", "", "run id")
	reason := set.String("reason", "", "cancellation reason")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *runID == "" {
		return apperror.New(apperror.CodeValidation, "--run is required")
	}
	service, err := newService("mock", configuration)
	if err != nil {
		return err
	}
	state, err := service.Cancel(ctx, *runID, "cli", *reason)
	if err != nil {
		return err
	}
	return printJSON(state)
}

func newService(mode string, configuration config.Config) (*application.Service, error) {
	planAgent, err := planner.New(mode, planner.Options{
		Inspector: repoharness.NewGitInspector(repoharness.DefaultLimits()),
		APIKey:    configuration.OpenAIAPIKey, OpenAIBaseURL: configuration.OpenAIBaseURL,
		ModelProvider:      configuration.ModelProvider,
		OpenAIOrganization: configuration.OpenAIOrganization, OpenAIProject: configuration.OpenAIProject,
		OpenAIMaxRetries: configuration.OpenAIMaxRetries, Model: configuration.PlannerModel,
		PromptVersion: configuration.PlannerPromptVersion, ReasoningEffort: configuration.PlannerReasoningEffort,
		MaxOutputTokens: configuration.PlannerMaxOutputTokens, Timeout: configuration.PlannerTimeout,
		Pricing: model.Pricing{
			InputUSDPerMillionTokens:  configuration.PlannerInputUSDPerMTok,
			OutputUSDPerMillionTokens: configuration.PlannerOutputUSDPerMTok,
		},
	})
	if err != nil {
		return nil, err
	}
	var store checkpoint.Store
	if configuration.PostgresEnabled {
		db, err := openPostgres(context.Background(), configuration)
		if err != nil {
			return nil, apperror.Wrap(err, apperror.CodeTransient, "cli.postgres.open", "could not connect to PostgreSQL")
		}
		if err := pg.CheckSchema(context.Background(), db); err != nil {
			_ = db.Close()
			return nil, apperror.Wrap(err, apperror.CodeConflict, "cli.postgres.schema", "database schema is not current")
		}
		store = checkpoint.NewPostgresStore(db)
	} else {
		store = checkpoint.NewFileStore(filepath.Join(configuration.DataDir, "runs"))
	}
	return application.NewService(store, planAgent), nil
}

func openPostgres(ctx context.Context, configuration config.Config) (*sql.DB, error) {
	return pg.Open(ctx, pg.Config{
		DSN: configuration.PostgresDSN, MaxOpenConns: configuration.PostgresMaxOpenConns,
		MaxIdleConns: configuration.PostgresMaxIdleConns, ConnMaxLifetime: configuration.PostgresConnMaxLifetime,
		PingTimeout: configuration.PostgresPingTimeout,
	})
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printHelp() {
	fmt.Print(`ForgeFlow v0.1 (Go)

Commands:
  inspect [--repository <path>] [--base <ref>]
  eval    [--suite planner/v1]
  eval    --suite software/v1 --validate-only [--limit 6] [--fixture-repository <path>]
  eval    execute --suite software/v1 --fixture-repository <path> --grader-repository <private-path> --modes single_agent,planner_developer,forgeflow --output <private-evidence.json>
  eval    --suite software/v1 --evidence <file> [--format json|markdown] [--output <file>]
  eval    --promote-current <report.json> --promote-candidate <report.json> --approve
  plan    --task <text> [--repository <path>] [--base <ref>] [--mode mock]
  show    --run <runId>
  approve --run <runId> [--comment <text>]
  reject  --run <runId> [--comment <text>]
  cancel  --run <runId> [--reason <text>]
  pause   --run <runId> [--reason <text>]
  resume  --run <runId>
  db      migrate|check
`)
}
