// Package models defines the data structures used throughout pganalyzer.
package models

import (
	"time"
)

// Instance represents a monitored PostgreSQL instance.
type Instance struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Database  string    `json:"database"`
	CreatedAt time.Time `json:"created_at"`
}

// Snapshot represents a point-in-time capture of PostgreSQL statistics.
type Snapshot struct {
	ID            int64      `json:"id"`
	InstanceID    int64      `json:"instance_id"`
	CapturedAt    time.Time  `json:"captured_at"`
	PGVersion     string     `json:"pg_version"`
	StatsReset    *time.Time `json:"stats_reset,omitempty"`
	CacheHitRatio *float64   `json:"cache_hit_ratio,omitempty"` // 0-100 percentage
	CreatedAt     time.Time  `json:"created_at"`
}

// QueryStat represents statistics for a single query from pg_stat_statements.
type QueryStat struct {
	ID             int64   `json:"id"`
	SnapshotID     int64   `json:"snapshot_id"`
	QueryID        int64   `json:"queryid"`
	Query          string  `json:"query"`
	Calls          int64   `json:"calls"`
	TotalExecTime  float64 `json:"total_exec_time"` // milliseconds
	MeanExecTime   float64 `json:"mean_exec_time"`  // milliseconds
	MinExecTime    float64 `json:"min_exec_time"`   // milliseconds
	MaxExecTime    float64 `json:"max_exec_time"`   // milliseconds
	Rows           int64   `json:"rows"`
	SharedBlksHit  int64   `json:"shared_blks_hit"`
	SharedBlksRead int64   `json:"shared_blks_read"`
	Plans          int64   `json:"plans"`           // PG14+
	TotalPlanTime  float64 `json:"total_plan_time"` // milliseconds
}

// QueryStatDelta represents the difference in query statistics between two snapshots.
type QueryStatDelta struct {
	QueryID        int64   `json:"queryid"`
	Query          string  `json:"query"`
	DeltaCalls     int64   `json:"delta_calls"`
	DeltaTotalTime float64 `json:"delta_total_time"` // milliseconds
	DeltaRows      int64   `json:"delta_rows"`
	DeltaBlksHit   int64   `json:"delta_blks_hit"`
	DeltaBlksRead  int64   `json:"delta_blks_read"`
	MeanExecTime   float64 `json:"mean_exec_time"`  // milliseconds
	CacheHitRatio  float64 `json:"cache_hit_ratio"` // 0-1
	AvgRowsPerCall float64 `json:"avg_rows_per_call"`
	FromSnapshotID int64   `json:"from_snapshot_id"`
	ToSnapshotID   int64   `json:"to_snapshot_id"`
}

// TableStat represents statistics for a single table.
type TableStat struct {
	ID             int64      `json:"id"`
	SnapshotID     int64      `json:"snapshot_id"`
	SchemaName     string     `json:"schemaname"`
	RelName        string     `json:"relname"`
	SeqScan        int64      `json:"seq_scan"`
	SeqTupRead     int64      `json:"seq_tup_read"`
	IdxScan        int64      `json:"idx_scan"`
	IdxTupFetch    int64      `json:"idx_tup_fetch"`
	NLiveTup       int64      `json:"n_live_tup"`
	NDeadTup       int64      `json:"n_dead_tup"`
	LastVacuum     *time.Time `json:"last_vacuum,omitempty"`
	LastAutovacuum *time.Time `json:"last_autovacuum,omitempty"`
	LastAnalyze    *time.Time `json:"last_analyze,omitempty"`
	TableSize      int64      `json:"table_size"` // bytes
	IndexSize      int64      `json:"index_size"` // bytes
}

// IndexStat represents statistics for a single index.
type IndexStat struct {
	ID           int64  `json:"id"`
	SnapshotID   int64  `json:"snapshot_id"`
	SchemaName   string `json:"schemaname"`
	RelName      string `json:"relname"`
	IndexRelName string `json:"indexrelname"`
	IdxScan      int64  `json:"idx_scan"`
	IdxTupRead   int64  `json:"idx_tup_read"`
	IdxTupFetch  int64  `json:"idx_tup_fetch"`
	IndexSize    int64  `json:"index_size"` // bytes
	IsUnique     bool   `json:"is_unique"`
	IsPrimary    bool   `json:"is_primary"`
}

// Suggestion represents a generated recommendation.
type Suggestion struct {
	ID           int64      `json:"id"`
	InstanceID   int64      `json:"instance_id"`
	RuleID       string     `json:"rule_id"`  // e.g., "unused_index", "slow_query"
	Severity     string     `json:"severity"` // "critical", "warning", "info"
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	TargetObject string     `json:"target_object"` // table/index/query identifier
	Metadata     string     `json:"metadata"`      // JSON with rule-specific data
	Status       string     `json:"status"`        // "active", "dismissed", "resolved"
	FirstSeenAt  time.Time  `json:"first_seen_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	DismissedAt  *time.Time `json:"dismissed_at,omitempty"`
}

// Severity constants for suggestions.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Status constants for suggestions.
const (
	StatusActive    = "active"
	StatusDismissed = "dismissed"
	StatusResolved  = "resolved"
)

// ExplainPlan represents a cached EXPLAIN plan for a query.
type ExplainPlan struct {
	ID            int64     `json:"id"`
	QueryID       int64     `json:"queryid"`
	PlanText      string    `json:"plan_text"`
	PlanJSON      string    `json:"plan_json,omitempty"`
	CapturedAt    time.Time `json:"captured_at"`
	ExecutionTime *float64  `json:"execution_time,omitempty"` // if ANALYZE was used
}

// DatabaseStats represents database-level statistics.
type DatabaseStats struct {
	DatabaseName  string  `json:"database_name"`
	BlksHit       int64   `json:"blks_hit"`
	BlksRead      int64   `json:"blks_read"`
	CacheHitRatio float64 `json:"cache_hit_ratio"` // 0-100 percentage
}

// BloatInfo represents table bloat information.
type BloatInfo struct {
	SchemaName   string  `json:"schemaname"`
	RelName      string  `json:"relname"`
	NDeadTup     int64   `json:"n_dead_tup"`
	NLiveTup     int64   `json:"n_live_tup"`
	BloatPercent float64 `json:"bloat_percent"` // dead tuples as percentage of live
}

// IndexDetail represents extended index information.
type IndexDetail struct {
	SchemaName  string `json:"schemaname"`
	TableName   string `json:"table_name"`
	IndexName   string `json:"index_name"`
	IndexDef    string `json:"index_def"`
	IndexSize   int64  `json:"index_size"`
	IdxScan     int64  `json:"idx_scan"`
	IdxTupRead  int64  `json:"idx_tup_read"`
	IdxTupFetch int64  `json:"idx_tup_fetch"`
	IsUnique    bool   `json:"is_unique"`
	IsPrimary   bool   `json:"is_primary"`
	IsValid     bool   `json:"is_valid"`
	TableSize   int64  `json:"table_size"`
}

// ConnectionActivity represents a snapshot of pg_stat_activity aggregates.
type ConnectionActivity struct {
	ID               int64 `json:"id"`
	SnapshotID       int64 `json:"snapshot_id"`
	ActiveCount      int   `json:"active_count"`
	IdleCount        int   `json:"idle_count"`
	IdleInTxCount    int   `json:"idle_in_tx_count"`
	IdleInTxAborted  int   `json:"idle_in_tx_aborted"`
	WaitingCount     int   `json:"waiting_count"`
	TotalConnections int   `json:"total_connections"`
	MaxConnections   int   `json:"max_connections"`
}

// LongRunningQuery represents a query running longer than threshold.
type LongRunningQuery struct {
	ID              int64     `json:"id"`
	SnapshotID      int64     `json:"snapshot_id"`
	PID             int       `json:"pid"`
	Username        string    `json:"usename"`
	DatabaseName    string    `json:"datname"`
	Query           string    `json:"query"`
	State           string    `json:"state"`
	WaitEventType   *string   `json:"wait_event_type,omitempty"`
	WaitEvent       *string   `json:"wait_event,omitempty"`
	QueryStart      time.Time `json:"query_start"`
	DurationSeconds float64   `json:"duration_seconds"`
}

// IdleInTransaction represents a connection idle in transaction.
type IdleInTransaction struct {
	ID              int64     `json:"id"`
	SnapshotID      int64     `json:"snapshot_id"`
	PID             int       `json:"pid"`
	Username        string    `json:"usename"`
	DatabaseName    string    `json:"datname"`
	State           string    `json:"state"`
	XactStart       time.Time `json:"xact_start"`
	DurationSeconds float64   `json:"duration_seconds"`
	Query           string    `json:"query"`
}

// LockStats represents aggregated lock statistics.
type LockStats struct {
	ID                int64 `json:"id"`
	SnapshotID        int64 `json:"snapshot_id"`
	TotalLocks        int   `json:"total_locks"`
	GrantedLocks      int   `json:"granted_locks"`
	WaitingLocks      int   `json:"waiting_locks"`
	AccessShareLocks  int   `json:"access_share_locks"`
	RowExclusiveLocks int   `json:"row_exclusive_locks"`
	ExclusiveLocks    int   `json:"exclusive_locks"`
}

// BlockedQuery represents a query blocked by another transaction.
type BlockedQuery struct {
	ID            int64     `json:"id"`
	SnapshotID    int64     `json:"snapshot_id"`
	BlockedPID    int       `json:"blocked_pid"`
	BlockedUser   string    `json:"blocked_user"`
	BlockedQuery  string    `json:"blocked_query"`
	BlockedStart  time.Time `json:"blocked_start"`
	WaitDuration  float64   `json:"wait_duration_seconds"`
	BlockingPID   int       `json:"blocking_pid"`
	BlockingUser  string    `json:"blocking_user"`
	BlockingQuery string    `json:"blocking_query"`
	LockType      string    `json:"lock_type"`
	LockMode      string    `json:"lock_mode"`
	Relation      *string   `json:"relation,omitempty"`
}

// ExtendedDatabaseStats represents operational database metrics.
type ExtendedDatabaseStats struct {
	ID            int64  `json:"id"`
	SnapshotID    int64  `json:"snapshot_id"`
	DatabaseName  string `json:"database_name"`
	XactCommit    int64  `json:"xact_commit"`
	XactRollback  int64  `json:"xact_rollback"`
	TempFiles     int64  `json:"temp_files"`
	TempBytes     int64  `json:"temp_bytes"`
	Deadlocks     int64  `json:"deadlocks"`
	ConflLock     int64  `json:"confl_lock"`
	ConflSnapshot int64  `json:"confl_snapshot"`
}
