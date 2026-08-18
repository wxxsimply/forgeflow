package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"forgeflow/internal/apperror"
	"forgeflow/internal/application"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/config"
	"forgeflow/internal/domain"
	fulleval "forgeflow/internal/eval"
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
		return runEval(ctx, args[1:])
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

func runEval(ctx context.Context, args []string) error {
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
