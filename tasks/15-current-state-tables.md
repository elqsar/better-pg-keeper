# Task 15: Current State Tables for Dashboard

Implement "current state" tables to ensure dashboard always displays the latest data from each collector, independent of snapshot timing.

## Problem

Dashboard shows "N/A" for some data because collectors run at different intervals (30s-1hr). When the latest snapshot was created by a fast collector (locks: 30s), it may not have data from slower collectors (query_stats: 60s).

## Solution

Create separate "current state" tables that are always up-to-date, independent of snapshots:
- Dashboard reads from current tables (real-time display)
- Historical snapshots remain for trend analysis

## Summary

| Component | Files | Purpose |
|-----------|-------|---------|
| Migration | `internal/storage/sqlite/migrations/011_*.sql` | Create 10 current state tables |
| Storage | `internal/storage/sqlite/storage.go` | Add 20+ Save/Get methods |
| Activity Collector | `internal/collector/activity/activity.go` | Dual-write pattern |
| Locks Collector | `internal/collector/locks/locks.go` | Dual-write pattern |
| Query Collector | `internal/collector/query/stats.go` | Dual-write pattern |
| Database Collector | `internal/collector/resource/database.go` | Dual-write pattern |
| Tables Collector | `internal/collector/resource/tables.go` | Dual-write pattern |
| Indexes Collector | `internal/collector/resource/indexes.go` | Dual-write pattern |
| Bloat Collector | `internal/collector/schema/bloat.go` | Dual-write pattern |
| Dashboard | `internal/api/handlers/pages.go` | Read from current tables |

---

## Phase 1: Database Migration

### 1.1 Create Migration (`internal/storage/sqlite/migrations/011_create_current_state_tables.sql`)

```sql
-- +migrate Up

-- Single-row tables (one row per instance)
CREATE TABLE current_connection_activity (
    instance_id      INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    active_count     INTEGER NOT NULL DEFAULT 0,
    idle_count       INTEGER NOT NULL DEFAULT 0,
    idle_in_tx_count INTEGER NOT NULL DEFAULT 0,
    idle_in_tx_aborted INTEGER NOT NULL DEFAULT 0,
    waiting_count    INTEGER NOT NULL DEFAULT 0,
    total_connections INTEGER NOT NULL DEFAULT 0,
    max_connections  INTEGER NOT NULL DEFAULT 100,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE current_lock_stats (
    instance_id         INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    total_locks         INTEGER NOT NULL DEFAULT 0,
    granted_locks       INTEGER NOT NULL DEFAULT 0,
    waiting_locks       INTEGER NOT NULL DEFAULT 0,
    access_share_locks  INTEGER NOT NULL DEFAULT 0,
    row_exclusive_locks INTEGER NOT NULL DEFAULT 0,
    exclusive_locks     INTEGER NOT NULL DEFAULT 0,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE current_database_stats (
    instance_id     INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    database_name   TEXT NOT NULL,
    xact_commit     INTEGER NOT NULL DEFAULT 0,
    xact_rollback   INTEGER NOT NULL DEFAULT 0,
    temp_files      INTEGER NOT NULL DEFAULT 0,
    temp_bytes      INTEGER NOT NULL DEFAULT 0,
    deadlocks       INTEGER NOT NULL DEFAULT 0,
    confl_lock      INTEGER NOT NULL DEFAULT 0,
    confl_snapshot  INTEGER NOT NULL DEFAULT 0,
    cache_hit_ratio REAL,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Multi-row tables (multiple rows per instance)
CREATE TABLE current_long_running_queries (
    instance_id      INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    pid              INTEGER NOT NULL,
    usename          TEXT,
    datname          TEXT,
    query            TEXT,
    state            TEXT,
    wait_event_type  TEXT,
    wait_event       TEXT,
    query_start      DATETIME,
    duration_seconds REAL,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, pid)
);

CREATE TABLE current_idle_in_transaction (
    instance_id      INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    pid              INTEGER NOT NULL,
    usename          TEXT,
    datname          TEXT,
    state            TEXT,
    xact_start       DATETIME,
    duration_seconds REAL,
    query            TEXT,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, pid)
);

CREATE TABLE current_blocked_queries (
    instance_id           INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    blocked_pid           INTEGER NOT NULL,
    blocked_user          TEXT,
    blocked_query         TEXT,
    blocked_start         DATETIME,
    wait_duration_seconds REAL,
    blocking_pid          INTEGER,
    blocking_user         TEXT,
    blocking_query        TEXT,
    lock_type             TEXT,
    lock_mode             TEXT,
    relation              TEXT,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, blocked_pid)
);

CREATE TABLE current_query_stats (
    instance_id      INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    queryid          INTEGER NOT NULL,
    query            TEXT NOT NULL,
    calls            INTEGER NOT NULL,
    total_exec_time  REAL NOT NULL,
    mean_exec_time   REAL NOT NULL,
    min_exec_time    REAL,
    max_exec_time    REAL,
    rows             INTEGER,
    shared_blks_hit  INTEGER,
    shared_blks_read INTEGER,
    plans            INTEGER,
    total_plan_time  REAL,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, queryid)
);
CREATE INDEX idx_current_query_stats_time ON current_query_stats(instance_id, total_exec_time DESC);

CREATE TABLE current_table_stats (
    instance_id     INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    schemaname      TEXT NOT NULL,
    relname         TEXT NOT NULL,
    seq_scan        INTEGER NOT NULL DEFAULT 0,
    seq_tup_read    INTEGER NOT NULL DEFAULT 0,
    idx_scan        INTEGER,
    idx_tup_fetch   INTEGER,
    n_live_tup      INTEGER NOT NULL DEFAULT 0,
    n_dead_tup      INTEGER NOT NULL DEFAULT 0,
    last_vacuum     DATETIME,
    last_autovacuum DATETIME,
    last_analyze    DATETIME,
    table_size      INTEGER NOT NULL DEFAULT 0,
    index_size      INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, schemaname, relname)
);

CREATE TABLE current_index_stats (
    instance_id   INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    schemaname    TEXT NOT NULL,
    relname       TEXT NOT NULL,
    indexrelname  TEXT NOT NULL,
    idx_scan      INTEGER NOT NULL DEFAULT 0,
    idx_tup_read  INTEGER NOT NULL DEFAULT 0,
    idx_tup_fetch INTEGER NOT NULL DEFAULT 0,
    index_size    INTEGER NOT NULL DEFAULT 0,
    is_unique     INTEGER NOT NULL DEFAULT 0,
    is_primary    INTEGER NOT NULL DEFAULT 0,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, schemaname, relname, indexrelname)
);

CREATE TABLE current_bloat_stats (
    instance_id   INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    schemaname    TEXT NOT NULL,
    relname       TEXT NOT NULL,
    n_dead_tup    INTEGER NOT NULL DEFAULT 0,
    n_live_tup    INTEGER NOT NULL DEFAULT 0,
    bloat_percent REAL NOT NULL DEFAULT 0,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, schemaname, relname)
);

-- +migrate Down
DROP TABLE IF EXISTS current_bloat_stats;
DROP TABLE IF EXISTS current_index_stats;
DROP TABLE IF EXISTS current_table_stats;
DROP INDEX IF EXISTS idx_current_query_stats_time;
DROP TABLE IF EXISTS current_query_stats;
DROP TABLE IF EXISTS current_blocked_queries;
DROP TABLE IF EXISTS current_idle_in_transaction;
DROP TABLE IF EXISTS current_long_running_queries;
DROP TABLE IF EXISTS current_database_stats;
DROP TABLE IF EXISTS current_lock_stats;
DROP TABLE IF EXISTS current_connection_activity;
```

---

## Phase 2: Storage Layer

### 2.1 Add to Storage Interface (`internal/storage/sqlite/storage.go`)

Add these methods to the `Storage` interface:

```go
// Current state operations (for dashboard)
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
```

### 2.2 Implementation Patterns

**Single-row tables (UPSERT):**
```go
func (s *SQLiteStorage) SaveCurrentConnectionActivity(ctx context.Context, instanceID int64, activity *models.ConnectionActivity) error {
    _, err := s.writeDB.ExecContext(ctx, `
        INSERT INTO current_connection_activity (instance_id, active_count, ..., updated_at)
        VALUES (?, ?, ..., CURRENT_TIMESTAMP)
        ON CONFLICT(instance_id) DO UPDATE SET
            active_count = excluded.active_count,
            ...,
            updated_at = CURRENT_TIMESTAMP
    `, instanceID, activity.ActiveCount, ...)
    return err
}
```

**Multi-row tables (UPSERT + delete stale):**
```go
func (s *SQLiteStorage) SaveCurrentQueryStats(ctx context.Context, instanceID int64, stats []models.QueryStat) error {
    // Use a single per-batch marker so stale cleanup works even if calls happen within the same second.
    batchTime := time.Now().UTC().Format(time.RFC3339Nano)
    tx, _ := s.writeDB.BeginTx(ctx, nil)
    defer tx.Rollback()

    // UPSERT all current rows
    for _, stat := range stats {
        tx.ExecContext(ctx, `
            INSERT INTO current_query_stats (..., updated_at) VALUES (?, ..., ?)
            ON CONFLICT(instance_id, queryid) DO UPDATE SET ..., updated_at = excluded.updated_at
        `, instanceID, ..., batchTime)
    }

    // Delete stale rows not in current batch
    tx.ExecContext(ctx, `DELETE FROM current_query_stats WHERE instance_id = ? AND updated_at != ?`, instanceID, batchTime)

    return tx.Commit()
}
```

---

## Phase 3: Collector Updates

### 3.1 Dual-Write Pattern

Each collector writes to BOTH historical (snapshot-based) and current tables:

**Activity Collector (`internal/collector/activity/activity.go`):**
```go
func (c *ActivityCollector) Collect(ctx context.Context, snapshotID int64) error {
    activity, _ := c.PGClient().GetConnectionActivity(ctx)

    // Historical (for trends)
    c.Storage().SaveConnectionActivity(ctx, snapshotID, activity)
    // Current (for dashboard)
    c.Storage().SaveCurrentConnectionActivity(ctx, c.InstanceID(), activity)

    longRunning, _ := c.PGClient().GetLongRunningQueries(ctx, c.longRunningThreshold)
    c.Storage().SaveLongRunningQueries(ctx, snapshotID, longRunning)
    c.Storage().SaveCurrentLongRunningQueries(ctx, c.InstanceID(), longRunning)

    idleInTx, _ := c.PGClient().GetIdleInTransaction(ctx, c.idleInTxThreshold)
    c.Storage().SaveIdleInTransaction(ctx, snapshotID, idleInTx)
    c.Storage().SaveCurrentIdleInTransaction(ctx, c.InstanceID(), idleInTx)

    return nil
}
```

### 3.2 Files to Modify

Apply dual-write pattern to all collectors:
- `internal/collector/activity/activity.go`
- `internal/collector/locks/locks.go`
- `internal/collector/query/stats.go`
- `internal/collector/resource/database.go`
- `internal/collector/resource/tables.go`
- `internal/collector/resource/indexes.go`
- `internal/collector/schema/bloat.go`

---

## Phase 4: Dashboard Handler Updates

### 4.1 Update PageStorage Interface (`internal/api/handlers/pages.go`)

```go
type PageStorage interface {
    // Keep for metadata
    GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
    GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
    GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error)

    // Current state operations (replace snapshot-based methods)
    GetCurrentQueryStats(ctx context.Context, instanceID int64) ([]models.QueryStat, error)
    GetCurrentTableStats(ctx context.Context, instanceID int64) ([]models.TableStat, error)
    GetCurrentIndexStats(ctx context.Context, instanceID int64) ([]models.IndexStat, error)
    GetCurrentBloatStats(ctx context.Context, instanceID int64) ([]models.BloatInfo, error)
    GetCurrentConnectionActivity(ctx context.Context, instanceID int64) (*models.ConnectionActivity, error)
    GetCurrentLongRunningQueries(ctx context.Context, instanceID int64) ([]models.LongRunningQuery, error)
    GetCurrentBlockedQueries(ctx context.Context, instanceID int64) ([]models.BlockedQuery, error)
    GetCurrentDatabaseStats(ctx context.Context, instanceID int64) (*models.ExtendedDatabaseStats, *float64, error)
}
```

### 4.2 Update Dashboard Handler

```go
func (h *PageHandler) Dashboard(c echo.Context) error {
    ctx := c.Request().Context()

    // Get latest snapshot for metadata only
    snapshot, _ := h.storage.GetLatestSnapshot(ctx, h.instanceID)
    if snapshot != nil {
        data.LastSnapshot = snapshot.CapturedAt
    }

    // Get data from CURRENT tables (not snapshot-based)
    stats, _ := h.storage.GetCurrentQueryStats(ctx, h.instanceID)
    activity, _ := h.storage.GetCurrentConnectionActivity(ctx, h.instanceID)
    longRunning, _ := h.storage.GetCurrentLongRunningQueries(ctx, h.instanceID)
    blocked, _ := h.storage.GetCurrentBlockedQueries(ctx, h.instanceID)
    dbStats, _, _ := h.storage.GetCurrentDatabaseStats(ctx, h.instanceID)

    // Process and render...
}
```

---

## Phase 5: Test Updates

### 5.1 Update Migration Count

`internal/storage/sqlite/storage_test.go`:
```go
if len(status) != 11 {  // was 10
    t.Errorf("Expected 11 migrations applied, got %d", len(status))
}
```

### 5.2 Update Mocks

Add new methods to mock implementations in:
- `internal/scheduler/scheduler_test.go`
- `internal/collector/collector_test.go`
- `internal/api/handlers/pages_test.go`
- `internal/analyzer/analyzer_test.go`

---

## Verification

1. **Build:** `go build ./...`
2. **Test:** `go test ./...`
3. **Run app:** Start and verify dashboard shows data immediately
4. **Verify data:**
   ```sql
   SELECT * FROM current_connection_activity;
   SELECT * FROM current_query_stats LIMIT 5;
   ```
5. **Test intervals:** Wait 2+ minutes, confirm dashboard never shows N/A

---

## Files to Create

- `internal/storage/sqlite/migrations/011_create_current_state_tables.sql`

## Files to Modify

- `internal/storage/sqlite/storage.go` - Add 20+ methods
- `internal/api/handlers/pages.go` - Use current tables
- `internal/collector/activity/activity.go` - Dual write
- `internal/collector/locks/locks.go` - Dual write
- `internal/collector/query/stats.go` - Dual write
- `internal/collector/resource/database.go` - Dual write
- `internal/collector/resource/tables.go` - Dual write
- `internal/collector/resource/indexes.go` - Dual write
- `internal/collector/schema/bloat.go` - Dual write
- `internal/storage/sqlite/storage_test.go` - Migration count
- `internal/scheduler/scheduler_test.go` - Mock methods
- `internal/collector/collector_test.go` - Mock methods
- `internal/api/handlers/pages_test.go` - Mock methods
