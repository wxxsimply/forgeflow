package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"strings"

	"forgeflow/internal/apperror"
	"forgeflow/internal/artifact"
	"forgeflow/internal/config"
	pg "forgeflow/internal/postgres"
)

func runArtifactMigration(ctx context.Context, args []string, configuration config.Config) error {
	if len(args) == 0 || args[0] != "migrate" {
		return apperror.New(apperror.CodeValidation, "artifact requires the migrate command")
	}
	set := flag.NewFlagSet("artifact migrate", flag.ContinueOnError)
	runID := set.String("run", "", "run id to migrate")
	if err := set.Parse(args[1:]); err != nil {
		return err
	}
	normalizedRunID := strings.TrimSpace(*runID)
	if normalizedRunID == "" {
		return apperror.New(apperror.CodeValidation, "--run is required")
	}
	if !configuration.PostgresEnabled {
		return apperror.New(apperror.CodeValidation, "PostgreSQL is required for Artifact migration")
	}
	if configuration.ArtifactBackend != "s3" {
		return apperror.New(apperror.CodeValidation, "FORGEFLOW_ARTIFACT_BACKEND=s3 is required for migration")
	}
	db, err := openPostgres(ctx, configuration)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeTransient, "artifact.migrate.postgres", "could not connect to PostgreSQL")
	}
	defer db.Close()
	if err := pg.CheckSchema(ctx, db); err != nil {
		return apperror.Wrap(err, apperror.CodeConflict, "artifact.migrate.schema", "database schema is not current")
	}
	var ownerID string
	if err := db.QueryRowContext(ctx, `SELECT owner_id::text FROM runs WHERE id = $1`, normalizedRunID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.Wrap(err, apperror.CodeNotFound, "artifact.migrate.run", "run was not found")
		}
		return apperror.Wrap(err, apperror.CodeTransient, "artifact.migrate.run", "could not read the run owner")
	}
	metadata := artifact.NewPostgresMetadata(db)
	source, err := artifact.NewFileStore(configuration.ArtifactRoot, metadata, int64(configuration.ArtifactMaxBytes))
	if err != nil {
		return fmt.Errorf("open source Artifact store: %w", err)
	}
	destinationStore, err := artifact.NewConfiguredStore(ctx, configuration, metadata)
	if err != nil {
		return fmt.Errorf("open destination Artifact store: %w", err)
	}
	destination, ok := destinationStore.(*artifact.S3Store)
	if !ok {
		return fmt.Errorf("configured Artifact destination is not S3")
	}
	if err := destination.Check(ctx); err != nil {
		return fmt.Errorf("Artifact destination preflight failed: %w", err)
	}
	result, err := destination.MigrateRun(ctx, source, ownerID, normalizedRunID)
	if err != nil {
		return apperror.Wrap(err, apperror.CodeInternal, "artifact.migrate", "Artifact migration failed")
	}
	return printJSON(result)
}
