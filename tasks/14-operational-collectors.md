# Task 14: Operational State Collectors

Add low-risk, high-signal operational collectors: `pg_stat_activity`, `pg_locks`, `pg_stat_database` extras, with dashboard integration and suggestions.

## Summary

| Component | Files | Purpose |
|-----------|-------|---------|
| Activity Collector | `internal/collector/activity/activity.go` | Connection counts, long-running queries, idle-in-tx |
| Locks Collector | `internal/collector/locks/locks.go` | Lock stats, blocked/blocking queries |
| Database Extras | `internal/collector/resource/database.go` (extend) | Txn rates, temp files, deadlocks |
| Models | `internal/models/models.go` (extend) | New data structures |
| Storage | `internal/storage/sqlite/` | New tables + methods |
| Postgres Client | `internal/postgres/pgx_client.go` (extend) | New query methods |
| Analyzer | `internal/analyzer/` (extend) | Process operational data |
| Suggestion Rules | `internal/suggester/rules/` (5 new) | Actionable recommendations |
| Dashboard | `internal/web/templates/dashboard.html` | New stat cards + sections |

---

## Phase 1: Data Layer

### 1.1 Models (`internal/models/models.go`)

Add these structs:

```go
// ConnectionActivity - snapshot of pg_stat_activity aggregates
type ConnectionActivity struct {
    ID               int64
    SnapshotID       int64
    ActiveCount      int
    IdleCount        int
    IdleInTxCount    int
    IdleInTxAborted  int
    WaitingCount     int
    TotalConnections int
    MaxConnections   int
}

// LongRunningQuery - queries exceeding threshold
type LongRunningQuery struct {
    ID              int64
    SnapshotID      int64
    PID             int
    Username        string
    DatabaseName    string
    Query           string
    State           string
    WaitEventType   *string
    WaitEvent       *string
    QueryStart      time.Time
    DurationSeconds float64
}

// IdleInTransaction - connections idle in transaction
type IdleInTransaction struct {
    ID              int64
    SnapshotID      int64
    PID             int
    Username        string
    DatabaseName    string
    State           string
    XactStart       time.Time
    DurationSeconds float64
    Query           string
}

// LockStats - aggregated lock statistics
type LockStats struct {
    ID                int64
    SnapshotID        int64
    TotalLocks        int
    GrantedLocks      int
    WaitingLocks      int
    AccessShareLocks  int
    RowExclusiveLocks int
    ExclusiveLocks    int
}

// BlockedQuery - query blocked by another
type BlockedQuery struct {
    ID              int64
    SnapshotID      int64
    BlockedPID      int
    BlockedUser     string
    BlockedQuery    string
    BlockedStart    time.Time
    WaitDuration    float64
    BlockingPID     int
    BlockingUser    string
    BlockingQuery   string
    LockType        string
    LockMode        string
    Relation        *string
}

// ExtendedDatabaseStats - operational database metrics
type ExtendedDatabaseStats struct {
    ID            int64
    SnapshotID    int64
    DatabaseName  string
    XactCommit    int64
    XactRollback  int64
    TempFiles     int64
    TempBytes     int64
    Deadlocks     int64
    ConflLock     int64
    ConflSnapshot int64
}
```

### 1.2 Migration (`internal/storage/sqlite/migrations/010_operational_stats.sql`)

Create tables: `connection_activity`, `long_running_queries`, `idle_in_transaction`, `lock_stats`, `blocked_queries`, `extended_database_stats`

### 1.3 Storage Methods (`internal/storage/sqlite/storage.go`)

Add methods:
- `SaveConnectionActivity(ctx, snapshotID, *ConnectionActivity) error`
- `SaveLongRunningQueries(ctx, snapshotID, []LongRunningQuery) error`
- `SaveIdleInTransaction(ctx, snapshotID, []IdleInTransaction) error`
- `SaveLockStats(ctx, snapshotID, *LockStats) error`
- `SaveBlockedQueries(ctx, snapshotID, []BlockedQuery) error`
- `SaveExtendedDatabaseStats(ctx, snapshotID, *ExtendedDatabaseStats) error`
- Corresponding `Get*` methods

### 1.4 Postgres Client (`internal/postgres/pgx_client.go`)

Add methods:
- `GetConnectionActivity(ctx) (*ConnectionActivity, error)`
- `GetLongRunningQueries(ctx, thresholdSec float64) ([]LongRunningQuery, error)`
- `GetIdleInTransaction(ctx, thresholdSec float64) ([]IdleInTransaction, error)`
- `GetLockStats(ctx) (*LockStats, error)`
- `GetBlockedQueries(ctx) ([]BlockedQuery, error)`
- `GetExtendedDatabaseStats(ctx) (*ExtendedDatabaseStats, error)`

**Key SQL Queries:**

```sql
-- Connection activity summary
SELECT
    COUNT(*) FILTER (WHERE state = 'active') as active_count,
    COUNT(*) FILTER (WHERE state = 'idle') as idle_count,
    COUNT(*) FILTER (WHERE state = 'idle in transaction') as idle_in_tx_count,
    COUNT(*) FILTER (WHERE state = 'idle in transaction (aborted)') as idle_in_tx_aborted,
    COUNT(*) as total_connections,
    COUNT(*) FILTER (WHERE wait_event_type IS NOT NULL AND wait_event_type != 'Activity') as waiting_count,
    (SELECT setting::int FROM pg_settings WHERE name = 'max_connections') as max_connections
FROM pg_stat_activity
WHERE backend_type = 'client backend';

-- Long-running queries
SELECT pid, usename, datname, query, state, wait_event_type, wait_event, query_start,
       EXTRACT(EPOCH FROM (NOW() - query_start)) as duration_seconds
FROM pg_stat_activity
WHERE state = 'active' AND query_start IS NOT NULL AND backend_type = 'client backend'
  AND EXTRACT(EPOCH FROM (NOW() - query_start)) > $1
  AND pid != pg_backend_pid()
ORDER BY duration_seconds DESC;

-- Blocked queries
SELECT blocked.pid as blocked_pid, blocked.usename as blocked_user, blocked.query as blocked_query,
       blocked.query_start as blocked_start,
       EXTRACT(EPOCH FROM (NOW() - blocked.query_start)) as wait_duration_seconds,
       blocking.pid as blocking_pid, blocking.usename as blocking_user, blocking.query as blocking_query,
       bl.locktype as lock_type, bl.mode as lock_mode, rel.relname as relation
FROM pg_locks bl
JOIN pg_stat_activity blocked ON bl.pid = blocked.pid
JOIN pg_locks kl ON bl.locktype = kl.locktype AND bl.pid != kl.pid
    AND bl.database IS NOT DISTINCT FROM kl.database
    AND bl.relation IS NOT DISTINCT FROM kl.relation
JOIN pg_stat_activity blocking ON kl.pid = blocking.pid
LEFT JOIN pg_class rel ON bl.relation = rel.oid
WHERE NOT bl.granted AND kl.granted
ORDER BY wait_duration_seconds DESC;

-- Extended database stats
SELECT datname, xact_commit, xact_rollback, temp_files, temp_bytes, deadlocks,
       confl_lock, confl_snapshot
FROM pg_stat_database WHERE datname = current_database();
```

---

## Phase 2: Collectors

### 2.1 Activity Collector (`internal/collector/activity/activity.go`)

```go
const (
    ActivityCollectorName    = "activity"
    DefaultActivityInterval  = 30 * time.Second
    DefaultLongRunningThreshold = 60.0  // seconds
    DefaultIdleInTxThreshold    = 60.0  // seconds
)

type ActivityCollector struct {
    collector.BaseCollector
    longRunningThreshold float64
    idleInTxThreshold    float64
}

func (c *ActivityCollector) Collect(ctx context.Context, snapshotID int64) error {
    // 1. Get connection activity summary
    // 2. Get long-running queries (> threshold)
    // 3. Get idle-in-transaction connections (> threshold)
    // 4. Save all to storage
}
```

### 2.2 Locks Collector (`internal/collector/locks/locks.go`)

```go
const (
    LocksCollectorName   = "locks"
    DefaultLocksInterval = 30 * time.Second
)

type LocksCollector struct {
    collector.BaseCollector
}

func (c *LocksCollector) Collect(ctx context.Context, snapshotID int64) error {
    // 1. Get lock statistics
    // 2. Get blocked queries
    // 3. Save to storage
}
```

### 2.3 Extend Database Collector (`internal/collector/resource/database.go`)

Add collection of: `xact_commit`, `xact_rollback`, `temp_files`, `temp_bytes`, `deadlocks`, `confl_*`

### 2.4 Register in `cmd/pganalyzer/main.go`

```go
coordinator.RegisterCollectors(
    // ... existing collectors ...
    activity.NewActivityCollector(activity.ActivityCollectorConfig{...}),
    locks.NewLocksCollector(locks.LocksCollectorConfig{...}),
)
```

---

## Phase 3: Analyzer Extensions

### 3.1 Extend AnalysisResult (`internal/analyzer/analyzer.go`)

```go
type AnalysisResult struct {
    // ... existing fields ...
    ActivityStats    *ActivityAnalysis
    LockStats        *LockAnalysis
    TransactionStats *TransactionAnalysis
}

type ActivityAnalysis struct {
    ConnectionUtilization float64
    LongRunningQueries    []models.LongRunningQuery
    IdleInTransaction     []models.IdleInTransaction
    WaitingConnections    int
}

type LockAnalysis struct {
    BlockedQueries    []models.BlockedQuery
    TotalWaitingLocks int
}

type TransactionAnalysis struct {
    TempFilesCreated int64
    TempBytesWritten int64
    DeadlockCount    int64
}
```

### 3.2 Add Analysis Logic (`internal/analyzer/main.go`)

Fetch operational data from storage and populate `AnalysisResult`.

---

## Phase 4: Suggestion Rules

### 4.1 Config (`internal/suggester/rule.go`)

```go
type Config struct {
    // ... existing ...
    LongRunningQuerySeconds      float64 // default: 60
    LongRunningCriticalSeconds   float64 // default: 300
    IdleInTxSeconds              float64 // default: 60
    IdleInTxCriticalSeconds      float64 // default: 300
    BlockedQuerySeconds          float64 // default: 10
    BlockedQueryCriticalSeconds  float64 // default: 60
    TempBytesWarning             int64   // default: 1GB
    TempBytesCritical            int64   // default: 10GB
    DeadlocksWarning             int64   // default: 1
    ConnectionUtilizationWarning float64 // default: 0.8
}
```

### 4.2 New Rules (`internal/suggester/rules/`)

| File | Rule ID | Trigger | Severity |
|------|---------|---------|----------|
| `long_running_query.go` | `long_running_query` | Query > 60s | Warning/Critical |
| `idle_in_transaction.go` | `idle_in_transaction` | Idle-in-tx > 60s | Warning/Critical |
| `lock_contention.go` | `lock_contention` | Blocked > 10s | Warning/Critical |
| `high_temp_usage.go` | `high_temp_usage` | Temp bytes > 1GB | Warning/Critical |
| `high_deadlocks.go` | `high_deadlocks` | Deadlocks > 0 | Warning/Critical |

### 4.3 Register in `cmd/pganalyzer/main.go`

---

## Phase 5: Dashboard UI

### 5.1 Page Data (`internal/api/handlers/pages.go`)

```go
type DashboardPageData struct {
    // ... existing ...
    ActivityStats      *ActivityStats
    LongRunningCount   int
    BlockedCount       int
    BlockedQueries     []BlockedQuerySummary
}
```

### 5.2 Template (`internal/web/templates/dashboard.html`)

**New stat cards:**
- Active Connections (X / max_connections)
- Long-Running Queries (count > 60s)
- Blocked Queries (count waiting for locks)

**New sections:**
- Connection Activity (grid: active, idle, idle-in-tx, waiting)
- Lock Activity (list of blocked queries with wait time)

---

## Verification

1. **Build & Run:**
   ```bash
   go build ./cmd/pganalyzer && ./pganalyzer
   ```

2. **Check collectors running:** Look for log lines `[activity] collecting...` and `[locks] collecting...`

3. **Verify data collection:**
   ```sql
   SELECT * FROM connection_activity ORDER BY id DESC LIMIT 1;
   SELECT * FROM lock_stats ORDER BY id DESC LIMIT 1;
   ```

4. **Test suggestions:** Create long-running query in test DB, verify suggestion appears

5. **Dashboard:** Open http://localhost:8080, verify new stat cards and sections render

6. **Run tests:**
   ```bash
   go test ./internal/collector/activity/...
   go test ./internal/collector/locks/...
   go test ./internal/suggester/rules/...
   ```

---

## Files to Create

- `internal/collector/activity/activity.go`
- `internal/collector/activity/activity_test.go`
- `internal/collector/locks/locks.go`
- `internal/collector/locks/locks_test.go`
- `internal/storage/sqlite/migrations/010_operational_stats.sql`
- `internal/suggester/rules/long_running_query.go`
- `internal/suggester/rules/idle_in_transaction.go`
- `internal/suggester/rules/lock_contention.go`
- `internal/suggester/rules/high_temp_usage.go`
- `internal/suggester/rules/high_deadlocks.go`

## Files to Modify

- `internal/models/models.go` - Add new model structs
- `internal/postgres/client.go` - Add interface methods
- `internal/postgres/pgx_client.go` - Implement new queries
- `internal/storage/sqlite/storage.go` - Add save/get methods
- `internal/analyzer/analyzer.go` - Extend AnalysisResult
- `internal/analyzer/main.go` - Add operational analysis
- `internal/suggester/rule.go` - Add config options
- `internal/api/handlers/pages.go` - Add dashboard data
- `internal/web/templates/dashboard.html` - Add UI sections
- `cmd/pganalyzer/main.go` - Register collectors and rules
