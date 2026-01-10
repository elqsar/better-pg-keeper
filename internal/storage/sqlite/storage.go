package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/pganalyzer/internal/models"

	_ "modernc.org/sqlite"
)

// Storage defines the interface for pganalyzer storage operations.
type Storage interface {
	// Close closes the database connection.
	Close() error

	// Instance operations
	GetInstance(ctx context.Context, id int64) (*models.Instance, error)
	GetInstanceByName(ctx context.Context, name string) (*models.Instance, error)
	CreateInstance(ctx context.Context, inst *models.Instance) (int64, error)
	GetOrCreateInstance(ctx context.Context, inst *models.Instance) (int64, error)
	ListInstances(ctx context.Context) ([]models.Instance, error)

	// Snapshot operations
	CreateSnapshot(ctx context.Context, snap *models.Snapshot) (int64, error)
	GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error)
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error)

	// Query stats operations
	SaveQueryStats(ctx context.Context, snapshotID int64, stats []models.QueryStat) error
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error)

	// Table stats operations
	SaveTableStats(ctx context.Context, snapshotID int64, stats []models.TableStat) error
	GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error)

	// Index stats operations
	SaveIndexStats(ctx context.Context, snapshotID int64, stats []models.IndexStat) error
	GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error)

	// Suggestion operations
	UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error
	GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error)
	GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error)
	DismissSuggestion(ctx context.Context, id int64) error
	ResolveSuggestion(ctx context.Context, id int64) error

	// Explain plan operations
	SaveExplainPlan(ctx context.Context, plan *models.ExplainPlan) (int64, error)
	GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error)

	// Maintenance operations
	PurgeOldSnapshots(ctx context.Context, retention time.Duration) (int64, error)
}

// SQLiteStorage implements Storage using SQLite.
type SQLiteStorage struct {
	db *sql.DB
}

// NewStorage creates a new SQLite storage instance.
func NewStorage(dbPath string) (*SQLiteStorage, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating database directory: %w", err)
		}
	}

	// Open database with connection pooling settings
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1) // SQLite performs best with a single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Connections don't expire

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Run migrations
	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &SQLiteStorage{db: db}, nil
}

// Close closes the database connection.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for advanced use cases.
func (s *SQLiteStorage) DB() *sql.DB {
	return s.db
}

// =============================================================================
// Instance Operations
// =============================================================================

// GetInstance retrieves an instance by ID.
func (s *SQLiteStorage) GetInstance(ctx context.Context, id int64) (*models.Instance, error) {
	var inst models.Instance
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, host, port, database, created_at
		FROM instances WHERE id = ?
	`, id).Scan(&inst.ID, &inst.Name, &inst.Host, &inst.Port, &inst.Database, &inst.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting instance: %w", err)
	}

	return &inst, nil
}

// GetInstanceByName retrieves an instance by name.
func (s *SQLiteStorage) GetInstanceByName(ctx context.Context, name string) (*models.Instance, error) {
	var inst models.Instance
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, host, port, database, created_at
		FROM instances WHERE name = ?
	`, name).Scan(&inst.ID, &inst.Name, &inst.Host, &inst.Port, &inst.Database, &inst.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting instance by name: %w", err)
	}

	return &inst, nil
}

// CreateInstance creates a new instance and returns its ID.
func (s *SQLiteStorage) CreateInstance(ctx context.Context, inst *models.Instance) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO instances (name, host, port, database)
		VALUES (?, ?, ?, ?)
	`, inst.Name, inst.Host, inst.Port, inst.Database)

	if err != nil {
		return 0, fmt.Errorf("creating instance: %w", err)
	}

	return result.LastInsertId()
}

// GetOrCreateInstance gets an existing instance by host/port/database or creates a new one.
func (s *SQLiteStorage) GetOrCreateInstance(ctx context.Context, inst *models.Instance) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM instances
		WHERE host = ? AND port = ? AND database = ?
	`, inst.Host, inst.Port, inst.Database).Scan(&id)

	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("checking existing instance: %w", err)
	}

	return s.CreateInstance(ctx, inst)
}

// ListInstances returns all instances.
func (s *SQLiteStorage) ListInstances(ctx context.Context) ([]models.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, host, port, database, created_at
		FROM instances ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	defer rows.Close()

	var instances []models.Instance
	for rows.Next() {
		var inst models.Instance
		if err := rows.Scan(&inst.ID, &inst.Name, &inst.Host, &inst.Port, &inst.Database, &inst.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning instance: %w", err)
		}
		instances = append(instances, inst)
	}

	return instances, rows.Err()
}

// =============================================================================
// Snapshot Operations
// =============================================================================

// CreateSnapshot creates a new snapshot and returns its ID.
func (s *SQLiteStorage) CreateSnapshot(ctx context.Context, snap *models.Snapshot) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO snapshots (instance_id, captured_at, pg_version, stats_reset)
		VALUES (?, ?, ?, ?)
	`, snap.InstanceID, snap.CapturedAt, snap.PGVersion, snap.StatsReset)

	if err != nil {
		return 0, fmt.Errorf("creating snapshot: %w", err)
	}

	return result.LastInsertId()
}

// GetSnapshotByID retrieves a snapshot by ID.
func (s *SQLiteStorage) GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error) {
	var snap models.Snapshot
	err := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, captured_at, pg_version, stats_reset, created_at
		FROM snapshots WHERE id = ?
	`, id).Scan(&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion, &snap.StatsReset, &snap.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting snapshot: %w", err)
	}

	return &snap, nil
}

// GetLatestSnapshot retrieves the most recent snapshot for an instance.
func (s *SQLiteStorage) GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error) {
	var snap models.Snapshot
	err := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, captured_at, pg_version, stats_reset, created_at
		FROM snapshots
		WHERE instance_id = ?
		ORDER BY captured_at DESC
		LIMIT 1
	`, instanceID).Scan(&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion, &snap.StatsReset, &snap.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting latest snapshot: %w", err)
	}

	return &snap, nil
}

// ListSnapshots returns snapshots for an instance, ordered by capture time descending.
func (s *SQLiteStorage) ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, captured_at, pg_version, stats_reset, created_at
		FROM snapshots
		WHERE instance_id = ?
		ORDER BY captured_at DESC
		LIMIT ?
	`, instanceID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []models.Snapshot
	for rows.Next() {
		var snap models.Snapshot
		if err := rows.Scan(&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion, &snap.StatsReset, &snap.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning snapshot: %w", err)
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, rows.Err()
}

// =============================================================================
// Query Stats Operations
// =============================================================================

// SaveQueryStats saves query statistics for a snapshot.
func (s *SQLiteStorage) SaveQueryStats(ctx context.Context, snapshotID int64, stats []models.QueryStat) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO query_stats (
			snapshot_id, queryid, query, calls, total_exec_time, mean_exec_time,
			min_exec_time, max_exec_time, rows, shared_blks_hit, shared_blks_read,
			plans, total_plan_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			snapshotID, stat.QueryID, stat.Query, stat.Calls, stat.TotalExecTime, stat.MeanExecTime,
			stat.MinExecTime, stat.MaxExecTime, stat.Rows, stat.SharedBlksHit, stat.SharedBlksRead,
			stat.Plans, stat.TotalPlanTime,
		)
		if err != nil {
			return fmt.Errorf("inserting query stat: %w", err)
		}
	}

	return tx.Commit()
}

// GetQueryStats retrieves query statistics for a snapshot.
func (s *SQLiteStorage) GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, snapshot_id, queryid, query, calls, total_exec_time, mean_exec_time,
			min_exec_time, max_exec_time, rows, shared_blks_hit, shared_blks_read,
			plans, total_plan_time
		FROM query_stats
		WHERE snapshot_id = ?
		ORDER BY total_exec_time DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying stats: %w", err)
	}
	defer rows.Close()

	var stats []models.QueryStat
	for rows.Next() {
		var stat models.QueryStat
		err := rows.Scan(
			&stat.ID, &stat.SnapshotID, &stat.QueryID, &stat.Query, &stat.Calls,
			&stat.TotalExecTime, &stat.MeanExecTime, &stat.MinExecTime, &stat.MaxExecTime,
			&stat.Rows, &stat.SharedBlksHit, &stat.SharedBlksRead, &stat.Plans, &stat.TotalPlanTime,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning query stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// GetQueryStatsDelta calculates the difference in query statistics between two snapshots.
// If stats were reset (indicated by negative deltas), uses current values as-is.
func (s *SQLiteStorage) GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.queryid,
			t.query,
			CASE WHEN t.calls - COALESCE(f.calls, 0) < 0 THEN t.calls ELSE t.calls - COALESCE(f.calls, 0) END as delta_calls,
			CASE WHEN t.total_exec_time - COALESCE(f.total_exec_time, 0) < 0 THEN t.total_exec_time ELSE t.total_exec_time - COALESCE(f.total_exec_time, 0) END as delta_total_time,
			CASE WHEN t.rows - COALESCE(f.rows, 0) < 0 THEN t.rows ELSE t.rows - COALESCE(f.rows, 0) END as delta_rows,
			CASE WHEN t.shared_blks_hit - COALESCE(f.shared_blks_hit, 0) < 0 THEN t.shared_blks_hit ELSE t.shared_blks_hit - COALESCE(f.shared_blks_hit, 0) END as delta_blks_hit,
			CASE WHEN t.shared_blks_read - COALESCE(f.shared_blks_read, 0) < 0 THEN t.shared_blks_read ELSE t.shared_blks_read - COALESCE(f.shared_blks_read, 0) END as delta_blks_read
		FROM query_stats t
		LEFT JOIN query_stats f ON t.queryid = f.queryid AND f.snapshot_id = ?
		WHERE t.snapshot_id = ?
		ORDER BY delta_total_time DESC
	`, fromSnapshotID, toSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying delta stats: %w", err)
	}
	defer rows.Close()

	var deltas []models.QueryStatDelta
	for rows.Next() {
		var d models.QueryStatDelta
		var deltaBlksHit, deltaBlksRead int64
		err := rows.Scan(
			&d.QueryID, &d.Query, &d.DeltaCalls, &d.DeltaTotalTime,
			&d.DeltaRows, &deltaBlksHit, &deltaBlksRead,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning delta stat: %w", err)
		}

		d.DeltaBlksHit = deltaBlksHit
		d.DeltaBlksRead = deltaBlksRead
		d.FromSnapshotID = fromSnapshotID
		d.ToSnapshotID = toSnapshotID

		// Calculate derived metrics
		if d.DeltaCalls > 0 {
			d.MeanExecTime = d.DeltaTotalTime / float64(d.DeltaCalls)
			d.AvgRowsPerCall = float64(d.DeltaRows) / float64(d.DeltaCalls)
		}

		totalBlks := deltaBlksHit + deltaBlksRead
		if totalBlks > 0 {
			d.CacheHitRatio = float64(deltaBlksHit) / float64(totalBlks)
		}

		deltas = append(deltas, d)
	}

	return deltas, rows.Err()
}

// =============================================================================
// Table Stats Operations
// =============================================================================

// SaveTableStats saves table statistics for a snapshot.
func (s *SQLiteStorage) SaveTableStats(ctx context.Context, snapshotID int64, stats []models.TableStat) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO table_stats (
			snapshot_id, schemaname, relname, seq_scan, seq_tup_read, idx_scan,
			idx_tup_fetch, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum,
			last_analyze, table_size, index_size
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			snapshotID, stat.SchemaName, stat.RelName, stat.SeqScan, stat.SeqTupRead,
			stat.IdxScan, stat.IdxTupFetch, stat.NLiveTup, stat.NDeadTup,
			stat.LastVacuum, stat.LastAutovacuum, stat.LastAnalyze,
			stat.TableSize, stat.IndexSize,
		)
		if err != nil {
			return fmt.Errorf("inserting table stat: %w", err)
		}
	}

	return tx.Commit()
}

// GetTableStats retrieves table statistics for a snapshot.
func (s *SQLiteStorage) GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, snapshot_id, schemaname, relname, seq_scan, seq_tup_read, idx_scan,
			idx_tup_fetch, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum,
			last_analyze, table_size, index_size
		FROM table_stats
		WHERE snapshot_id = ?
		ORDER BY table_size DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying table stats: %w", err)
	}
	defer rows.Close()

	var stats []models.TableStat
	for rows.Next() {
		var stat models.TableStat
		err := rows.Scan(
			&stat.ID, &stat.SnapshotID, &stat.SchemaName, &stat.RelName,
			&stat.SeqScan, &stat.SeqTupRead, &stat.IdxScan, &stat.IdxTupFetch,
			&stat.NLiveTup, &stat.NDeadTup, &stat.LastVacuum, &stat.LastAutovacuum,
			&stat.LastAnalyze, &stat.TableSize, &stat.IndexSize,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning table stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// =============================================================================
// Index Stats Operations
// =============================================================================

// SaveIndexStats saves index statistics for a snapshot.
func (s *SQLiteStorage) SaveIndexStats(ctx context.Context, snapshotID int64, stats []models.IndexStat) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO index_stats (
			snapshot_id, schemaname, relname, indexrelname, idx_scan,
			idx_tup_read, idx_tup_fetch, index_size, is_unique, is_primary
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			snapshotID, stat.SchemaName, stat.RelName, stat.IndexRelName,
			stat.IdxScan, stat.IdxTupRead, stat.IdxTupFetch, stat.IndexSize,
			stat.IsUnique, stat.IsPrimary,
		)
		if err != nil {
			return fmt.Errorf("inserting index stat: %w", err)
		}
	}

	return tx.Commit()
}

// GetIndexStats retrieves index statistics for a snapshot.
func (s *SQLiteStorage) GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, snapshot_id, schemaname, relname, indexrelname, idx_scan,
			idx_tup_read, idx_tup_fetch, index_size, is_unique, is_primary
		FROM index_stats
		WHERE snapshot_id = ?
		ORDER BY index_size DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying index stats: %w", err)
	}
	defer rows.Close()

	var stats []models.IndexStat
	for rows.Next() {
		var stat models.IndexStat
		err := rows.Scan(
			&stat.ID, &stat.SnapshotID, &stat.SchemaName, &stat.RelName,
			&stat.IndexRelName, &stat.IdxScan, &stat.IdxTupRead, &stat.IdxTupFetch,
			&stat.IndexSize, &stat.IsUnique, &stat.IsPrimary,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning index stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// =============================================================================
// Suggestion Operations
// =============================================================================

// UpsertSuggestion creates or updates a suggestion.
// Suggestions are deduplicated by (instance_id, rule_id, target_object).
func (s *SQLiteStorage) UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error {
	now := time.Now()

	// Try to update existing suggestion
	result, err := s.db.ExecContext(ctx, `
		UPDATE suggestions
		SET severity = ?, title = ?, description = ?, metadata = ?,
			status = CASE WHEN status = 'resolved' THEN 'active' ELSE status END,
			last_seen_at = ?
		WHERE instance_id = ? AND rule_id = ? AND target_object = ?
	`, sug.Severity, sug.Title, sug.Description, sug.Metadata, now,
		sug.InstanceID, sug.RuleID, sug.TargetObject)

	if err != nil {
		return fmt.Errorf("updating suggestion: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("getting rows affected: %w", err)
	}

	if rowsAffected > 0 {
		return nil // Updated existing
	}

	// Insert new suggestion
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO suggestions (
			instance_id, rule_id, severity, title, description, target_object,
			metadata, status, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?)
	`, sug.InstanceID, sug.RuleID, sug.Severity, sug.Title, sug.Description,
		sug.TargetObject, sug.Metadata, now, now)

	if err != nil {
		return fmt.Errorf("inserting suggestion: %w", err)
	}

	return nil
}

// GetActiveSuggestions retrieves active suggestions for an instance.
func (s *SQLiteStorage) GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, rule_id, severity, title, description,
			target_object, metadata, status, first_seen_at, last_seen_at, dismissed_at
		FROM suggestions
		WHERE instance_id = ? AND status = 'active'
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'warning' THEN 2
				WHEN 'info' THEN 3
				ELSE 4
			END,
			last_seen_at DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying suggestions: %w", err)
	}
	defer rows.Close()

	var suggestions []models.Suggestion
	for rows.Next() {
		var sug models.Suggestion
		err := rows.Scan(
			&sug.ID, &sug.InstanceID, &sug.RuleID, &sug.Severity, &sug.Title,
			&sug.Description, &sug.TargetObject, &sug.Metadata, &sug.Status,
			&sug.FirstSeenAt, &sug.LastSeenAt, &sug.DismissedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning suggestion: %w", err)
		}
		suggestions = append(suggestions, sug)
	}

	return suggestions, rows.Err()
}

// GetSuggestionByID retrieves a suggestion by ID.
func (s *SQLiteStorage) GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error) {
	var sug models.Suggestion
	err := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, rule_id, severity, title, description,
			target_object, metadata, status, first_seen_at, last_seen_at, dismissed_at
		FROM suggestions
		WHERE id = ?
	`, id).Scan(
		&sug.ID, &sug.InstanceID, &sug.RuleID, &sug.Severity, &sug.Title,
		&sug.Description, &sug.TargetObject, &sug.Metadata, &sug.Status,
		&sug.FirstSeenAt, &sug.LastSeenAt, &sug.DismissedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting suggestion: %w", err)
	}

	return &sug, nil
}

// DismissSuggestion marks a suggestion as dismissed.
func (s *SQLiteStorage) DismissSuggestion(ctx context.Context, id int64) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE suggestions
		SET status = 'dismissed', dismissed_at = ?
		WHERE id = ?
	`, now, id)

	if err != nil {
		return fmt.Errorf("dismissing suggestion: %w", err)
	}

	return nil
}

// ResolveSuggestion marks a suggestion as resolved.
func (s *SQLiteStorage) ResolveSuggestion(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE suggestions
		SET status = 'resolved'
		WHERE id = ?
	`, id)

	if err != nil {
		return fmt.Errorf("resolving suggestion: %w", err)
	}

	return nil
}

// =============================================================================
// Explain Plan Operations
// =============================================================================

// SaveExplainPlan saves an explain plan.
func (s *SQLiteStorage) SaveExplainPlan(ctx context.Context, plan *models.ExplainPlan) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO explain_plans (queryid, plan_text, plan_json, captured_at, execution_time)
		VALUES (?, ?, ?, ?, ?)
	`, plan.QueryID, plan.PlanText, plan.PlanJSON, plan.CapturedAt, plan.ExecutionTime)

	if err != nil {
		return 0, fmt.Errorf("saving explain plan: %w", err)
	}

	return result.LastInsertId()
}

// GetExplainPlan retrieves the most recent explain plan for a query.
func (s *SQLiteStorage) GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error) {
	var plan models.ExplainPlan
	err := s.db.QueryRowContext(ctx, `
		SELECT id, queryid, plan_text, plan_json, captured_at, execution_time
		FROM explain_plans
		WHERE queryid = ?
		ORDER BY captured_at DESC
		LIMIT 1
	`, queryID).Scan(
		&plan.ID, &plan.QueryID, &plan.PlanText, &plan.PlanJSON,
		&plan.CapturedAt, &plan.ExecutionTime,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting explain plan: %w", err)
	}

	return &plan, nil
}

// =============================================================================
// Maintenance Operations
// =============================================================================

// PurgeOldSnapshots deletes snapshots older than the retention period.
// Related stats are cascade-deleted due to foreign key constraints.
func (s *SQLiteStorage) PurgeOldSnapshots(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM snapshots WHERE captured_at < ?
	`, cutoff)

	if err != nil {
		return 0, fmt.Errorf("purging old snapshots: %w", err)
	}

	return result.RowsAffected()
}

// Ensure SQLiteStorage implements Storage interface.
var _ Storage = (*SQLiteStorage)(nil)
