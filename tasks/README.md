# Implementation Tasks

This directory contains implementation tasks for PostgreSQL Analyzer (pganalyzer).

## Task Overview

| # | Task | Description | Dependencies |
|---|------|-------------|--------------|
| 01 | [Project Setup](01-project-setup.md) | Initialize Go module and project structure | - |
| 02 | [Configuration](02-configuration.md) | YAML config loading with env var expansion | 01 |
| 03 | [Storage (SQLite)](03-storage-sqlite.md) | SQLite storage layer with migrations | 01, 02 |
| 04 | [PostgreSQL Client](04-postgres-client.md) | PostgreSQL client for stats collection | 01, 02 |
| 05 | [Collectors](05-collectors.md) | Data collectors for metrics | 03, 04 |
| 06 | [Analyzer](06-analyzer.md) | Analysis logic for issue detection | 03, 05 |
| 07 | [Suggester](07-suggester.md) | Rules-based recommendation engine | 03, 06 |
| 08 | [Scheduler](08-scheduler.md) | Job scheduler for collection/analysis | 05, 06, 07 |
| 09 | [API Server](09-api-server.md) | REST API endpoints | 02, 03, 08 |
| 10 | [Web UI](10-web-ui.md) | Server-rendered HTML pages | 09 |
| 11 | [Production Readiness](11-production-readiness.md) | Docker, docs, testing | All |

## Dependency Graph

```
01 Project Setup
 │
 ├── 02 Configuration
 │    │
 │    ├── 03 Storage ─────────────────┐
 │    │    │                          │
 │    │    └── 05 Collectors ◄────────┤
 │    │         │                     │
 │    │         └── 06 Analyzer       │
 │    │              │                │
 │    │              └── 07 Suggester │
 │    │                   │           │
 │    │                   └── 08 Scheduler
 │    │                        │
 │    └── 04 PostgreSQL Client─┘
 │
 └── 09 API Server
      │
      └── 10 Web UI
           │
           └── 11 Production Readiness
```

## Suggested Implementation Order

**Phase 1: Foundation (Tasks 01-04)**
1. Project Setup
2. Configuration
3. Storage (SQLite)
4. PostgreSQL Client

**Phase 2: Data Pipeline (Tasks 05-08)**
5. Collectors
6. Analyzer
7. Suggester
8. Scheduler

**Phase 3: User Interface (Tasks 09-10)**
9. API Server
10. Web UI

**Phase 4: Deployment (Task 11)**
11. Production Readiness

## Progress Tracking

Update this section as tasks are completed:

- [x] 01 - Project Setup (completed 2026-01-10)
- [x] 02 - Configuration (completed 2026-01-10)
- [x] 03 - Storage (SQLite) (completed 2026-01-10)
- [x] 04 - PostgreSQL Client (completed 2026-01-10)
- [x] 05 - Collectors (completed 2026-01-10)
- [x] 06 - Analyzer (completed 2026-01-10)
- [x] 07 - Suggester (completed 2026-01-10)
- [x] 08 - Scheduler (completed 2026-01-10)
- [ ] 09 - API Server
- [ ] 10 - Web UI
- [ ] 11 - Production Readiness

## Quick Commands

Common development commands using [Task](https://taskfile.dev/):

```bash
# Build the application
task build

# Run the application
task run

# Run tests
task test

# Run tests with coverage
task test:coverage

# Format code
task fmt

# Run linters
task lint

# Clean build artifacts
task clean

# Show version info
task version

# Show all available tasks
task
```

## What Was Completed in Task 01

- Go module initialized (`github.com/user/pganalyzer`)
- Directory structure created matching tech design
- Dependencies added: pgx/v5, sqlite, echo/v4, yaml.v3
- Entry point created with signal handling and version flags
- Example configuration file created
- Taskfile.yaml created with common commands

## What Was Completed in Task 02

- Config structs defined in `internal/config/config.go`:
  - `Config`, `PostgresConfig`, `StorageConfig`, `SchedulerConfig`, `ServerConfig`, `ThresholdsConfig`
  - Custom `Duration` type with YAML marshaling/unmarshaling
  - `Default()` function providing sensible defaults
- Config loading in `internal/config/loader.go`:
  - YAML file loading with path override via `PGANALYZER_CONFIG` env var
  - Environment variable expansion with `${VAR}` and `${VAR:-default}` syntax
  - Helper functions: `Load()`, `MustLoad()`, `LoadFromString()`
  - `PostgresConfig.FormatConnectionString()` helper
- Validation in `internal/config/validation.go`:
  - Required field validation (host, database, user)
  - Port range validation (1-65535)
  - Threshold validation (positive values, valid ranges)
  - SSLMode validation (disable, allow, prefer, require)
  - Descriptive error messages with field paths
- Comprehensive tests in `internal/config/config_test.go`:
  - 22 test cases covering all functionality
  - Tests for YAML parsing, env var expansion, validation errors, defaults

## What Was Completed in Task 03

- Data models defined in `internal/models/models.go`:
  - `Instance`, `Snapshot`, `QueryStat`, `QueryStatDelta`
  - `TableStat`, `IndexStat`, `Suggestion`, `ExplainPlan`
  - Severity and Status constants
- Migration system in `internal/storage/sqlite/migrations.go`:
  - Embedded SQL migrations using `embed.FS`
  - `_migrations` table to track applied migrations
  - `Migrate()`, `Rollback()`, `GetMigrationStatus()` functions
  - Transaction-safe migration application
- Schema migrations in `internal/storage/sqlite/migrations/`:
  - `001_create_instances.sql` - instances table with unique constraint
  - `002_create_snapshots.sql` - snapshots table with index
  - `003_create_query_stats.sql` - query_stats with queryid index
  - `004_create_table_stats.sql` - table_stats table
  - `005_create_index_stats.sql` - index_stats table
  - `006_create_suggestions.sql` - suggestions with deduplication
  - `007_create_explain_plans.sql` - explain_plans table
  - All tables support cascade deletes via foreign keys
- Storage implementation in `internal/storage/sqlite/storage.go`:
  - `Storage` interface with all CRUD operations
  - `NewStorage(dbPath)` with auto-migration
  - Connection pooling with WAL mode and foreign keys enabled
  - Instance operations: Get, GetByName, Create, GetOrCreate, List
  - Snapshot operations: Create, GetByID, GetLatest, List
  - Query stats: Save, Get, GetDelta (with stats reset handling)
  - Table/Index stats: Save, Get
  - Suggestions: Upsert, GetActive, GetByID, Dismiss, Resolve
  - Explain plans: Save, Get (returns latest)
  - Maintenance: PurgeOldSnapshots with cascade deletes
- Comprehensive tests in `internal/storage/sqlite/storage_test.go`:
  - 30+ test cases covering all operations
  - Tests for migrations, CRUD, delta calculation, cascade deletes
  - Stats reset detection testing

## What Was Completed in Task 04

- Extended models in `internal/models/models.go`:
  - Added `DatabaseStats` struct for database-level statistics (cache hit ratio)
  - Added `BloatInfo` struct for table bloat information
  - Added `IndexDetail` struct for extended index information
- Client interface in `internal/postgres/client.go`:
  - Defined `Client` interface with all required methods
  - Defined `ClientConfig` struct with connection pool settings
  - `DefaultClientConfig()` function with sensible defaults
- PostgreSQL client in `internal/postgres/pgx_client.go`:
  - Connection management: `NewClient()`, `Connect()`, `Close()`, `Ping()`
  - Uses `pgxpool` for connection pooling with configurable pool settings
  - Stats collection:
    - `GetStatStatements()` - query statistics from pg_stat_statements
    - `GetStatTables()` - table statistics from pg_stat_user_tables
    - `GetStatIndexes()` - index statistics from pg_stat_user_indexes
    - `GetDatabaseStats()` - cache hit ratio from pg_stat_database
  - Schema analysis:
    - `GetTableBloat()` - tables with significant dead tuples
    - `GetIndexDetails()` - extended index info with definitions
  - Query analysis:
    - `Explain()` - EXPLAIN with JSON format, BUFFERS, VERBOSE, SETTINGS
    - Safety check prevents ANALYZE on write queries (INSERT/UPDATE/DELETE)
  - Metadata queries:
    - `GetVersion()` - PostgreSQL version string
    - `GetStatsResetTime()` - stats reset time from pg_stat_statements_info (PG14+)
- Comprehensive tests in `internal/postgres/client_test.go`:
  - Config validation tests
  - Connection string building tests
  - Not-connected error handling tests
  - Write query detection tests
  - Model structure tests

## What Was Completed in Task 05

- Collector interface and base collector in `internal/collector/`:
  - `Collector` interface with `Name()`, `Collect()`, `Interval()` methods
  - `CollectorConfig` struct for common settings
  - `BaseCollector` with shared logic (PG client, storage, instance ID, logging)
- QueryStatsCollector in `internal/collector/query/stats.go`:
  - 1-minute default interval
  - Fetches from pg_stat_statements via PG client
  - Stores in query_stats table
  - Detects stats reset by comparing stats_reset timestamp
  - Logs warning on stats reset detection
- TableStatsCollector in `internal/collector/resource/tables.go`:
  - 5-minute default interval
  - Fetches from pg_stat_user_tables
  - Stores in table_stats table with sizes
- IndexStatsCollector in `internal/collector/resource/indexes.go`:
  - 5-minute default interval
  - Fetches from pg_stat_user_indexes
  - Stores in index_stats table with unique/primary flags
- DatabaseStatsCollector in `internal/collector/resource/database.go`:
  - 1-minute default interval
  - Fetches cache hit ratio from pg_stat_database
  - Stores in snapshot metadata (cache_hit_ratio column)
- BloatCollector in `internal/collector/schema/bloat.go`:
  - 1-hour default interval
  - Calculates dead tuple ratio
  - Filters tables with significant bloat (>1000 dead tuples by default)
  - Stores bloat metrics in bloat_stats table
- Snapshot Coordinator in `internal/collector/coordinator.go`:
  - Manages snapshot lifecycle across collectors
  - Creates single snapshot per collection cycle
  - Passes snapshot ID to all collectors
  - Handles partial failures (tracks errors per collector)
  - `CollectionResult` with error tracking
- New migrations:
  - `008_add_cache_hit_ratio.sql` - adds cache_hit_ratio to snapshots
  - `009_create_bloat_stats.sql` - creates bloat_stats table
- Extended storage interface:
  - `UpdateSnapshotCacheHitRatio()` for database stats collector
  - `SaveBloatStats()` and `GetBloatStats()` for bloat collector
- Updated models:
  - Added `CacheHitRatio` field to `Snapshot` struct
- Comprehensive tests in `internal/collector/collector_test.go`:
  - Mock PostgreSQL client for unit tests
  - Tests for each collector in isolation
  - Tests for snapshot coordination
  - Tests for stats reset detection
  - Tests for partial failure handling
  - Tests for context cancellation
  - Tests for default and custom intervals

## What Was Completed in Task 06

- Analyzer interface and types in `internal/analyzer/analyzer.go`:
  - `Analyzer` interface with `Analyze(ctx, snapshotID)` method
  - `AnalysisResult` struct aggregating all issue types
  - `SlowQuery` struct with execution time metrics and delta values
  - `CacheAnalysis` struct with overall ratio and poor-performing queries
  - `TableIssue` struct for bloat, stale vacuum/analyze, missing index issues
  - `IndexIssue` struct for unused and duplicate index detection
  - `Config` struct with configurable thresholds
  - `ConfigFromThresholds()` helper to convert from config package
  - `Storage` interface subset for testability
- SlowQueryAnalyzer in `internal/analyzer/slow_queries.go`:
  - Identifies queries exceeding mean execution time threshold
  - Uses absolute values from snapshot for historical context
  - Calculates per-query cache hit ratio
  - `AnalyzeWithDeltas()` for recent performance analysis
  - Results sorted by total execution time (most impactful first)
- CacheAnalyzer in `internal/analyzer/cache.go`:
  - Database-level cache hit ratio from snapshot
  - Per-query cache hit ratio calculation
  - Flags queries with poor cache performance (below threshold)
  - Filters queries with minimal block activity to reduce noise
  - `AnalyzeWithDeltas()` for accurate recent cache analysis
- TableAnalyzer in `internal/analyzer/tables.go`:
  - High bloat detection using bloat_stats (dead tuple ratio)
  - Stale vacuum detection (days since last vacuum + dead tuples)
  - Stale analyze detection (days since last analyze)
  - Missing index detection (high sequential scan ratio on large tables)
  - Severity escalation (warning → critical) based on thresholds
  - Skips small tables to reduce noise
- IndexAnalyzer in `internal/analyzer/indexes.go`:
  - Unused index detection (idx_scan = 0)
  - Excludes primary keys and unique indexes (constraint purposes)
  - Duplicate index detection using name patterns and size similarity
  - Space savings calculation for each issue
  - `formatBytes()` helper for human-readable sizes
- MainAnalyzer orchestrator in `internal/analyzer/main.go`:
  - Orchestrates all sub-analyzers (slow query, cache, table, index)
  - Handles partial failures gracefully (errors tracked, others continue)
  - `Analyze()` for single snapshot analysis
  - `AnalyzeWithTimeRange()` for delta-based analysis
  - `GetIssueCount()`, `GetCriticalCount()`, `GetWarningCount()` helpers
- Comprehensive tests in `internal/analyzer/analyzer_test.go`:
  - Mock storage implementation for isolated testing
  - 16 test cases covering all analyzers
  - Tests for slow query detection and sorting
  - Tests for cache analysis with thresholds
  - Tests for table issues (bloat, stale vacuum, missing index)
  - Tests for index issues (unused, duplicate detection)
  - Tests for main analyzer orchestration
  - Tests for partial failure handling
  - Tests for result count helpers

## What Was Completed in Task 07

- Rule interface and types in `internal/suggester/rule.go`:
  - `Rule` interface with `ID()`, `Name()`, `Evaluate()` methods
  - `Suggestion` struct with RuleID, Severity, Title, Description, TargetObject, Metadata
  - `ToModel()` method for converting to storage model
  - `Config` struct with configurable thresholds for all rules
  - `DefaultConfig()` function with sensible defaults
- SlowQueryRule in `internal/suggester/rules/slow_query.go`:
  - Rule ID: `slow_query`
  - Trigger: `mean_exec_time > threshold`
  - Severity: warning (>1s), critical (>5s)
  - Includes execution stats and optimization hints
  - Metadata: queryid, mean_time, call_count, cache_hit_ratio
- UnusedIndexRule in `internal/suggester/rules/unused_index.go`:
  - Rule ID: `unused_index`
  - Trigger: `idx_scan = 0` (excludes primary keys and unique constraints)
  - Severity: warning
  - Includes DROP INDEX statement in description
  - Metadata: index_size, table_name, space_savings
- MissingIndexRule in `internal/suggester/rules/missing_index.go`:
  - Rule ID: `missing_index`
  - Trigger: high seq_scan ratio on large tables
  - Severity: info (moderate ratio), warning (high ratio)
  - Skips tables below minimum size threshold
  - Metadata: seq_scan_ratio, table_size, n_live_tup
- BloatRule in `internal/suggester/rules/bloat.go`:
  - Rule ID: `table_bloat`
  - Trigger: dead_tup_ratio > threshold
  - Severity: warning (>20%), critical (>50%)
  - Recommends VACUUM or VACUUM FULL based on severity
  - Metadata: dead_tuples, live_tuples, bloat_ratio
- VacuumRule in `internal/suggester/rules/vacuum.go`:
  - Rule ID: `stale_vacuum`
  - Trigger: last_vacuum older than threshold with high dead tuples
  - Severity: warning
  - Includes VACUUM ANALYZE command
  - Metadata: last_vacuum, dead_tuples, days_since_vacuum
- CacheRule in `internal/suggester/rules/cache.go`:
  - Rule ID: `low_cache_hit`
  - Trigger: cache_hit_ratio < threshold
  - Severity: warning (<95%), critical (<90%)
  - Lists queries with poor cache performance
  - Metadata: hit_ratio, poor_query_count
- Suggester orchestrator in `internal/suggester/suggester.go`:
  - `Suggester` struct with registered rules
  - `RegisterRule()` and `RegisterRules()` methods
  - `Suggest()` runs all rules and manages suggestions
  - Deduplication by (rule_id, target_object)
  - Upserts new/updated suggestions via storage
  - Marks resolved suggestions when issues disappear
  - `SuggestResult` with counts: total, new, updated, resolved
  - `GetSuggestionStats()` returns counts by severity
- Comprehensive tests in `internal/suggester/suggester_test.go`:
  - Mock storage implementation for isolated testing
  - 14 test functions covering all rules
  - Tests for each rule with triggering and non-triggering data
  - Tests for severity calculation
  - Tests for deduplication logic
  - Tests for suggestion resolution when issues are fixed
  - Tests for suggestion stats aggregation
  - Tests for nil/empty analysis handling

## What Was Completed in Task 08

- Scheduler struct and configuration in `internal/scheduler/scheduler.go`:
  - `Scheduler` struct managing collection, analysis, and maintenance jobs
  - `Config` struct with references to coordinator, analyzer, suggester, storage
  - `NewScheduler()` constructor with required field validation
  - Default configuration with 5m snapshot interval, 15m analysis interval
- Ticker-based scheduling in `internal/scheduler/loops.go`:
  - `runCollectionLoop()` - runs at snapshot_interval (default 5m)
  - `runAnalysisLoop()` - runs at analysis_interval (default 15m)
  - `runMaintenanceLoop()` - runs daily for cleanup
  - Initial execution on start with proper delay handling
  - Respects context cancellation and stop signals
- Collection job:
  - Uses coordinator to run all collectors
  - Creates snapshot per collection cycle
  - Tracks collection duration and errors
  - Logs collection results with snapshot ID
- Analysis job:
  - Fetches latest snapshot for analysis
  - Runs analyzer on snapshot data
  - Runs suggester with analysis results
  - Tracks analysis duration and issue counts
- Maintenance job:
  - Runs daily (configurable)
  - Purges old snapshots based on retention config
  - Logs purge results
- Manual trigger in `TriggerSnapshot()`:
  - Runs immediate collection and analysis
  - Prevents concurrent manual triggers with mutex
  - Returns `TriggerResult` with all results
  - Updates health status
- Graceful shutdown:
  - `Stop()` and `StopWithTimeout()` methods
  - Closes stop channel to signal all loops
  - Waits for goroutines with timeout
  - Logs shutdown progress
- Health status tracking with `HealthStatus` struct:
  - `LastCollectionTime`, `LastCollectionSuccess`, `LastCollectionDuration`
  - `LastAnalysisTime`, `LastAnalysisSuccess`, `LastAnalysisDuration`
  - `LastMaintenanceTime`, `LastMaintenanceSuccess`
  - `TotalCollections`, `TotalAnalyses`, `FailedCollections`, `FailedAnalyses`
  - `GetHealth()` returns thread-safe snapshot for health checks
- Comprehensive tests in `internal/scheduler/scheduler_test.go`:
  - Mock storage implementing all required interfaces
  - Mock PG client for coordinator
  - Mock collector for testing
  - 11 test functions covering:
    - NewScheduler required field validation
    - Start/stop lifecycle
    - Collection loop creates snapshots at intervals
    - TriggerSnapshot manual execution
    - Sequential trigger calls work correctly
    - Health status tracking and updates
    - Graceful shutdown completes quickly
    - Context cancellation handling
    - Maintenance job runs without errors
    - Scheduler restart after stop
