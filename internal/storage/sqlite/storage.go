package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"

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
	UpdateSnapshotCacheHitRatio(ctx context.Context, snapshotID int64, ratio float64) error

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

	// Bloat stats operations
	SaveBloatStats(ctx context.Context, snapshotID int64, stats []models.BloatInfo) error
	GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error)

	// Connection activity operations
	SaveConnectionActivity(ctx context.Context, snapshotID int64, activity *models.ConnectionActivity) error
	GetConnectionActivity(ctx context.Context, snapshotID int64) (*models.ConnectionActivity, error)

	// Long running queries operations
	SaveLongRunningQueries(ctx context.Context, snapshotID int64, queries []models.LongRunningQuery) error
	GetLongRunningQueries(ctx context.Context, snapshotID int64) ([]models.LongRunningQuery, error)

	// Idle in transaction operations
	SaveIdleInTransaction(ctx context.Context, snapshotID int64, idle []models.IdleInTransaction) error
	GetIdleInTransaction(ctx context.Context, snapshotID int64) ([]models.IdleInTransaction, error)

	// Lock stats operations
	SaveLockStats(ctx context.Context, snapshotID int64, stats *models.LockStats) error
	GetLockStats(ctx context.Context, snapshotID int64) (*models.LockStats, error)

	// Blocked queries operations
	SaveBlockedQueries(ctx context.Context, snapshotID int64, queries []models.BlockedQuery) error
	GetBlockedQueries(ctx context.Context, snapshotID int64) ([]models.BlockedQuery, error)

	// Extended database stats operations
	SaveExtendedDatabaseStats(ctx context.Context, snapshotID int64, stats *models.ExtendedDatabaseStats) error
	GetExtendedDatabaseStats(ctx context.Context, snapshotID int64) (*models.ExtendedDatabaseStats, error)

	// Suggestion operations
	UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
	GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error)
	DismissSuggestion(ctx context.Context, id int64) error
	ResolveSuggestion(ctx context.Context, id int64) error

	// Explain plan operations
	SaveExplainPlan(ctx context.Context, plan *models.ExplainPlan) (int64, error)
	GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error)

	// Maintenance operations
	PurgeOldSnapshots(ctx context.Context, retention time.Duration) (int64, error)

	// Current state operations (for dashboard - always up-to-date)
	SaveCurrentConnectionActivity(ctx context.Context, instanceID int64, activity *models.ConnectionActivity) error
	GetCurrentConnectionActivity(ctx context.Context, instanceID int64) (*models.ConnectionActivity, error)

	SaveCurrentLockStats(ctx context.Context, instanceID int64, stats *models.LockStats) error
	GetCurrentLockStats(ctx context.Context, instanceID int64) (*models.LockStats, error)

	SaveCurrentDatabaseStats(ctx context.Context, instanceID int64, stats *models.ExtendedDatabaseStats, cacheHitRatio *float64) error
	GetCurrentDatabaseStats(ctx context.Context, instanceID int64) (*models.ExtendedDatabaseStats, *float64, error)

	SaveCurrentLongRunningQueries(ctx context.Context, instanceID int64, queries []models.LongRunningQuery) error
	GetCurrentLongRunningQueries(ctx context.Context, instanceID int64) ([]models.LongRunningQuery, error)

	SaveCurrentIdleInTransaction(ctx context.Context, instanceID int64, idle []models.IdleInTransaction) error
	GetCurrentIdleInTransaction(ctx context.Context, instanceID int64) ([]models.IdleInTransaction, error)

	SaveCurrentBlockedQueries(ctx context.Context, instanceID int64, queries []models.BlockedQuery) error
	GetCurrentBlockedQueries(ctx context.Context, instanceID int64) ([]models.BlockedQuery, error)

	SaveCurrentQueryStats(ctx context.Context, instanceID int64, stats []models.QueryStat) error
	GetCurrentQueryStats(ctx context.Context, instanceID int64) ([]models.QueryStat, error)

	SaveCurrentTableStats(ctx context.Context, instanceID int64, stats []models.TableStat) error
	GetCurrentTableStats(ctx context.Context, instanceID int64) ([]models.TableStat, error)

	SaveCurrentIndexStats(ctx context.Context, instanceID int64, stats []models.IndexStat) error
	GetCurrentIndexStats(ctx context.Context, instanceID int64) ([]models.IndexStat, error)

	SaveCurrentBloatStats(ctx context.Context, instanceID int64, stats []models.BloatInfo) error
	GetCurrentBloatStats(ctx context.Context, instanceID int64) ([]models.BloatInfo, error)
}

// SQLiteStorage implements Storage using SQLite.
// Uses separate connections for reads and writes to allow concurrent access in WAL mode.
type SQLiteStorage struct {
	writeDB *sql.DB // Single connection for writes
	readDB  *sql.DB // Multiple connections for concurrent reads
}

// NewStorage creates a new SQLite storage instance.
func NewStorage(dbPath string) (*SQLiteStorage, error) {
	// Handle in-memory database specially
	// Each :memory: connection creates a separate database, so we need shared cache
	// or use the same connection for both read and write
	isMemory := dbPath == ":memory:" || dbPath == ""

	if isMemory {
		return newMemoryStorage()
	}

	return newFileStorage(dbPath)
}

// newMemoryStorage creates an in-memory storage instance.
// Uses a single connection since :memory: databases are not shared between connections.
func newMemoryStorage() (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("opening memory database: %w", err)
	}

	// Single connection for in-memory database
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging memory database: %w", err)
	}

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	// Use same connection for both read and write in memory mode
	return &SQLiteStorage{writeDB: db, readDB: db}, nil
}

// newFileStorage creates a file-based storage instance with separate read/write connections.
func newFileStorage(dbPath string) (*SQLiteStorage, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating database directory: %w", err)
		}
	}

	// Open write connection (single connection for serialized writes)
	writeDB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("opening write database: %w", err)
	}

	// Configure write connection pool - single writer
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)

	// Test write connection
	if err := writeDB.Ping(); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("pinging write database: %w", err)
	}

	// Run migrations on write connection
	ctx := context.Background()
	if err := Migrate(ctx, writeDB); err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	// Open read connection pool (multiple connections for concurrent reads)
	// WAL mode allows concurrent readers with a single writer
	readDB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&mode=ro")
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("opening read database: %w", err)
	}

	// Configure read connection pool - allow concurrent readers
	readDB.SetMaxOpenConns(10)
	readDB.SetMaxIdleConns(5)
	readDB.SetConnMaxLifetime(0)

	// Test read connection
	if err := readDB.Ping(); err != nil {
		writeDB.Close()
		readDB.Close()
		return nil, fmt.Errorf("pinging read database: %w", err)
	}

	return &SQLiteStorage{writeDB: writeDB, readDB: readDB}, nil
}

// Close closes the database connections.
func (s *SQLiteStorage) Close() error {
	var errs []error

	// If readDB and writeDB are the same (in-memory mode), only close once
	if s.readDB == s.writeDB {
		if err := s.writeDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing db: %w", err))
		}
	} else {
		if err := s.readDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing read db: %w", err))
		}
		if err := s.writeDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing write db: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// DB returns the write database connection for advanced use cases.
func (s *SQLiteStorage) DB() *sql.DB {
	return s.writeDB
}

// ReadDB returns the read database connection for advanced use cases.
func (s *SQLiteStorage) ReadDB() *sql.DB {
	return s.readDB
}

// =============================================================================
// Instance Operations
// =============================================================================

// GetInstance retrieves an instance by ID.
func (s *SQLiteStorage) GetInstance(ctx context.Context, id int64) (*models.Instance, error) {
	var inst models.Instance
	err := s.readDB.QueryRowContext(ctx, `
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
	err := s.readDB.QueryRowContext(ctx, `
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
	result, err := s.writeDB.ExecContext(ctx, `
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
	err := s.readDB.QueryRowContext(ctx, `
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
	rows, err := s.readDB.QueryContext(ctx, `
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
	result, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO snapshots (instance_id, captured_at, pg_version, stats_reset, cache_hit_ratio)
		VALUES (?, ?, ?, ?, ?)
	`, snap.InstanceID, snap.CapturedAt, snap.PGVersion, snap.StatsReset, snap.CacheHitRatio)

	if err != nil {
		return 0, fmt.Errorf("creating snapshot: %w", err)
	}

	return result.LastInsertId()
}

// GetSnapshotByID retrieves a snapshot by ID.
func (s *SQLiteStorage) GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error) {
	var snap models.Snapshot
	err := s.readDB.QueryRowContext(ctx, `
		SELECT id, instance_id, captured_at, pg_version, stats_reset, cache_hit_ratio, created_at
		FROM snapshots WHERE id = ?
	`, id).Scan(&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion, &snap.StatsReset, &snap.CacheHitRatio, &snap.CreatedAt)

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
	err := s.readDB.QueryRowContext(ctx, `
		SELECT id, instance_id, captured_at, pg_version, stats_reset, cache_hit_ratio, created_at
		FROM snapshots
		WHERE instance_id = ?
		ORDER BY captured_at DESC
		LIMIT 1
	`, instanceID).Scan(&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion, &snap.StatsReset, &snap.CacheHitRatio, &snap.CreatedAt)

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

	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, instance_id, captured_at, pg_version, stats_reset, cache_hit_ratio, created_at
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
		if err := rows.Scan(&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion, &snap.StatsReset, &snap.CacheHitRatio, &snap.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning snapshot: %w", err)
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, rows.Err()
}

// UpdateSnapshotCacheHitRatio updates the cache hit ratio for a snapshot.
func (s *SQLiteStorage) UpdateSnapshotCacheHitRatio(ctx context.Context, snapshotID int64, ratio float64) error {
	_, err := s.writeDB.ExecContext(ctx, `
		UPDATE snapshots SET cache_hit_ratio = ? WHERE id = ?
	`, ratio, snapshotID)

	if err != nil {
		return fmt.Errorf("updating snapshot cache hit ratio: %w", err)
	}

	return nil
}

// =============================================================================
// Query Stats Operations
// =============================================================================

// SaveQueryStats saves query statistics for a snapshot.
func (s *SQLiteStorage) SaveQueryStats(ctx context.Context, snapshotID int64, stats []models.QueryStat) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := s.writeDB.BeginTx(ctx, nil)
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
	rows, err := s.readDB.QueryContext(ctx, `
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
	rows, err := s.readDB.QueryContext(ctx, `
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

	tx, err := s.writeDB.BeginTx(ctx, nil)
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
	rows, err := s.readDB.QueryContext(ctx, `
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

	tx, err := s.writeDB.BeginTx(ctx, nil)
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
	rows, err := s.readDB.QueryContext(ctx, `
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
// Bloat Stats Operations
// =============================================================================

// SaveBloatStats saves bloat statistics for a snapshot.
func (s *SQLiteStorage) SaveBloatStats(ctx context.Context, snapshotID int64, stats []models.BloatInfo) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO bloat_stats (
			snapshot_id, schemaname, relname, n_dead_tup, n_live_tup, bloat_percent
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			snapshotID, stat.SchemaName, stat.RelName,
			stat.NDeadTup, stat.NLiveTup, stat.BloatPercent,
		)
		if err != nil {
			return fmt.Errorf("inserting bloat stat: %w", err)
		}
	}

	return tx.Commit()
}

// GetBloatStats retrieves bloat statistics for a snapshot.
func (s *SQLiteStorage) GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT schemaname, relname, n_dead_tup, n_live_tup, bloat_percent
		FROM bloat_stats
		WHERE snapshot_id = ?
		ORDER BY bloat_percent DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying bloat stats: %w", err)
	}
	defer rows.Close()

	var stats []models.BloatInfo
	for rows.Next() {
		var stat models.BloatInfo
		err := rows.Scan(
			&stat.SchemaName, &stat.RelName,
			&stat.NDeadTup, &stat.NLiveTup, &stat.BloatPercent,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning bloat stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// =============================================================================
// Connection Activity Operations
// =============================================================================

// SaveConnectionActivity saves connection activity for a snapshot.
func (s *SQLiteStorage) SaveConnectionActivity(ctx context.Context, snapshotID int64, activity *models.ConnectionActivity) error {
	if activity == nil {
		return nil
	}

	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO connection_activity (
			snapshot_id, active_count, idle_count, idle_in_tx_count, idle_in_tx_aborted,
			waiting_count, total_connections, max_connections
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, snapshotID, activity.ActiveCount, activity.IdleCount, activity.IdleInTxCount,
		activity.IdleInTxAborted, activity.WaitingCount, activity.TotalConnections, activity.MaxConnections)

	if err != nil {
		return fmt.Errorf("saving connection activity: %w", err)
	}

	return nil
}

// GetConnectionActivity retrieves connection activity for a snapshot.
func (s *SQLiteStorage) GetConnectionActivity(ctx context.Context, snapshotID int64) (*models.ConnectionActivity, error) {
	var activity models.ConnectionActivity
	err := s.readDB.QueryRowContext(ctx, `
		SELECT id, snapshot_id, active_count, idle_count, idle_in_tx_count, idle_in_tx_aborted,
			waiting_count, total_connections, max_connections
		FROM connection_activity
		WHERE snapshot_id = ?
	`, snapshotID).Scan(
		&activity.ID, &activity.SnapshotID, &activity.ActiveCount, &activity.IdleCount,
		&activity.IdleInTxCount, &activity.IdleInTxAborted, &activity.WaitingCount,
		&activity.TotalConnections, &activity.MaxConnections,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting connection activity: %w", err)
	}

	return &activity, nil
}

// =============================================================================
// Long Running Queries Operations
// =============================================================================

// SaveLongRunningQueries saves long running queries for a snapshot.
func (s *SQLiteStorage) SaveLongRunningQueries(ctx context.Context, snapshotID int64, queries []models.LongRunningQuery) error {
	if len(queries) == 0 {
		return nil
	}

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO long_running_queries (
			snapshot_id, pid, usename, datname, query, state,
			wait_event_type, wait_event, query_start, duration_seconds
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, q := range queries {
		_, err := stmt.ExecContext(ctx,
			snapshotID, q.PID, q.Username, q.DatabaseName, q.Query, q.State,
			q.WaitEventType, q.WaitEvent, q.QueryStart, q.DurationSeconds,
		)
		if err != nil {
			return fmt.Errorf("inserting long running query: %w", err)
		}
	}

	return tx.Commit()
}

// GetLongRunningQueries retrieves long running queries for a snapshot.
func (s *SQLiteStorage) GetLongRunningQueries(ctx context.Context, snapshotID int64) ([]models.LongRunningQuery, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, snapshot_id, pid, usename, datname, query, state,
			wait_event_type, wait_event, query_start, duration_seconds
		FROM long_running_queries
		WHERE snapshot_id = ?
		ORDER BY duration_seconds DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying long running queries: %w", err)
	}
	defer rows.Close()

	var queries []models.LongRunningQuery
	for rows.Next() {
		var q models.LongRunningQuery
		err := rows.Scan(
			&q.ID, &q.SnapshotID, &q.PID, &q.Username, &q.DatabaseName,
			&q.Query, &q.State, &q.WaitEventType, &q.WaitEvent,
			&q.QueryStart, &q.DurationSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning long running query: %w", err)
		}
		queries = append(queries, q)
	}

	return queries, rows.Err()
}

// =============================================================================
// Idle In Transaction Operations
// =============================================================================

// SaveIdleInTransaction saves idle in transaction connections for a snapshot.
func (s *SQLiteStorage) SaveIdleInTransaction(ctx context.Context, snapshotID int64, idle []models.IdleInTransaction) error {
	if len(idle) == 0 {
		return nil
	}

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO idle_in_transaction (
			snapshot_id, pid, usename, datname, state,
			xact_start, duration_seconds, query
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, i := range idle {
		_, err := stmt.ExecContext(ctx,
			snapshotID, i.PID, i.Username, i.DatabaseName, i.State,
			i.XactStart, i.DurationSeconds, i.Query,
		)
		if err != nil {
			return fmt.Errorf("inserting idle in transaction: %w", err)
		}
	}

	return tx.Commit()
}

// GetIdleInTransaction retrieves idle in transaction connections for a snapshot.
func (s *SQLiteStorage) GetIdleInTransaction(ctx context.Context, snapshotID int64) ([]models.IdleInTransaction, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, snapshot_id, pid, usename, datname, state,
			xact_start, duration_seconds, query
		FROM idle_in_transaction
		WHERE snapshot_id = ?
		ORDER BY duration_seconds DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying idle in transaction: %w", err)
	}
	defer rows.Close()

	var idle []models.IdleInTransaction
	for rows.Next() {
		var i models.IdleInTransaction
		err := rows.Scan(
			&i.ID, &i.SnapshotID, &i.PID, &i.Username, &i.DatabaseName,
			&i.State, &i.XactStart, &i.DurationSeconds, &i.Query,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning idle in transaction: %w", err)
		}
		idle = append(idle, i)
	}

	return idle, rows.Err()
}

// =============================================================================
// Lock Stats Operations
// =============================================================================

// SaveLockStats saves lock statistics for a snapshot.
func (s *SQLiteStorage) SaveLockStats(ctx context.Context, snapshotID int64, stats *models.LockStats) error {
	if stats == nil {
		return nil
	}

	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO lock_stats (
			snapshot_id, total_locks, granted_locks, waiting_locks,
			access_share_locks, row_exclusive_locks, exclusive_locks
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, snapshotID, stats.TotalLocks, stats.GrantedLocks, stats.WaitingLocks,
		stats.AccessShareLocks, stats.RowExclusiveLocks, stats.ExclusiveLocks)

	if err != nil {
		return fmt.Errorf("saving lock stats: %w", err)
	}

	return nil
}

// GetLockStats retrieves lock statistics for a snapshot.
func (s *SQLiteStorage) GetLockStats(ctx context.Context, snapshotID int64) (*models.LockStats, error) {
	var stats models.LockStats
	err := s.readDB.QueryRowContext(ctx, `
		SELECT id, snapshot_id, total_locks, granted_locks, waiting_locks,
			access_share_locks, row_exclusive_locks, exclusive_locks
		FROM lock_stats
		WHERE snapshot_id = ?
	`, snapshotID).Scan(
		&stats.ID, &stats.SnapshotID, &stats.TotalLocks, &stats.GrantedLocks,
		&stats.WaitingLocks, &stats.AccessShareLocks, &stats.RowExclusiveLocks,
		&stats.ExclusiveLocks,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting lock stats: %w", err)
	}

	return &stats, nil
}

// =============================================================================
// Blocked Queries Operations
// =============================================================================

// SaveBlockedQueries saves blocked queries for a snapshot.
func (s *SQLiteStorage) SaveBlockedQueries(ctx context.Context, snapshotID int64, queries []models.BlockedQuery) error {
	if len(queries) == 0 {
		return nil
	}

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO blocked_queries (
			snapshot_id, blocked_pid, blocked_user, blocked_query, blocked_start,
			wait_duration_seconds, blocking_pid, blocking_user, blocking_query,
			lock_type, lock_mode, relation
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, q := range queries {
		_, err := stmt.ExecContext(ctx,
			snapshotID, q.BlockedPID, q.BlockedUser, q.BlockedQuery, q.BlockedStart,
			q.WaitDuration, q.BlockingPID, q.BlockingUser, q.BlockingQuery,
			q.LockType, q.LockMode, q.Relation,
		)
		if err != nil {
			return fmt.Errorf("inserting blocked query: %w", err)
		}
	}

	return tx.Commit()
}

// GetBlockedQueries retrieves blocked queries for a snapshot.
func (s *SQLiteStorage) GetBlockedQueries(ctx context.Context, snapshotID int64) ([]models.BlockedQuery, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, snapshot_id, blocked_pid, blocked_user, blocked_query, blocked_start,
			wait_duration_seconds, blocking_pid, blocking_user, blocking_query,
			lock_type, lock_mode, relation
		FROM blocked_queries
		WHERE snapshot_id = ?
		ORDER BY wait_duration_seconds DESC
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("querying blocked queries: %w", err)
	}
	defer rows.Close()

	var queries []models.BlockedQuery
	for rows.Next() {
		var q models.BlockedQuery
		err := rows.Scan(
			&q.ID, &q.SnapshotID, &q.BlockedPID, &q.BlockedUser, &q.BlockedQuery,
			&q.BlockedStart, &q.WaitDuration, &q.BlockingPID, &q.BlockingUser,
			&q.BlockingQuery, &q.LockType, &q.LockMode, &q.Relation,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning blocked query: %w", err)
		}
		queries = append(queries, q)
	}

	return queries, rows.Err()
}

// =============================================================================
// Extended Database Stats Operations
// =============================================================================

// SaveExtendedDatabaseStats saves extended database statistics for a snapshot.
func (s *SQLiteStorage) SaveExtendedDatabaseStats(ctx context.Context, snapshotID int64, stats *models.ExtendedDatabaseStats) error {
	if stats == nil {
		return nil
	}

	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO extended_database_stats (
			snapshot_id, database_name, xact_commit, xact_rollback,
			temp_files, temp_bytes, deadlocks, confl_lock, confl_snapshot
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, snapshotID, stats.DatabaseName, stats.XactCommit, stats.XactRollback,
		stats.TempFiles, stats.TempBytes, stats.Deadlocks, stats.ConflLock, stats.ConflSnapshot)

	if err != nil {
		return fmt.Errorf("saving extended database stats: %w", err)
	}

	return nil
}

// GetExtendedDatabaseStats retrieves extended database statistics for a snapshot.
func (s *SQLiteStorage) GetExtendedDatabaseStats(ctx context.Context, snapshotID int64) (*models.ExtendedDatabaseStats, error) {
	var stats models.ExtendedDatabaseStats
	err := s.readDB.QueryRowContext(ctx, `
		SELECT id, snapshot_id, database_name, xact_commit, xact_rollback,
			temp_files, temp_bytes, deadlocks, confl_lock, confl_snapshot
		FROM extended_database_stats
		WHERE snapshot_id = ?
	`, snapshotID).Scan(
		&stats.ID, &stats.SnapshotID, &stats.DatabaseName, &stats.XactCommit,
		&stats.XactRollback, &stats.TempFiles, &stats.TempBytes, &stats.Deadlocks,
		&stats.ConflLock, &stats.ConflSnapshot,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting extended database stats: %w", err)
	}

	return &stats, nil
}

// =============================================================================
// Suggestion Operations
// =============================================================================

// UpsertSuggestion creates or updates a suggestion.
// Suggestions are deduplicated by (instance_id, rule_id, target_object).
func (s *SQLiteStorage) UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error {
	now := time.Now()

	// Try to update existing suggestion
	result, err := s.writeDB.ExecContext(ctx, `
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
	_, err = s.writeDB.ExecContext(ctx, `
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

// GetSuggestionsByStatus retrieves suggestions for an instance filtered by status.
func (s *SQLiteStorage) GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT id, instance_id, rule_id, severity, title, description,
			target_object, metadata, status, first_seen_at, last_seen_at, dismissed_at
		FROM suggestions
		WHERE instance_id = ? AND status = ?
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'warning' THEN 2
				WHEN 'info' THEN 3
				ELSE 4
			END,
			last_seen_at DESC
	`, instanceID, status)
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
	err := s.readDB.QueryRowContext(ctx, `
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
	_, err := s.writeDB.ExecContext(ctx, `
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
	_, err := s.writeDB.ExecContext(ctx, `
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
	result, err := s.writeDB.ExecContext(ctx, `
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
	err := s.readDB.QueryRowContext(ctx, `
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

	result, err := s.writeDB.ExecContext(ctx, `
		DELETE FROM snapshots WHERE captured_at < ?
	`, cutoff)

	if err != nil {
		return 0, fmt.Errorf("purging old snapshots: %w", err)
	}

	return result.RowsAffected()
}

// =============================================================================
// Current State Operations (for dashboard - always up-to-date)
// =============================================================================

// SaveCurrentConnectionActivity saves or updates current connection activity for an instance.
func (s *SQLiteStorage) SaveCurrentConnectionActivity(ctx context.Context, instanceID int64, activity *models.ConnectionActivity) error {
	if activity == nil {
		return nil
	}

	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO current_connection_activity (
			instance_id, active_count, idle_count, idle_in_tx_count, idle_in_tx_aborted,
			waiting_count, total_connections, max_connections, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(instance_id) DO UPDATE SET
			active_count = excluded.active_count,
			idle_count = excluded.idle_count,
			idle_in_tx_count = excluded.idle_in_tx_count,
			idle_in_tx_aborted = excluded.idle_in_tx_aborted,
			waiting_count = excluded.waiting_count,
			total_connections = excluded.total_connections,
			max_connections = excluded.max_connections,
			updated_at = CURRENT_TIMESTAMP
	`, instanceID, activity.ActiveCount, activity.IdleCount, activity.IdleInTxCount,
		activity.IdleInTxAborted, activity.WaitingCount, activity.TotalConnections, activity.MaxConnections)

	if err != nil {
		return fmt.Errorf("saving current connection activity: %w", err)
	}

	return nil
}

// GetCurrentConnectionActivity retrieves current connection activity for an instance.
func (s *SQLiteStorage) GetCurrentConnectionActivity(ctx context.Context, instanceID int64) (*models.ConnectionActivity, error) {
	var activity models.ConnectionActivity
	err := s.readDB.QueryRowContext(ctx, `
		SELECT instance_id, active_count, idle_count, idle_in_tx_count, idle_in_tx_aborted,
			waiting_count, total_connections, max_connections
		FROM current_connection_activity
		WHERE instance_id = ?
	`, instanceID).Scan(
		&activity.SnapshotID, &activity.ActiveCount, &activity.IdleCount,
		&activity.IdleInTxCount, &activity.IdleInTxAborted, &activity.WaitingCount,
		&activity.TotalConnections, &activity.MaxConnections,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting current connection activity: %w", err)
	}

	return &activity, nil
}

// SaveCurrentLockStats saves or updates current lock statistics for an instance.
func (s *SQLiteStorage) SaveCurrentLockStats(ctx context.Context, instanceID int64, stats *models.LockStats) error {
	if stats == nil {
		return nil
	}

	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO current_lock_stats (
			instance_id, total_locks, granted_locks, waiting_locks,
			access_share_locks, row_exclusive_locks, exclusive_locks, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(instance_id) DO UPDATE SET
			total_locks = excluded.total_locks,
			granted_locks = excluded.granted_locks,
			waiting_locks = excluded.waiting_locks,
			access_share_locks = excluded.access_share_locks,
			row_exclusive_locks = excluded.row_exclusive_locks,
			exclusive_locks = excluded.exclusive_locks,
			updated_at = CURRENT_TIMESTAMP
	`, instanceID, stats.TotalLocks, stats.GrantedLocks, stats.WaitingLocks,
		stats.AccessShareLocks, stats.RowExclusiveLocks, stats.ExclusiveLocks)

	if err != nil {
		return fmt.Errorf("saving current lock stats: %w", err)
	}

	return nil
}

// GetCurrentLockStats retrieves current lock statistics for an instance.
func (s *SQLiteStorage) GetCurrentLockStats(ctx context.Context, instanceID int64) (*models.LockStats, error) {
	var stats models.LockStats
	err := s.readDB.QueryRowContext(ctx, `
		SELECT instance_id, total_locks, granted_locks, waiting_locks,
			access_share_locks, row_exclusive_locks, exclusive_locks
		FROM current_lock_stats
		WHERE instance_id = ?
	`, instanceID).Scan(
		&stats.SnapshotID, &stats.TotalLocks, &stats.GrantedLocks,
		&stats.WaitingLocks, &stats.AccessShareLocks, &stats.RowExclusiveLocks,
		&stats.ExclusiveLocks,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting current lock stats: %w", err)
	}

	return &stats, nil
}

// SaveCurrentDatabaseStats saves or updates current database statistics for an instance.
func (s *SQLiteStorage) SaveCurrentDatabaseStats(ctx context.Context, instanceID int64, stats *models.ExtendedDatabaseStats, cacheHitRatio *float64) error {
	if stats == nil {
		return nil
	}

	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO current_database_stats (
			instance_id, database_name, xact_commit, xact_rollback, temp_files,
			temp_bytes, deadlocks, confl_lock, confl_snapshot, cache_hit_ratio, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(instance_id) DO UPDATE SET
			database_name = excluded.database_name,
			xact_commit = excluded.xact_commit,
			xact_rollback = excluded.xact_rollback,
			temp_files = excluded.temp_files,
			temp_bytes = excluded.temp_bytes,
			deadlocks = excluded.deadlocks,
			confl_lock = excluded.confl_lock,
			confl_snapshot = excluded.confl_snapshot,
			cache_hit_ratio = excluded.cache_hit_ratio,
			updated_at = CURRENT_TIMESTAMP
	`, instanceID, stats.DatabaseName, stats.XactCommit, stats.XactRollback,
		stats.TempFiles, stats.TempBytes, stats.Deadlocks, stats.ConflLock,
		stats.ConflSnapshot, cacheHitRatio)

	if err != nil {
		return fmt.Errorf("saving current database stats: %w", err)
	}

	return nil
}

// GetCurrentDatabaseStats retrieves current database statistics for an instance.
func (s *SQLiteStorage) GetCurrentDatabaseStats(ctx context.Context, instanceID int64) (*models.ExtendedDatabaseStats, *float64, error) {
	var stats models.ExtendedDatabaseStats
	var cacheHitRatio sql.NullFloat64
	err := s.readDB.QueryRowContext(ctx, `
		SELECT instance_id, database_name, xact_commit, xact_rollback, temp_files,
			temp_bytes, deadlocks, confl_lock, confl_snapshot, cache_hit_ratio
		FROM current_database_stats
		WHERE instance_id = ?
	`, instanceID).Scan(
		&stats.SnapshotID, &stats.DatabaseName, &stats.XactCommit, &stats.XactRollback,
		&stats.TempFiles, &stats.TempBytes, &stats.Deadlocks, &stats.ConflLock,
		&stats.ConflSnapshot, &cacheHitRatio,
	)

	if err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("getting current database stats: %w", err)
	}

	var ratio *float64
	if cacheHitRatio.Valid {
		ratio = &cacheHitRatio.Float64
	}

	return &stats, ratio, nil
}

// SaveCurrentLongRunningQueries saves or updates current long running queries for an instance.
func (s *SQLiteStorage) SaveCurrentLongRunningQueries(ctx context.Context, instanceID int64, queries []models.LongRunningQuery) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Upsert all current rows
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_long_running_queries (
			instance_id, pid, usename, datname, query, state,
			wait_event_type, wait_event, query_start, duration_seconds, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, pid) DO UPDATE SET
			usename = excluded.usename,
			datname = excluded.datname,
			query = excluded.query,
			state = excluded.state,
			wait_event_type = excluded.wait_event_type,
			wait_event = excluded.wait_event,
			query_start = excluded.query_start,
			duration_seconds = excluded.duration_seconds,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, q := range queries {
		_, err := stmt.ExecContext(ctx,
			instanceID, q.PID, q.Username, q.DatabaseName, q.Query, q.State,
			q.WaitEventType, q.WaitEvent, q.QueryStart, q.DurationSeconds, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting long running query: %w", err)
		}
	}

	// Delete stale rows not in current batch
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_long_running_queries WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale long running queries: %w", err)
	}

	return tx.Commit()
}

// GetCurrentLongRunningQueries retrieves current long running queries for an instance.
func (s *SQLiteStorage) GetCurrentLongRunningQueries(ctx context.Context, instanceID int64) ([]models.LongRunningQuery, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, pid, usename, datname, query, state,
			wait_event_type, wait_event, query_start, duration_seconds
		FROM current_long_running_queries
		WHERE instance_id = ?
		ORDER BY duration_seconds DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current long running queries: %w", err)
	}
	defer rows.Close()

	var queries []models.LongRunningQuery
	for rows.Next() {
		var q models.LongRunningQuery
		err := rows.Scan(
			&q.SnapshotID, &q.PID, &q.Username, &q.DatabaseName,
			&q.Query, &q.State, &q.WaitEventType, &q.WaitEvent,
			&q.QueryStart, &q.DurationSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning long running query: %w", err)
		}
		queries = append(queries, q)
	}

	return queries, rows.Err()
}

// SaveCurrentIdleInTransaction saves or updates current idle in transaction connections for an instance.
func (s *SQLiteStorage) SaveCurrentIdleInTransaction(ctx context.Context, instanceID int64, idle []models.IdleInTransaction) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_idle_in_transaction (
			instance_id, pid, usename, datname, state,
			xact_start, duration_seconds, query, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, pid) DO UPDATE SET
			usename = excluded.usename,
			datname = excluded.datname,
			state = excluded.state,
			xact_start = excluded.xact_start,
			duration_seconds = excluded.duration_seconds,
			query = excluded.query,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, i := range idle {
		_, err := stmt.ExecContext(ctx,
			instanceID, i.PID, i.Username, i.DatabaseName, i.State,
			i.XactStart, i.DurationSeconds, i.Query, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting idle in transaction: %w", err)
		}
	}

	// Delete stale rows
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_idle_in_transaction WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale idle in transaction: %w", err)
	}

	return tx.Commit()
}

// GetCurrentIdleInTransaction retrieves current idle in transaction connections for an instance.
func (s *SQLiteStorage) GetCurrentIdleInTransaction(ctx context.Context, instanceID int64) ([]models.IdleInTransaction, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, pid, usename, datname, state,
			xact_start, duration_seconds, query
		FROM current_idle_in_transaction
		WHERE instance_id = ?
		ORDER BY duration_seconds DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current idle in transaction: %w", err)
	}
	defer rows.Close()

	var idle []models.IdleInTransaction
	for rows.Next() {
		var i models.IdleInTransaction
		err := rows.Scan(
			&i.SnapshotID, &i.PID, &i.Username, &i.DatabaseName,
			&i.State, &i.XactStart, &i.DurationSeconds, &i.Query,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning idle in transaction: %w", err)
		}
		idle = append(idle, i)
	}

	return idle, rows.Err()
}

// SaveCurrentBlockedQueries saves or updates current blocked queries for an instance.
func (s *SQLiteStorage) SaveCurrentBlockedQueries(ctx context.Context, instanceID int64, queries []models.BlockedQuery) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_blocked_queries (
			instance_id, blocked_pid, blocked_user, blocked_query, blocked_start,
			wait_duration_seconds, blocking_pid, blocking_user, blocking_query,
			lock_type, lock_mode, relation, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, blocked_pid) DO UPDATE SET
			blocked_user = excluded.blocked_user,
			blocked_query = excluded.blocked_query,
			blocked_start = excluded.blocked_start,
			wait_duration_seconds = excluded.wait_duration_seconds,
			blocking_pid = excluded.blocking_pid,
			blocking_user = excluded.blocking_user,
			blocking_query = excluded.blocking_query,
			lock_type = excluded.lock_type,
			lock_mode = excluded.lock_mode,
			relation = excluded.relation,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, q := range queries {
		_, err := stmt.ExecContext(ctx,
			instanceID, q.BlockedPID, q.BlockedUser, q.BlockedQuery, q.BlockedStart,
			q.WaitDuration, q.BlockingPID, q.BlockingUser, q.BlockingQuery,
			q.LockType, q.LockMode, q.Relation, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting blocked query: %w", err)
		}
	}

	// Delete stale rows
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_blocked_queries WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale blocked queries: %w", err)
	}

	return tx.Commit()
}

// GetCurrentBlockedQueries retrieves current blocked queries for an instance.
func (s *SQLiteStorage) GetCurrentBlockedQueries(ctx context.Context, instanceID int64) ([]models.BlockedQuery, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, blocked_pid, blocked_user, blocked_query, blocked_start,
			wait_duration_seconds, blocking_pid, blocking_user, blocking_query,
			lock_type, lock_mode, relation
		FROM current_blocked_queries
		WHERE instance_id = ?
		ORDER BY wait_duration_seconds DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current blocked queries: %w", err)
	}
	defer rows.Close()

	var queries []models.BlockedQuery
	for rows.Next() {
		var q models.BlockedQuery
		err := rows.Scan(
			&q.SnapshotID, &q.BlockedPID, &q.BlockedUser, &q.BlockedQuery,
			&q.BlockedStart, &q.WaitDuration, &q.BlockingPID, &q.BlockingUser,
			&q.BlockingQuery, &q.LockType, &q.LockMode, &q.Relation,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning blocked query: %w", err)
		}
		queries = append(queries, q)
	}

	return queries, rows.Err()
}

// SaveCurrentQueryStats saves or updates current query statistics for an instance.
func (s *SQLiteStorage) SaveCurrentQueryStats(ctx context.Context, instanceID int64, stats []models.QueryStat) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_query_stats (
			instance_id, queryid, query, calls, total_exec_time, mean_exec_time,
			min_exec_time, max_exec_time, rows, shared_blks_hit, shared_blks_read,
			plans, total_plan_time, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, queryid) DO UPDATE SET
			query = excluded.query,
			calls = excluded.calls,
			total_exec_time = excluded.total_exec_time,
			mean_exec_time = excluded.mean_exec_time,
			min_exec_time = excluded.min_exec_time,
			max_exec_time = excluded.max_exec_time,
			rows = excluded.rows,
			shared_blks_hit = excluded.shared_blks_hit,
			shared_blks_read = excluded.shared_blks_read,
			plans = excluded.plans,
			total_plan_time = excluded.total_plan_time,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			instanceID, stat.QueryID, stat.Query, stat.Calls, stat.TotalExecTime, stat.MeanExecTime,
			stat.MinExecTime, stat.MaxExecTime, stat.Rows, stat.SharedBlksHit, stat.SharedBlksRead,
			stat.Plans, stat.TotalPlanTime, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting query stat: %w", err)
		}
	}

	// Delete stale rows
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_query_stats WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale query stats: %w", err)
	}

	return tx.Commit()
}

// GetCurrentQueryStats retrieves current query statistics for an instance.
func (s *SQLiteStorage) GetCurrentQueryStats(ctx context.Context, instanceID int64) ([]models.QueryStat, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, queryid, query, calls, total_exec_time, mean_exec_time,
			min_exec_time, max_exec_time, rows, shared_blks_hit, shared_blks_read,
			plans, total_plan_time
		FROM current_query_stats
		WHERE instance_id = ?
		ORDER BY total_exec_time DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current query stats: %w", err)
	}
	defer rows.Close()

	var stats []models.QueryStat
	for rows.Next() {
		var stat models.QueryStat
		err := rows.Scan(
			&stat.SnapshotID, &stat.QueryID, &stat.Query, &stat.Calls,
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

// SaveCurrentTableStats saves or updates current table statistics for an instance.
func (s *SQLiteStorage) SaveCurrentTableStats(ctx context.Context, instanceID int64, stats []models.TableStat) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_table_stats (
			instance_id, schemaname, relname, seq_scan, seq_tup_read, idx_scan,
			idx_tup_fetch, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum,
			last_analyze, table_size, index_size, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, schemaname, relname) DO UPDATE SET
			seq_scan = excluded.seq_scan,
			seq_tup_read = excluded.seq_tup_read,
			idx_scan = excluded.idx_scan,
			idx_tup_fetch = excluded.idx_tup_fetch,
			n_live_tup = excluded.n_live_tup,
			n_dead_tup = excluded.n_dead_tup,
			last_vacuum = excluded.last_vacuum,
			last_autovacuum = excluded.last_autovacuum,
			last_analyze = excluded.last_analyze,
			table_size = excluded.table_size,
			index_size = excluded.index_size,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			instanceID, stat.SchemaName, stat.RelName, stat.SeqScan, stat.SeqTupRead,
			stat.IdxScan, stat.IdxTupFetch, stat.NLiveTup, stat.NDeadTup,
			stat.LastVacuum, stat.LastAutovacuum, stat.LastAnalyze,
			stat.TableSize, stat.IndexSize, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting table stat: %w", err)
		}
	}

	// Delete stale rows
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_table_stats WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale table stats: %w", err)
	}

	return tx.Commit()
}

// GetCurrentTableStats retrieves current table statistics for an instance.
func (s *SQLiteStorage) GetCurrentTableStats(ctx context.Context, instanceID int64) ([]models.TableStat, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, schemaname, relname, seq_scan, seq_tup_read, idx_scan,
			idx_tup_fetch, n_live_tup, n_dead_tup, last_vacuum, last_autovacuum,
			last_analyze, table_size, index_size
		FROM current_table_stats
		WHERE instance_id = ?
		ORDER BY table_size DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current table stats: %w", err)
	}
	defer rows.Close()

	var stats []models.TableStat
	for rows.Next() {
		var stat models.TableStat
		err := rows.Scan(
			&stat.SnapshotID, &stat.SchemaName, &stat.RelName,
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

// SaveCurrentIndexStats saves or updates current index statistics for an instance.
func (s *SQLiteStorage) SaveCurrentIndexStats(ctx context.Context, instanceID int64, stats []models.IndexStat) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_index_stats (
			instance_id, schemaname, relname, indexrelname, idx_scan,
			idx_tup_read, idx_tup_fetch, index_size, is_unique, is_primary, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, schemaname, relname, indexrelname) DO UPDATE SET
			idx_scan = excluded.idx_scan,
			idx_tup_read = excluded.idx_tup_read,
			idx_tup_fetch = excluded.idx_tup_fetch,
			index_size = excluded.index_size,
			is_unique = excluded.is_unique,
			is_primary = excluded.is_primary,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			instanceID, stat.SchemaName, stat.RelName, stat.IndexRelName,
			stat.IdxScan, stat.IdxTupRead, stat.IdxTupFetch, stat.IndexSize,
			stat.IsUnique, stat.IsPrimary, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting index stat: %w", err)
		}
	}

	// Delete stale rows
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_index_stats WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale index stats: %w", err)
	}

	return tx.Commit()
}

// GetCurrentIndexStats retrieves current index statistics for an instance.
func (s *SQLiteStorage) GetCurrentIndexStats(ctx context.Context, instanceID int64) ([]models.IndexStat, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, schemaname, relname, indexrelname, idx_scan,
			idx_tup_read, idx_tup_fetch, index_size, is_unique, is_primary
		FROM current_index_stats
		WHERE instance_id = ?
		ORDER BY index_size DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current index stats: %w", err)
	}
	defer rows.Close()

	var stats []models.IndexStat
	for rows.Next() {
		var stat models.IndexStat
		err := rows.Scan(
			&stat.SnapshotID, &stat.SchemaName, &stat.RelName, &stat.IndexRelName,
			&stat.IdxScan, &stat.IdxTupRead, &stat.IdxTupFetch,
			&stat.IndexSize, &stat.IsUnique, &stat.IsPrimary,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning index stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// SaveCurrentBloatStats saves or updates current bloat statistics for an instance.
func (s *SQLiteStorage) SaveCurrentBloatStats(ctx context.Context, instanceID int64, stats []models.BloatInfo) error {
	batchTime := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO current_bloat_stats (
			instance_id, schemaname, relname, n_dead_tup, n_live_tup, bloat_percent, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, schemaname, relname) DO UPDATE SET
			n_dead_tup = excluded.n_dead_tup,
			n_live_tup = excluded.n_live_tup,
			bloat_percent = excluded.bloat_percent,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, stat := range stats {
		_, err := stmt.ExecContext(ctx,
			instanceID, stat.SchemaName, stat.RelName,
			stat.NDeadTup, stat.NLiveTup, stat.BloatPercent, batchTime,
		)
		if err != nil {
			return fmt.Errorf("upserting bloat stat: %w", err)
		}
	}

	// Delete stale rows
	_, err = tx.ExecContext(ctx, `
		DELETE FROM current_bloat_stats WHERE instance_id = ? AND updated_at != ?
	`, instanceID, batchTime)
	if err != nil {
		return fmt.Errorf("deleting stale bloat stats: %w", err)
	}

	return tx.Commit()
}

// GetCurrentBloatStats retrieves current bloat statistics for an instance.
func (s *SQLiteStorage) GetCurrentBloatStats(ctx context.Context, instanceID int64) ([]models.BloatInfo, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT schemaname, relname, n_dead_tup, n_live_tup, bloat_percent
		FROM current_bloat_stats
		WHERE instance_id = ?
		ORDER BY bloat_percent DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying current bloat stats: %w", err)
	}
	defer rows.Close()

	var stats []models.BloatInfo
	for rows.Next() {
		var stat models.BloatInfo
		err := rows.Scan(
			&stat.SchemaName, &stat.RelName,
			&stat.NDeadTup, &stat.NLiveTup, &stat.BloatPercent,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning bloat stat: %w", err)
		}
		stats = append(stats, stat)
	}

	return stats, rows.Err()
}

// Ensure SQLiteStorage implements Storage interface.
var _ Storage = (*SQLiteStorage)(nil)
