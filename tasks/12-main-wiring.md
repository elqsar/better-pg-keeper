# Task 12: Main Application Wiring

## Overview

Connect all implemented components in `cmd/pganalyzer/main.go` to create a fully functional PostgreSQL analyzer application.

## Dependencies

- Task 01: Project Setup
- Task 02: Configuration
- Task 03: Storage (SQLite)
- Task 04: PostgreSQL Client
- Task 05: Collectors
- Task 06: Analyzer
- Task 07: Suggester
- Task 08: Scheduler
- Task 09: API Server
- Task 10: Web UI

## Deliverables

### 1. Updated `cmd/pganalyzer/main.go`

The `run()` function now wires all components in the correct order:

1. **Load Configuration** - Using `config.Load()`
2. **Initialize Storage** - Using `sqlite.NewStorage()`
3. **Initialize PostgreSQL Client** - Using `postgres.NewClient()` and `Connect()`
4. **Get/Create Instance** - Using `storage.GetOrCreateInstance()`
5. **Create Coordinator** - Using `collector.NewCoordinator()`
6. **Register Collectors**:
   - `query.StatsCollector` - Query statistics from pg_stat_statements
   - `resource.TableStatsCollector` - Table statistics
   - `resource.IndexStatsCollector` - Index statistics
   - `resource.DatabaseStatsCollector` - Database-level cache hit ratio
   - `schema.BloatCollector` - Table bloat information
7. **Create Analyzer** - Using `analyzer.NewMainAnalyzer()`
8. **Create Suggester** - Using `suggester.NewSuggester()`
9. **Register Suggester Rules**:
   - `SlowQueryRule` - Detects slow queries
   - `UnusedIndexRule` - Detects unused indexes
   - `MissingIndexRule` - Suggests missing indexes
   - `BloatRule` - Detects table bloat
   - `VacuumRule` - Detects stale vacuum
   - `CacheRule` - Detects low cache hit ratio
10. **Create Scheduler** - Using `scheduler.NewScheduler()`
11. **Start Scheduler** - Background goroutines for collection/analysis
12. **Create API Server** - Using `api.NewServer()`
13. **Start HTTP Server** - In background goroutine
14. **Wait for Shutdown** - Listen for context cancellation or server error
15. **Graceful Shutdown** - Clean shutdown of all services

### 2. Component Lifecycle

```
main()
  │
  ├─ Parse flags
  ├─ Setup logging
  ├─ Setup signal handling (SIGINT, SIGTERM)
  │
  └─ run(ctx, configPath)
       │
       ├─ config.Load()
       ├─ sqlite.NewStorage() ──────────── defer Close()
       ├─ postgres.NewClient()
       │   └─ Connect() ──────────────────── defer Close()
       ├─ storage.GetOrCreateInstance()
       ├─ collector.NewCoordinator()
       │   └─ RegisterCollectors(5 collectors)
       ├─ analyzer.NewMainAnalyzer()
       ├─ suggester.NewSuggester()
       │   └─ RegisterRules(6 rules)
       ├─ scheduler.NewScheduler()
       │   └─ Start() ────────────────────── defer Stop()
       ├─ api.NewServer()
       │   └─ Start() (background goroutine)
       │
       ├─ Wait for shutdown:
       │   - ctx.Done() (signal received)
       │   - serverErr (server failed)
       │
       └─ Graceful shutdown:
           └─ server.Shutdown(30s timeout)
```

### 3. Error Handling

- Each initialization step returns an error if it fails
- Errors are wrapped with context using `fmt.Errorf("...: %w", err)`
- Deferred cleanup ensures resources are released even on error
- Server errors are caught via channel and returned

### 4. Logging

Structured logging with `slog` at each step:
- `configuration loaded` - Shows postgres host and database
- `storage initialized` - Shows database path
- `connected to PostgreSQL` - Shows host and database
- `instance ready` - Shows instance ID and name
- `collectors registered` - Shows count (5)
- `analyzer initialized`
- `suggester initialized` - Shows rule count (6)
- `scheduler started` - Shows intervals
- `starting HTTP server` - Shows host and port
- `shutdown signal received` - On graceful shutdown
- `initiating graceful shutdown`
- `pganalyzer stopped` - Final message

## Testing

### Build and Run

```bash
# Build the application
task build

# Run with default config
./bin/pganalyzer

# Run with custom config
./bin/pganalyzer -config /path/to/config.yaml

# Show version
./bin/pganalyzer -version
```

### Expected Startup Logs

```
time=2026-01-11T... level=INFO msg="starting pganalyzer" version=dev config=configs/config.yaml
time=2026-01-11T... level=INFO msg="configuration loaded" postgres_host=localhost postgres_db=mydb
time=2026-01-11T... level=INFO msg="storage initialized" path=./data/pganalyzer.db
time=2026-01-11T... level=INFO msg="connected to PostgreSQL" host=localhost database=mydb
time=2026-01-11T... level=INFO msg="instance ready" id=1 name=localhost:5432/mydb
time=2026-01-11T... level=INFO msg="collectors registered" count=5
time=2026-01-11T... level=INFO msg="analyzer initialized"
time=2026-01-11T... level=INFO msg="suggester initialized" rules=6
time=2026-01-11T... level=INFO msg="scheduler started" snapshot_interval=5m0s analysis_interval=15m0s
time=2026-01-11T... level=INFO msg="starting HTTP server" host=0.0.0.0 port=8080
```

### Verify Endpoints

```bash
# Health check
curl http://localhost:8080/health

# Dashboard API
curl http://localhost:8080/api/v1/dashboard

# Web UI (browser)
open http://localhost:8080/
```

### Graceful Shutdown

Press Ctrl+C or send SIGTERM:

```
time=2026-01-11T... level=INFO msg="received shutdown signal" signal=interrupt
time=2026-01-11T... level=INFO msg="shutdown signal received"
time=2026-01-11T... level=INFO msg="initiating graceful shutdown"
time=2026-01-11T... level=INFO msg="pganalyzer stopped"
```

## Configuration Reference

See `configs/config.yaml` for the complete configuration file. Key sections:

```yaml
postgres:
  host: localhost
  port: 5432
  database: mydb
  user: myuser
  password: mypassword
  sslmode: prefer

storage:
  path: ./data/pganalyzer.db
  retention:
    snapshots: 168h  # 7 days
    query_stats: 720h  # 30 days

scheduler:
  snapshot_interval: 5m
  analysis_interval: 15m

server:
  host: 0.0.0.0
  port: 8080
  auth:
    enabled: true
    username: admin
    password: admin

thresholds:
  slow_query_ms: 1000
  cache_hit_ratio_warning: 0.95
  bloat_percent_warning: 20
  unused_index_days: 30
  seq_scan_ratio_warning: 0.5
  min_table_size_for_index: 10000
```

## Imports Added

```go
import (
    "net/http"
    "time"

    "github.com/user/pganalyzer/internal/analyzer"
    "github.com/user/pganalyzer/internal/api"
    "github.com/user/pganalyzer/internal/collector"
    "github.com/user/pganalyzer/internal/collector/query"
    "github.com/user/pganalyzer/internal/collector/resource"
    "github.com/user/pganalyzer/internal/collector/schema"
    "github.com/user/pganalyzer/internal/config"
    "github.com/user/pganalyzer/internal/models"
    "github.com/user/pganalyzer/internal/postgres"
    "github.com/user/pganalyzer/internal/scheduler"
    "github.com/user/pganalyzer/internal/storage/sqlite"
    "github.com/user/pganalyzer/internal/suggester"
    "github.com/user/pganalyzer/internal/suggester/rules"
)
```
