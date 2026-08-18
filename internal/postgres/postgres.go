package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"forgeflow/migrations"
)

type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	PingTimeout     time.Duration
}

func Open(ctx context.Context, config Config) (*sql.DB, error) {
	if strings.TrimSpace(config.DSN) == "" {
		return nil, fmt.Errorf("PostgreSQL DSN is required")
	}
	if config.MaxOpenConns <= 0 {
		config.MaxOpenConns = 20
	}
	if config.MaxIdleConns < 0 || config.MaxIdleConns > config.MaxOpenConns {
		return nil, fmt.Errorf("PostgreSQL idle connection limit is invalid")
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 5
	}
	if config.ConnMaxLifetime <= 0 {
		config.ConnMaxLifetime = 30 * time.Minute
	}
	if config.PingTimeout <= 0 {
		config.PingTimeout = 5 * time.Second
	}
	db, err := sql.Open("pgx", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	pingContext, cancel := context.WithTimeout(ctx, config.PingTimeout)
	defer cancel()
	if err := db.PingContext(pingContext); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return db, nil
}

func CheckSchema(ctx context.Context, db *sql.DB) error {
	current, err := migrations.CurrentVersion(ctx, db)
	if err != nil {
		return err
	}
	latest, err := migrations.LatestVersion()
	if err != nil {
		return err
	}
	if current != latest {
		return fmt.Errorf("database schema version is %d, expected %d; run migrations explicitly", current, latest)
	}
	return nil
}
