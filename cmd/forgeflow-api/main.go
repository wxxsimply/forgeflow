package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"forgeflow/internal/application"
	"forgeflow/internal/auth"
	"forgeflow/internal/buildinfo"
	"forgeflow/internal/checkpoint"
	"forgeflow/internal/config"
	"forgeflow/internal/controlplane"
	"forgeflow/internal/governance"
	"forgeflow/internal/httpapi"
	"forgeflow/internal/observability"
	"forgeflow/internal/planner"
	pg "forgeflow/internal/postgres"
	"forgeflow/internal/repository"
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
	if err := run(ctx, configuration); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configuration config.Config) error {
	if !configuration.PostgresEnabled {
		return fmt.Errorf("FORGEFLOW_POSTGRES_ENABLED=true is required for the API")
	}
	if strings.TrimSpace(configuration.OpenAIAPIKey) != "" {
		return fmt.Errorf("OPENAI_API_KEY must not be present in the API process; configure it only for workers")
	}
	telemetry, err := observability.NewTelemetry(ctx, observability.Options{ServiceName: "forgeflow-api", Version: configuration.ServiceVersion, Environment: configuration.Environment, OTLPEndpoint: configuration.OTLPEndpoint, SampleRatio: configuration.OTELSampleRatio, Metrics: configuration.MetricsEnabled})
	if err != nil {
		return err
	}
	defer shutdownTelemetry(telemetry)
	db, err := pg.Open(ctx, pg.Config{DSN: configuration.PostgresDSN, MaxOpenConns: configuration.PostgresMaxOpenConns, MaxIdleConns: configuration.PostgresMaxIdleConns, ConnMaxLifetime: configuration.PostgresConnMaxLifetime, PingTimeout: configuration.PostgresPingTimeout})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := pg.CheckSchema(ctx, db); err != nil {
		return err
	}
	authStore := auth.NewPostgresStore(db)
	authService, err := auth.NewService(authStore, auth.Options{SessionTTL: configuration.SessionTTL, IdleTTL: configuration.SessionIdleTTL})
	if err != nil {
		return err
	}
	if configuration.BootstrapAdminEmail != "" {
		count, err := authStore.CountUsers(ctx)
		if err != nil {
			return err
		}
		if count == 0 {
			user, err := authService.BootstrapAdmin(ctx, configuration.BootstrapAdminEmail, configuration.BootstrapAdminPassword)
			if err != nil {
				return err
			}
			slog.Info("bootstrap administrator created", "user_id", user.ID)
		}
	}
	checkpointStore := checkpoint.NewPostgresStore(db)
	runService := application.NewService(checkpointStore, planner.Mock{})
	catalog, err := governance.NewCatalog(configuration)
	if err != nil {
		return err
	}
	api, err := httpapi.New(httpapi.Options{Auth: authService, Control: controlplane.NewStore(db), Runs: runService, Inspector: repository.NewGitInspector(repository.DefaultLimits()), CookieSecure: configuration.HTTPCookieSecure, CookieDomain: configuration.HTTPCookieDomain, CookieMaxAge: configuration.SessionTTL, AllowedOrigins: configuration.HTTPAllowedOrigins, RepositoryRoots: configuration.RepositoryRoots, MetricsEnabled: configuration.MetricsEnabled, ServiceVersion: configuration.ServiceVersion, GitCommit: buildinfo.Commit, Governance: governance.NewStore(db), Catalog: catalog})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: configuration.HTTPAddress, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errorsChannel := make(chan error, 1)
	go func() {
		slog.Info("ForgeFlow API listening", "address", configuration.HTTPAddress)
		errorsChannel <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func shutdownTelemetry(telemetry *observability.Telemetry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := telemetry.Shutdown(ctx); err != nil {
		slog.Warn("telemetry shutdown failed", "error", err)
	}
}
