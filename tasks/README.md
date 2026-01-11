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
| 12 | [Main Wiring](12-main-wiring.md) | Connect all components in main.go | 01-10 |
| 13 | [Tailwind Migration](13-tailwind-migration.md) | Migrate CSS to Tailwind, improve aesthetics | 10 |
| 14 | [Operational Collectors](14-operational-collectors.md) | pg_stat_activity, pg_locks, extended pg_stat_database | 05, 06, 07, 10 |

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
           ├── 11 Production Readiness
           │
           └── 13 Tailwind Migration
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
- [x] 09 - API Server (completed 2026-01-10)
- [x] 10 - Web UI (completed 2026-01-10)
- [x] 11 - Production Readiness (completed 2026-01-11)
- [x] 12 - Main Wiring (completed 2026-01-11)
- [x] 13 - Tailwind Migration (completed 2026-01-11)
- [x] 14 - Operational Collectors (completed 2026-01-11)

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

# Build CSS with Tailwind
task css

# Watch CSS changes (development)
task css:watch

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

## What Was Completed in Task 09

- Echo server setup in `internal/api/server.go`:
  - `Server` struct with Echo instance and dependencies
  - `ServerConfig` struct for server creation
  - `NewServer()` constructor with validation
  - Middleware configuration: Recover, RequestID, Logger, CORS
  - Route registration for all API endpoints
  - `Start()` and `Shutdown()` methods for graceful lifecycle
  - Custom HTTP error handler for consistent error responses
- Basic Auth middleware in `internal/api/middleware/auth.go`:
  - `BasicAuth()` middleware validates against config credentials
  - Skips authentication for `/health` endpoint
  - Returns 401 with WWW-Authenticate header on failure
  - `RequireAuth()` alternative for selective route protection
- Error handling in `internal/api/errors.go`:
  - `ErrorResponse` struct with error, code, and details fields
  - Error code constants: BAD_REQUEST, UNAUTHORIZED, NOT_FOUND, etc.
  - Helper functions: `BadRequest()`, `NotFound()`, `InternalError()`, etc.
  - `CustomHTTPErrorHandler` maps Echo errors to standard format
- Health endpoint in `internal/api/handlers/health.go`:
  - `GET /health` - public endpoint (no auth required)
  - Checks PostgreSQL connectivity via `Ping()`
  - Returns last snapshot time from storage
  - Status: "ok" or "degraded" based on PG connection
- Dashboard API in `internal/api/handlers/dashboard.go`:
  - `GET /api/v1/dashboard` returns:
    - Cache hit ratio from latest snapshot
    - Total unique queries count
    - Slow queries count (mean_exec_time > 1000ms)
    - Active suggestions count
    - Top 5 queries by total execution time
    - Recent 5 suggestions with severity
- Queries API in `internal/api/handlers/queries.go`:
  - `GET /api/v1/queries` - paginated query list
    - Sort options: calls, mean_time, total_time, rows
    - Order: asc/desc (default: desc)
    - Pagination: limit (default 20, max 100), offset
    - Returns query details with cache hit ratio
  - `GET /api/v1/queries/top` - top N queries by metric
    - Metrics: calls, time, rows
    - Limit parameter (default 10, max 50)
  - `POST /api/v1/queries/:id/explain`
    - Runs EXPLAIN on query (not ANALYZE for safety)
    - Stores result in explain_plans table
    - Returns plan_text and plan_json
- Schema API in `internal/api/handlers/schema.go`:
  - `GET /api/v1/schema/tables` - table stats with sizes
    - Returns seq_scan_ratio, live/dead tuples, last vacuum/analyze
  - `GET /api/v1/schema/indexes` - index stats
    - Returns scans, size, is_unique, is_primary flags
  - `GET /api/v1/schema/bloat` - tables with bloat
    - Returns dead/live tuples and bloat percentage
- Suggestions API in `internal/api/handlers/suggestions.go`:
  - `GET /api/v1/suggestions` - filtered suggestions list
    - Filter by status (active, dismissed) and severity
    - Returns metadata as parsed JSON object
  - `POST /api/v1/suggestions/:id/dismiss`
    - Marks suggestion as dismissed with timestamp
    - Returns 404 if not found, 409 if already dismissed
- Snapshots API in `internal/api/handlers/snapshots.go`:
  - `GET /api/v1/snapshots` - list recent snapshots
    - Limit parameter (default 20, max 100)
    - Returns capture time, PG version, cache hit ratio
  - `POST /api/v1/snapshots` - trigger manual snapshot
    - Runs collection and analysis cycle
    - Returns snapshot ID, status, duration
    - Returns 409 if collection already in progress
- Comprehensive tests in `internal/api/server_test.go`:
  - Mock storage implementing all handler interfaces
  - Mock PostgreSQL client for health checks
  - 13 test functions covering:
    - Auth middleware with valid/invalid credentials
    - Health endpoint skips auth
    - Dashboard endpoint with data aggregation
    - Queries endpoint with pagination
    - Suggestions list and dismiss endpoints
    - Schema endpoints (tables, indexes, bloat)
    - Error response format validation
    - Pagination behavior
    - Snapshots list endpoint

## What Was Completed in Task 10

- Template engine in `internal/web/templates.go`:
  - `TemplateRenderer` struct implementing Echo's `Renderer` interface
  - Embedded filesystem using `embed.FS` for templates and static files
  - Template helper functions:
    - `formatTime`, `formatTimeShort` for timestamp formatting
    - `formatDuration` for milliseconds to human-readable duration
    - `formatBytes` for bytes to human-readable size (B, KB, MB, GB)
    - `formatNumber` with thousand separators
    - `formatPercent` for percentage formatting
    - `truncate` for string truncation
    - `severityClass`, `severityIcon` for severity styling
    - `cacheRatioClass` for cache hit ratio coloring
    - Math functions: `add`, `sub`, `mul`, `div`, `seq`
    - `safeHTML` for rendering trusted HTML
  - `StaticFS()` function exposing embedded static files
- CSS styles in `internal/web/static/style.css`:
  - Clean, minimal design with CSS custom properties
  - Responsive layout with mobile breakpoints
  - Component styles: cards, tables, buttons, forms
  - Severity color indicators (critical/warning/info)
  - Cache ratio color indicators (excellent/good/warning/critical)
  - Modal and pagination styling
  - Loading spinner animation
- Dashboard page in `internal/web/templates/dashboard.html`:
  - Overview stats cards (cache hit ratio, queries, slow queries, suggestions)
  - Top 5 queries by total execution time table
  - Recent 5 suggestions list with severity badges
  - Links to detailed views
- Queries page in `internal/web/templates/queries.html`:
  - Sortable table (total_time, mean_time, calls, rows)
  - Pagination controls with page navigation
  - Cache hit ratio per query with color indicators
  - Details and EXPLAIN buttons for each query
  - EXPLAIN modal with async fetch via JavaScript
- Schema page in `internal/web/templates/schema.html`:
  - Three tabs: Tables, Indexes, Bloat
  - Tables tab: size, rows, dead tuples, scan counts, vacuum/analyze times
  - Indexes tab: scans, size, type (PK/UQ/Idx), unused detection
  - Bloat tab: dead/live tuples ratio, VACUUM recommendations
- Suggestions page in `internal/web/templates/suggestions.html`:
  - Filter by severity (critical, warning, info)
  - Filter by status (active, dismissed)
  - Summary cards with counts per severity
  - Suggestion cards with dismiss functionality
  - Async dismiss via JavaScript with UI feedback
- Query detail page in `internal/web/templates/query_detail.html`:
  - Full query text display
  - Execution statistics (calls, mean/min/max/total time, rows)
  - Cache statistics (hit ratio, blocks hit/read, plans)
  - EXPLAIN plan display with refresh button
- Page handlers in `internal/api/handlers/pages.go`:
  - `PageHandler` struct with storage interface
  - `Dashboard` handler with stats aggregation
  - `Queries` handler with sorting and pagination
  - `QueryDetail` handler with EXPLAIN plan loading
  - `Schema` handler with tab switching (tables/indexes/bloat)
  - `Suggestions` handler with severity filtering
  - Helper functions for data transformation
- Server integration in `internal/api/server.go`:
  - Template renderer initialization
  - Static file serving from embedded filesystem at `/static/*`
  - Web UI routes at root level (`/`, `/queries`, `/queries/:id`, `/schema`, `/suggestions`)
- Comprehensive tests:
  - Template helper function tests in `internal/web/templates_test.go`
  - Page handler tests in `internal/api/handlers/pages_test.go`:
    - Dashboard page with data and without snapshot
    - Queries page with sorting and pagination
    - Query detail page (found and not found cases)
    - Schema page for all three tabs
    - Suggestions page with and without filters
    - Helper function unit tests
    - Sort function tests

## What Was Completed in Task 12

- Updated `cmd/pganalyzer/main.go` with complete component wiring:
  - Added imports for all internal packages (14 packages)
  - Implemented `run()` function with proper initialization order
- Component initialization sequence:
  1. Load configuration using `config.Load()`
  2. Initialize SQLite storage with `sqlite.NewStorage()`
  3. Create and connect PostgreSQL client
  4. Get or create monitored instance record
  5. Create coordinator and register 5 collectors:
     - `query.StatsCollector` - Query statistics
     - `resource.TableStatsCollector` - Table statistics
     - `resource.IndexStatsCollector` - Index statistics
     - `resource.DatabaseStatsCollector` - Cache hit ratio
     - `schema.BloatCollector` - Table bloat
  6. Create analyzer with thresholds from config
  7. Create suggester and register 6 rules:
     - `SlowQueryRule` - Slow query detection
     - `UnusedIndexRule` - Unused index detection
     - `MissingIndexRule` - Missing index suggestions
     - `BloatRule` - Table bloat detection
     - `VacuumRule` - Stale vacuum detection
     - `CacheRule` - Low cache hit ratio detection
  8. Create and start scheduler for background jobs
  9. Create and start HTTP API server
- Graceful shutdown handling:
  - Listens for SIGINT/SIGTERM signals
  - Server shutdown with 30-second timeout
  - Proper cleanup via deferred calls
- Structured logging with slog at each initialization step
- Error handling with wrapped errors for debugging
- Created task documentation in `tasks/12-main-wiring.md`

## What Was Completed in Task 11

- Dockerfile with multi-stage build:
  - `golang:1.22-alpine` for builder stage
  - `alpine:3.19` for runtime stage
  - CGO_ENABLED=0 for pure Go build (modernc.org/sqlite)
  - Non-root user (`pganalyzer`) for security
  - Volume for persistent data directory
  - Health check configuration
  - Exposes port 8080
- Docker Compose configuration in `docker-compose.yaml`:
  - Three profiles: `full`, `standalone`, `postgres-only`
  - `pganalyzer` service with health check
  - `postgres` service with pg_stat_statements enabled
  - Volume mounts for data and config
  - Environment variable passthrough
- Updated `Taskfile.yaml` with docker tasks:
  - `docker:build` - Build Docker image with version info
  - `docker:run` - Run standalone container
  - `docker:up` - Start full stack with docker-compose
  - `docker:up:standalone` - Start pganalyzer only
  - `docker:up:postgres` - Start test PostgreSQL only
  - `docker:down`, `docker:logs`, `docker:clean`
  - `test:integration`, `test:integration:docker` for integration tests
- Integration tests in `tests/integration/`:
  - `setup_test.go` - Test environment setup with real PostgreSQL
  - `collection_test.go` - Full collection cycle tests
  - `analysis_test.go` - Analysis and suggestion generation tests
  - `api_test.go` - API endpoints end-to-end tests
  - Tests use build tag `//go:build integration`
- README.md with comprehensive documentation:
  - Project description and features list
  - Quick start guide with prerequisites
  - Configuration reference
  - Docker deployment instructions
  - API endpoint documentation
  - Web UI pages overview
  - Development commands
  - Architecture overview
  - Analysis rules table
- Enhanced `configs/config.example.yaml`:
  - Detailed comments for all options
  - Environment variable placeholders
  - Example configurations for different scenarios
  - Section headers and documentation
- PostgreSQL setup documentation in `docs/postgresql-setup.md`:
  - Enable pg_stat_statements extension
  - Create monitoring user with minimal privileges
  - Grant necessary permissions
  - Network configuration (pg_hba.conf)
  - Verification steps
  - Docker and cloud provider setup
  - Troubleshooting guide
- Structured logging implementation:
  - `LoggingConfig` in config with level, format, requests options
  - `internal/logging/logging.go` - Setup helper with JSON/text format
  - `internal/api/middleware/request_logger.go` - HTTP request logging
  - Configurable log levels (debug, info, warn, error)
  - JSON format option for production
- Prometheus metrics endpoint (optional):
  - `MetricsConfig` in config with enabled flag and path
  - `internal/metrics/metrics.go` - Prometheus metrics:
    - Collection duration histogram
    - Snapshot count counter
    - Analysis duration and issue counts
    - Database metrics (cache ratio, query count)
    - HTTP request metrics
    - Build info metric
  - `GET /metrics` endpoint (no auth required)
- Security review completed:
  - No secrets logged (verified password fields not in logs)
  - SQL injection prevention (parameterized queries only)
  - Write query protection in Explain function
  - Input validation in API handlers
  - No database modifications (read-only operations)
  - Auth middleware skips health and metrics endpoints
- Helper script `scripts/init-postgres.sql`:
  - Creates pg_stat_statements extension
  - Sample tables for testing
  - Generates initial query activity

## What Was Completed in Task 13

- Tailwind CSS build system setup:
  - `internal/web/tailwind/tailwind.config.js` - Custom theme configuration
    - Primary color (#0d6efd) with hover state
    - Severity colors (critical, warning, info, success) with backgrounds
    - Cache ratio colors (excellent, good, warning, critical)
    - Custom shadows and fonts
  - `internal/web/tailwind/input.css` - Tailwind directives and component classes
    - Base styles for html, body, headings
    - Reusable component classes: buttons, cards, badges, tables
    - Navigation links (active/inactive states)
    - Tabs, forms, modals, pagination
    - Empty state and loading spinner styles
  - `scripts/build-css.sh` - Build script with auto-download
    - Downloads Tailwind standalone CLI (~20MB, no Node.js required)
    - Platform detection (macOS arm64/x64, Linux arm64/x64)
    - Generates minified CSS output
- Taskfile.yaml updates:
  - `task css` - Build CSS with Tailwind (downloads CLI if needed)
  - `task css:watch` - Watch mode for development
  - `task css:install` - Explicit CLI download
  - `task build` now depends on `css` task
- Template helper functions in `internal/web/templates.go`:
  - `severityBadgeClass()` - Returns Tailwind badge classes (badge-critical, etc.)
  - `suggestionCardClass()` - Returns Tailwind card classes with severity borders
  - `dict()` - Creates map for passing multiple values to sub-templates
  - `eq()` - Equality comparison for template conditionals
- All 5 templates migrated to Tailwind CSS:
  - `dashboard.html` - Stats grid, top queries table, recent suggestions
  - `suggestions.html` - Filters, severity badges, suggestion cards with dismiss
  - `queries.html` - Table with sorting/pagination, EXPLAIN modal with backdrop blur
  - `schema.html` - Tab navigation (Tables/Indexes/Bloat), data tables
  - `query_detail.html` - Two-column stats layout, code blocks for query/EXPLAIN
- Aesthetic improvements:
  - Modern card styling with `rounded-xl`, `shadow-sm`, `hover:shadow-md`
  - Pill-shaped severity badges with `rounded-full`
  - Modal with backdrop blur effect (`bg-gray-900/50 backdrop-blur-sm`)
  - Improved empty states with SVG icons
  - Tabular numbers (`tabular-nums`) for aligned data columns
  - Sticky header navigation
  - Better visual hierarchy with improved typography
  - Responsive grid layouts (mobile-first)
  - Smooth transitions on interactive elements
- All existing tests pass (template rendering, page handlers)

## What Was Completed in Task 14

- New data models in `internal/models/models.go`:
  - `ConnectionActivity` - pg_stat_activity aggregates (active, idle, idle-in-tx, waiting counts)
  - `LongRunningQuery` - queries exceeding duration threshold with PID, user, database, wait events
  - `IdleInTransaction` - connections idle in transaction with duration
  - `LockStats` - aggregated lock statistics by lock type
  - `BlockedQuery` - blocked/blocking query pairs with wait duration and lock details
  - `ExtendedDatabaseStats` - transaction rates, temp files, deadlocks, conflicts
- New migration `internal/storage/sqlite/migrations/010_create_operational_stats.sql`:
  - `connection_activity` table for connection snapshots
  - `long_running_queries` table for queries exceeding threshold
  - `idle_in_transaction` table for idle-in-tx connections
  - `lock_stats` table for lock aggregates
  - `blocked_queries` table for blocking relationships
  - `extended_database_stats` table for operational metrics
  - All tables with foreign key cascade deletes and appropriate indexes
- Storage methods in `internal/storage/sqlite/storage.go`:
  - Save/Get methods for all 6 new data types
  - Bulk insert for long-running queries, idle-in-tx, blocked queries
- PostgreSQL client methods in `internal/postgres/pgx_client.go`:
  - `GetConnectionActivity()` - aggregates from pg_stat_activity
  - `GetLongRunningQueries(threshold)` - active queries exceeding duration
  - `GetIdleInTransaction(threshold)` - idle-in-tx connections exceeding duration
  - `GetLockStats()` - lock counts by type from pg_locks
  - `GetBlockedQueries()` - blocked/blocking relationships with lock details
  - `GetExtendedDatabaseStats()` - xact_commit/rollback, temp files, deadlocks
- Activity Collector in `internal/collector/activity/activity.go`:
  - 30-second default interval
  - Collects connection activity summary
  - Collects long-running queries (default threshold: 60s)
  - Collects idle-in-transaction connections (default threshold: 60s)
  - Configurable thresholds via `ActivityCollectorConfig`
- Locks Collector in `internal/collector/locks/locks.go`:
  - 30-second default interval
  - Collects lock statistics aggregates
  - Collects blocked/blocking query relationships
- Extended Database Collector in `internal/collector/resource/database.go`:
  - Added collection of extended stats (temp files, deadlocks, conflicts)
  - Saves to `extended_database_stats` table
- Analyzer extensions in `internal/analyzer/`:
  - Extended `Storage` interface with operational stats getters
  - Extended `AnalysisResult` with `ActivityStats`, `LockStats`, `TransactionStats`
  - `ActivityAnalysis` struct with connection utilization, long-running, idle-in-tx
  - `LockAnalysis` struct with blocked queries and waiting lock count
  - `TransactionAnalysis` struct with temp files and deadlock metrics
  - `MainAnalyzer.Analyze()` now fetches and includes operational data
- New suggestion rules in `internal/suggester/rules/`:
  - `long_running_query.go` - queries > 60s (warning), > 300s (critical)
  - `idle_in_transaction.go` - idle-in-tx > 60s (warning), > 300s (critical)
  - `lock_contention.go` - blocked queries > 10s (warning), > 60s (critical)
  - `high_temp_usage.go` - temp bytes > 1GB (warning), > 10GB (critical)
  - `high_deadlocks.go` - any deadlocks detected (warning), > 5 (critical)
- Config extensions in `internal/suggester/rule.go`:
  - `LongRunningQuerySeconds`, `LongRunningCriticalSeconds`
  - `IdleInTxSeconds`, `IdleInTxCriticalSeconds`
  - `BlockedQuerySeconds`, `BlockedQueryCriticalSeconds`
  - `TempBytesWarning`, `TempBytesCritical`
  - `DeadlocksWarning`
  - `ConnectionUtilizationWarning`
- Dashboard updates in `internal/web/templates/dashboard.html`:
  - Active Connections stat card (current / max_connections)
  - Long-Running Queries stat card
  - Blocked Queries stat card
  - Connection Activity section (active, idle, idle-in-tx, waiting grid)
  - Lock Activity section (blocked queries list with wait durations)
- Page handler updates in `internal/api/handlers/pages.go`:
  - `PageStorage` interface extended with operational stats getters
  - `DashboardPageData` extended with activity/lock stats
  - Dashboard handler fetches and renders operational data
- Main wiring in `cmd/pganalyzer/main.go`:
  - Activity collector registered with coordinator
  - Locks collector registered with coordinator
  - 5 new suggestion rules registered with suggester
- Test fixes across all packages:
  - Updated mock implementations in `scheduler_test.go`, `collector_test.go`, `analyzer_test.go`, `pages_test.go`
  - Updated migration count assertion in `storage_test.go`
  - All tests passing
