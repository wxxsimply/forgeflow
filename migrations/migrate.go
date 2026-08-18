package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed *.up.sql *.down.sql
var files embed.FS

const advisoryLockID int64 = 7_308_441_907_124_661_111

type Migration struct {
	Version int64
	Name    string
	Up      string
	Down    string
}

func List() ([]Migration, error) {
	entries, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	migrations := make([]Migration, 0, len(entries))
	for _, upName := range entries {
		parts := strings.SplitN(upName, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("migration %q does not start with a numeric version", upName)
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration %q has an invalid version", upName)
		}
		downName := strings.TrimSuffix(upName, ".up.sql") + ".down.sql"
		up, err := files.ReadFile(upName)
		if err != nil {
			return nil, err
		}
		down, err := files.ReadFile(downName)
		if err != nil {
			return nil, fmt.Errorf("migration %q has no down file: %w", upName, err)
		}
		migrations = append(migrations, Migration{Version: version, Name: strings.TrimSuffix(parts[1], ".up.sql"), Up: string(up), Down: string(down)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for index := 1; index < len(migrations); index++ {
		if migrations[index-1].Version == migrations[index].Version {
			return nil, fmt.Errorf("migration version %d is duplicated", migrations[index].Version)
		}
	}
	return migrations, nil
}

func Apply(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version bigint PRIMARY KEY,
        name text NOT NULL,
        applied_at timestamptz NOT NULL DEFAULT now()
    )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	migrationList, err := List()
	if err != nil {
		return err
	}
	for _, migration := range migrationList {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.Version).Scan(&exists); err != nil {
			return fmt.Errorf("read migration version %d: %w", migration.Version, err)
		}
		if exists {
			continue
		}
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryLockID); err == nil {
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.Version).Scan(&exists); err == nil && exists {
				if err := tx.Commit(); err != nil {
					return fmt.Errorf("commit migration recheck %d: %w", migration.Version, err)
				}
				continue
			}
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, migration.Up)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name) VALUES ($1, $2)`, migration.Version, migration.Name)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d_%s: %w", migration.Version, migration.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.Version, err)
		}
	}
	return nil
}

func CurrentVersion(ctx context.Context, db *sql.DB) (int64, error) {
	var version sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version.Int64, nil
}

func LatestVersion() (int64, error) {
	all, err := List()
	if err != nil || len(all) == 0 {
		return 0, err
	}
	return all[len(all)-1].Version, nil
}
