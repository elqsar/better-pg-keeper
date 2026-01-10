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
- [ ] 05 - Collectors
- [ ] 06 - Analyzer
- [ ] 07 - Suggester
- [ ] 08 - Scheduler
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
