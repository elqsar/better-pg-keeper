// Package sqlite provides SQLite storage implementation for pganalyzer.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents a single database migration.
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

// MigrationRecord represents a record of an applied migration.
type MigrationRecord struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

// parseMigration parses a migration file content into up and down SQL.
func parseMigration(content string) (up, down string) {
	lines := strings.Split(content, "\n")
	var upLines, downLines []string
	var inUp, inDown bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- +migrate Up") {
			inUp = true
			inDown = false
			continue
		}
		if strings.HasPrefix(trimmed, "-- +migrate Down") {
			inUp = false
			inDown = true
			continue
		}
		if inUp {
			upLines = append(upLines, line)
		}
		if inDown {
			downLines = append(downLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(upLines, "\n")),
		strings.TrimSpace(strings.Join(downLines, "\n"))
}

// loadMigrations loads all migrations from the embedded filesystem.
func loadMigrations() ([]Migration, error) {
	var migrations []Migration

	err := fs.WalkDir(migrationsFS, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}

		content, err := migrationsFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", path, err)
		}

		name := filepath.Base(path)
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parsing version from %s: %w", name, err)
		}

		upSQL, downSQL := parseMigration(string(content))

		migrations = append(migrations, Migration{
			Version: version,
			Name:    strings.TrimSuffix(name, ".sql"),
			UpSQL:   upSQL,
			DownSQL: downSQL,
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// ensureMigrationsTable creates the migrations tracking table if it doesn't exist.
func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS _migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

// getAppliedMigrations returns a map of applied migration versions.
func getAppliedMigrations(ctx context.Context, db *sql.DB) (map[int]MigrationRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name, applied_at FROM _migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]MigrationRecord)
	for rows.Next() {
		var rec MigrationRecord
		if err := rows.Scan(&rec.Version, &rec.Name, &rec.AppliedAt); err != nil {
			return nil, err
		}
		applied[rec.Version] = rec
	}

	return applied, rows.Err()
}

// Migrate applies all pending migrations to the database.
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("ensuring migrations table: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}

	applied, err := getAppliedMigrations(ctx, db)
	if err != nil {
		return fmt.Errorf("getting applied migrations: %w", err)
	}

	for _, m := range migrations {
		if _, ok := applied[m.Version]; ok {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("starting transaction for migration %d: %w", m.Version, err)
		}

		if _, err := tx.ExecContext(ctx, m.UpSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying migration %d (%s): %w", m.Version, m.Name, err)
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO _migrations (version, name) VALUES (?, ?)`,
			m.Version, m.Name); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", m.Version, err)
		}
	}

	return nil
}

// Rollback rolls back the last applied migration.
func Rollback(ctx context.Context, db *sql.DB) error {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return fmt.Errorf("ensuring migrations table: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}

	// Find the last applied migration
	var lastVersion int
	err = db.QueryRowContext(ctx, `SELECT version FROM _migrations ORDER BY version DESC LIMIT 1`).Scan(&lastVersion)
	if err == sql.ErrNoRows {
		return nil // Nothing to rollback
	}
	if err != nil {
		return fmt.Errorf("getting last migration: %w", err)
	}

	// Find the migration to rollback
	var migration *Migration
	for i := range migrations {
		if migrations[i].Version == lastVersion {
			migration = &migrations[i]
			break
		}
	}

	if migration == nil {
		return fmt.Errorf("migration %d not found in filesystem", lastVersion)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction for rollback: %w", err)
	}

	if migration.DownSQL != "" {
		if _, err := tx.ExecContext(ctx, migration.DownSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("rolling back migration %d (%s): %w", migration.Version, migration.Name, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM _migrations WHERE version = ?`, migration.Version); err != nil {
		tx.Rollback()
		return fmt.Errorf("removing migration record %d: %w", migration.Version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing rollback: %w", err)
	}

	return nil
}

// GetMigrationStatus returns the current migration status.
func GetMigrationStatus(ctx context.Context, db *sql.DB) ([]MigrationRecord, error) {
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return nil, fmt.Errorf("ensuring migrations table: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version, name, applied_at FROM _migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MigrationRecord
	for rows.Next() {
		var rec MigrationRecord
		if err := rows.Scan(&rec.Version, &rec.Name, &rec.AppliedAt); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}
