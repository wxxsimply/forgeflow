package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"forgeflow/internal/application"
	"forgeflow/internal/assessment"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/config"
	"forgeflow/internal/developer"
	"forgeflow/internal/domain"
	"forgeflow/internal/graph"
	"forgeflow/internal/model"
	"forgeflow/internal/observability"
	"forgeflow/internal/planner"
	"forgeflow/internal/policy"
	pg "forgeflow/internal/postgres"
	"forgeflow/internal/queue"
	"forgeflow/internal/repository"
	"forgeflow/internal/reviewer"
	"forgeflow/internal/sandbox"
	"forgeflow/internal/security"
	toolruntime "forgeflow/internal/tool"
	"forgeflow/internal/worker"
)

func main() {
	configuration, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	logger, err := observability.NewLogger(os.Stderr, configuration.LogLevel, configuration.Environment)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, configuration); err != nil && ctx.Err() == nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configuration config.Config) error {
	if !configuration.PostgresEnabled {
		return fmt.Errorf("FORGEFLOW_POSTGRES_ENABLED=true is required for the worker")
	}
	telemetry, err := observability.NewTelemetry(ctx, observability.Options{ServiceName: "forgeflow-worker", Version: configuration.ServiceVersion, Environment: configuration.Environment, OTLPEndpoint: configuration.OTLPEndpoint, SampleRatio: configuration.OTELSampleRatio, Metrics: configuration.MetricsEnabled})
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetry)
	db, err := pg.Open(ctx, pg.Config{
		DSN: configuration.PostgresDSN, MaxOpenConns: configuration.PostgresMaxOpenConns,
		MaxIdleConns: configuration.PostgresMaxIdleConns, ConnMaxLifetime: configuration.PostgresConnMaxLifetime,
		PingTimeout: configuration.PostgresPingTimeout,
	})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := pg.CheckSchema(ctx, db); err != nil {
		return err
	}
	provider, err := workerModelProvider(configuration)
	if err != nil {
		return err
	}
	planAgent, err := planner.New(configuration.PlannerMode, planner.Options{
		Provider:  provider,
		Inspector: repository.NewGitInspector(repository.DefaultLimits()), APIKey: configuration.OpenAIAPIKey,
		OpenAIBaseURL: configuration.OpenAIBaseURL, OpenAIOrganization: configuration.OpenAIOrganization,
		OpenAIProject: configuration.OpenAIProject, OpenAIMaxRetries: configuration.OpenAIMaxRetries,
		Model: configuration.PlannerModel, PromptVersion: configuration.PlannerPromptVersion,
		ReasoningEffort: configuration.PlannerReasoningEffort, MaxOutputTokens: configuration.PlannerMaxOutputTokens,
		Timeout: configuration.PlannerTimeout, Pricing: model.Pricing{
			InputUSDPerMillionTokens: configuration.PlannerInputUSDPerMTok, OutputUSDPerMillionTokens: configuration.PlannerOutputUSDPerMTok,
		},
	})
	if err != nil {
		return err
	}
	store := checkpoint.NewPostgresStore(db)
	service, err := workerApplication(configuration, store, planAgent, provider)
	if err != nil {
		return err
	}
	jobQueue := queue.NewPostgresQueue(db)
	hostname, _ := os.Hostname()
	workerID := hostname + "-" + domain.NewID()
	processor, err := worker.New(worker.Options{
		ID: workerID, Queue: jobQueue, StateSource: store,
		LeaseTTL: configuration.WorkerLeaseTTL, HeartbeatInterval: configuration.WorkerHeartbeatInterval,
		EmptyPollInterval: configuration.WorkerPollInterval,
		Handler: worker.HandlerFunc(func(jobContext context.Context, leased queue.LeasedJob) error {
			var payload struct {
				RunID string `json:"runId"`
			}
			if err := json.Unmarshal(leased.Payload, &payload); err != nil || payload.RunID == "" {
				return fmt.Errorf("invalid run wakeup payload")
			}
			state, err := service.Get(jobContext, payload.RunID)
			if err != nil {
				return err
			}
			if !workerShouldExecute(state) {
				return nil
			}
			_, err = service.Continue(jobContext, payload.RunID)
			return err
		}),
	})
	if err != nil {
		return err
	}
	dispatchErrors := make(chan error, 1)
	go func() { dispatchErrors <- dispatchLoop(ctx, jobQueue, configuration.WorkerPollInterval) }()
	workerErrors := make(chan error, 1)
	go func() { workerErrors <- processor.Run(ctx) }()
	statusServer := &http.Server{Addr: configuration.WorkerMetricsAddress, Handler: workerStatusHandler(), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
	statusErrors := make(chan error, 1)
	go func() {
		slog.Info("ForgeFlow worker status server listening", "address", configuration.WorkerMetricsAddress)
		statusErrors <- statusServer.ListenAndServe()
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = statusServer.Shutdown(shutdown)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-dispatchErrors:
		return err
	case err := <-workerErrors:
		return err
	case err := <-statusErrors:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func workerShouldExecute(state *domain.RunState) bool {
	if state == nil || state.Status.Terminal() || state.Status == domain.StatusPaused {
		return false
	}
	if state.Cancellation.Requested() || state.Pause.Requested() {
		return true
	}
	if state.Status == domain.StatusWaitingPlanApproval || state.Status == domain.StatusWaitingActionApproval {
		return state.PendingApproval != nil && state.PendingApproval.Status != domain.ApprovalPending
	}
	return true
}

func workerModelProvider(configuration config.Config) (model.Provider, error) {
	if configuration.PlannerMode != "openai" {
		return nil, nil
	}
	return model.NewOpenAIProvider(model.OpenAIConfig{
		APIKey: configuration.OpenAIAPIKey, BaseURL: configuration.OpenAIBaseURL,
		Organization: configuration.OpenAIOrganization, Project: configuration.OpenAIProject,
		MaxRetries: configuration.OpenAIMaxRetries,
	})
}

func workerApplication(configuration config.Config, store checkpoint.Store, planAgent planner.Planner, provider model.Provider) (*application.Service, error) {
	if configuration.WorkflowMode == "planning" {
		return application.NewService(store, planAgent), nil
	}
	if provider == nil {
		return nil, fmt.Errorf("development workflow requires a model provider")
	}
	limits := repository.DefaultLimits()
	workspaces, err := repository.NewGitWorkspaceManager(configuration.SandboxWorkspaceRoot, limits)
	if err != nil {
		return nil, err
	}
	runner, err := sandbox.NewDockerRunner(sandbox.DockerConfig{
		Enabled: configuration.DockerEnabled, Binary: configuration.DockerBinary,
		WorkspaceRoot: configuration.SandboxWorkspaceRoot, AllowedImages: []string{configuration.SandboxImage},
		CPUs: configuration.SandboxCPUs, Memory: configuration.SandboxMemory, PIDsLimit: configuration.SandboxPIDsLimit,
		TmpfsSizeBytes: int64(configuration.SandboxTmpfsBytes), MaxOutputBytes: int64(configuration.SandboxMaxOutputBytes), MaxTimeout: configuration.SandboxTimeout,
	})
	if err != nil {
		return nil, err
	}
	registry := toolruntime.NewRegistry()
	if err := toolruntime.RegisterMutationTools(registry, toolruntime.DefaultPatchLimits()); err != nil {
		return nil, err
	}
	if err := toolruntime.RegisterCommandTools(registry, runner, configuration.SandboxImage); err != nil {
		return nil, err
	}
	tools, err := toolruntime.NewRuntime(registry, policy.DefaultEngine())
	if err != nil {
		return nil, err
	}
	developerAgent, err := developer.NewAgent(developer.Options{
		Provider: provider, ContextBuilder: developer.NewContextBuilder(limits, configuration.DeveloperContextMaxBytes),
		Model: configuration.DeveloperModel, PromptVersion: configuration.DeveloperPromptVersion,
		ReasoningEffort: configuration.DeveloperReasoningEffort, MaxOutputTokens: configuration.DeveloperMaxOutputTokens,
		Timeout: configuration.DeveloperTimeout,
	})
	if err != nil {
		return nil, err
	}
	reviewerAgent, err := reviewer.NewAgent(reviewer.Options{
		Provider: provider, ContextBuilder: assessment.NewContextBuilder(limits, configuration.ReviewerContextMaxBytes),
		Model: configuration.ReviewerModel, PromptVersion: configuration.ReviewerPromptVersion,
		ReasoningEffort: configuration.ReviewerReasoningEffort, MaxOutputTokens: configuration.ReviewerMaxOutputTokens,
		Timeout: configuration.ReviewerTimeout,
	})
	if err != nil {
		return nil, err
	}
	securityAgent, err := security.NewAgent(security.Options{
		Provider: provider, ContextBuilder: assessment.NewContextBuilder(limits, configuration.SecurityContextMaxBytes),
		Model: configuration.SecurityModel, PromptVersion: configuration.SecurityPromptVersion,
		ReasoningEffort: configuration.SecurityReasoningEffort, MaxOutputTokens: configuration.SecurityMaxOutputTokens,
		Timeout: configuration.SecurityTimeout,
	})
	if err != nil {
		return nil, err
	}
	definition, err := graph.DevelopmentDefinition(graph.DevelopmentOptions{
		Planner: planAgent, Developer: developerAgent, Reviewer: reviewerAgent, Security: securityAgent,
		WorkspaceManager: workspaces, ToolRuntime: tools,
	})
	if err != nil {
		return nil, err
	}
	return application.NewServiceWithDefinition(store, definition), nil
}

func workerStatusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET /metrics", observability.DefaultMetrics().Handler())
	return mux
}

func shutdownTelemetry(telemetry *observability.Telemetry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := telemetry.Shutdown(ctx); err != nil {
		slog.Warn("telemetry shutdown failed", "error", err)
	}
}

func dispatchLoop(ctx context.Context, jobQueue *queue.PostgresQueue, interval time.Duration) error {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := jobQueue.DispatchOutbox(ctx, 100, 5); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
