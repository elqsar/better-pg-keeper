# PGAnalyzer

A PostgreSQL performance analyzer that collects query statistics, detects performance issues, and provides actionable recommendations.

## Features

- **Query Statistics Collection**: Collects data from `pg_stat_statements` to track query performance
- **Performance Analysis**: Identifies slow queries, poor cache performance, table bloat, and unused indexes
- **Outage Warnings**: Transaction ID wraparound, replication slots retaining WAL, sequences running out,
  forgotten prepared transactions, disk growth and connection saturation
- **Automated Recommendations**: Generates actionable suggestions for performance improvements
- **Index Advisor**: Plans the busiest queries hourly and proposes ready-to-run `CREATE INDEX`
  statements, checked against the planner with hypopg when it is installed
- **Web Dashboard**: Server-rendered HTML UI for visualizing metrics and suggestions
- **REST API**: Full API access to all collected data and analysis results
- **Scheduled Collection**: Automated background collection and analysis at configurable intervals
- **Data Retention**: Automatic cleanup of old snapshots based on retention policies

## Prerequisites

- Go 1.25 or later (see `go.mod`)
- PostgreSQL 14+ with `pg_stat_statements` extension enabled

  PGAnalyzer refuses to start against anything older. If the cluster was upgraded
  from an older major version, run `ALTER EXTENSION pg_stat_statements UPDATE;` so
  the `pg_stat_statements_info` view exists; without it, statistics-reset
  detection is skipped.
- [Task](https://taskfile.dev/) (optional, for build automation)

## Quick Start

### 1. Enable pg_stat_statements in PostgreSQL

Add to your `postgresql.conf`:

```ini
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
pg_stat_statements.max = 10000
```

Restart PostgreSQL and create the extension:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

The monitoring role needs `pg_read_all_stats` (or superuser) so it can see query
text and statistics for every role, not just its own:

```sql
CREATE ROLE pganalyzer LOGIN PASSWORD 'change-me';
GRANT pg_read_all_stats TO pganalyzer;
GRANT CONNECT ON DATABASE your_database TO pganalyzer;
```

To check sequences for exhaustion the role must also be able to read them. Grant
`pg_read_all_data` or, per schema:

```sql
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO pganalyzer;
```

Sequences it cannot read are skipped; the rest of the outage checks work with
`pg_read_all_stats` alone.

`pg_stat_statements.track = all` also counts statements executed inside functions and
procedures. PGAnalyzer aggregates the resulting rows by query ID, so a query is
reported once regardless of how many roles run it or how it was invoked.

### 2. Configure PGAnalyzer

Copy the example configuration:

```bash
cp configs/config.example.yaml configs/config.yaml
```

Edit `configs/config.yaml` with your PostgreSQL connection details:

```yaml
postgres:
  host: localhost
  port: 5432
  database: your_database
  user: your_user
  password: your_password
  sslmode: prefer
```

### 3. Run PGAnalyzer

#### Using Task (recommended)

```bash
# Build and run
task run

# Or just build
task build
./bin/pganalyzer
```

#### Using Go directly

```bash
go build -o pganalyzer ./cmd/pganalyzer
./pganalyzer
```

#### Using Docker

```bash
# Build image
task docker:build

# Run container
task docker:run
```

### 4. Access the Dashboard

Open http://localhost:8080 in your browser.

Default credentials (if auth is enabled):
- Username: `admin`
- Password: `admin`

## Configuration

PGAnalyzer is configured via YAML file. See [configs/config.example.yaml](configs/config.example.yaml) for all options.

### Environment Variable Expansion

Configuration values support environment variable expansion:

```yaml
postgres:
  password: ${POSTGRES_PASSWORD:-default_password}
```

### Key Configuration Options

| Section | Option | Default | Description |
|---------|--------|---------|-------------|
| postgres.host | - | localhost | PostgreSQL host |
| postgres.port | - | 5432 | PostgreSQL port |
| storage.path | - | ./data/pganalyzer.db | SQLite database path |
| scheduler.snapshot_interval | - | 5m | Window for grouping collector runs into a snapshot |
| scheduler.analysis_interval | - | 15m | Analysis interval |
| storage.retention.snapshots | - | 168h | Snapshot retention (7 days) |
| storage.retention.query_stats | - | 720h | Independent query history retention (30 days) |
| storage.retention.size_history | - | 2160h | Hourly database size samples for the disk growth forecast (90 days) |
| server.port | - | 8080 | HTTP server port |
| thresholds.slow_query_ms | - | 1000 | Slow query threshold (ms) |
| thresholds.slow_query_window | - | 24h | Period slow queries are judged over |
| thresholds.unused_index_days | - | 30 | Minimum days of scan statistics before flagging an unused index |
| thresholds.cache_hit_ratio | - | 95.0 | Cache hit ratio warning threshold (%) |
| thresholds.disk_capacity_gb | - | 0 | Size of the data volume; enables the "disk full in N days" forecast |
| thresholds.server_memory_gb | - | 0 | RAM of the PostgreSQL server; enables memory-setting checks |

## Index Advisor

Every hour PGAnalyzer takes the busiest queries of the last 24 hours (by
execution time, `index_advisor.max_queries`, default 20) and asks PostgreSQL for
their generic plans. Queries are never executed: they are prepared and explained
in a read-only transaction with a 2s statement timeout and a 100ms lock timeout.

A sequential scan becomes an `index_recommendation` when its filter can use a
btree index (equality columns first, then one range column), the table has at
least `min_table_size_for_index` rows, the filter matches under 20% of them, and
no existing index already starts with those columns. Proposals from several
queries on one table are merged when one is a prefix of another.

If the [hypopg](https://github.com/HypoPG/hypopg) extension is installed in the
monitored database (it is available on RDS, Cloud SQL, Azure and Supabase), each
proposal is first tried as a hypothetical index, and dropped if the planner would
not use it. PGAnalyzer never installs extensions.

Planning needs `SELECT` on the tables a query reads; queries the role cannot
plan are recorded and skipped. The query page shows the latest plan with a
plain-language summary.

## Notifications

PGAnalyzer can post to Slack or any webhook, so nobody has to watch the dashboard:

- **Alerts** when an issue reaches `min_severity` (default: critical) or gets worse,
  a reminder every `renotify_after` while it stays active, and a resolved message
  once it has stayed gone for `resolve_grace`. Everything found in one analysis
  cycle arrives as a single message.
- **Collection failures**: an alert when no collection has succeeded for
  `collection_stale_after`, and a message when it recovers.
- **Digest** (weekly by default): active issues by severity, issues that
  appeared or were resolved in the period, and the queries that used the most
  database time. Resolved query issues show what the fix did (see below).

```yaml
notifications:
  enabled: true
  dashboard_url: https://pganalyzer.example.com
  channels:
    - type: slack
      url: ${SLACK_WEBHOOK_URL}
```

Check the setup with `pganalyzer -config configs/config.yaml -notify-test` (or
`task notify:test -- -config configs/config.yaml`). See
[configs/config.example.yaml](configs/config.example.yaml) for every option.

Alerts come from PGAnalyzer itself, so they stop if PGAnalyzer stops. Point an
uptime monitor at `/health` to cover that.

## Fix Verification

When a `slow_query` or `index_recommendation` suggestion resolves, PGAnalyzer
compares the mean time of its queries before and after, from query history:
"index on orders(status, created_at): 3.2s → 40ms (99% faster)". The result is
shown on the suggestion page and next to the issue in the digest.

- Each side covers up to `thresholds.slow_query_window` (default 24h). A slow
  query resolves only once its trailing-window mean drops, up to a window after
  the fix, so its "before" period ends a window earlier.
- Verdicts: faster (at least 20% lower), slower (at least 20% higher), no
  significant change, no longer running, or waiting for data. A verdict needs at
  least 1h and 10 calls after the resolution.
- History older than `storage.retention.query_stats` can't be compared.

## Docker Deployment

### Using Docker Compose

```bash
# Start pganalyzer with a test PostgreSQL instance
task docker:up

# Or start pganalyzer only (connects to external PostgreSQL)
task docker:up:standalone

# View logs
task docker:logs

# Stop services
task docker:down
```

### Building the Docker Image

```bash
docker build -t pganalyzer:latest .
```

### Running the Container

```bash
docker run -d \
  --name pganalyzer \
  -p 8080:8080 \
  -v ./data:/app/data \
  -v ./configs/config.yaml:/app/configs/config.yaml:ro \
  -e POSTGRES_PASSWORD=your_password \
  pganalyzer:latest
```

## API Endpoints

### Health Check
- `GET /health` - Returns service health status (no auth required)

### Dashboard
- `GET /api/v1/dashboard` - Overview statistics

### Queries
- `GET /api/v1/queries` - List queries with pagination
- `GET /api/v1/queries/top` - Top N queries by metric
- `GET /api/v1/queries/:id/history` - Retained query samples; accepts RFC3339 `from`/`to`, `limit`, and `offset`
- `POST /api/v1/queries/:id/explain` - Get EXPLAIN plan for a query. Without parameter
  values it returns the generic plan of the normalized query; the query is never executed.

### Schema
- `GET /api/v1/schema/tables` - Table statistics
- `GET /api/v1/schema/indexes` - Index statistics
- `GET /api/v1/schema/bloat` - Table bloat information

### Suggestions
- `GET /api/v1/suggestions` - List recommendations
- `POST /api/v1/suggestions/:id/dismiss` - Dismiss a suggestion

### Snapshots
- `GET /api/v1/snapshots` - List recent snapshots
- `POST /api/v1/snapshots` - Trigger manual snapshot

## Web UI Pages

- `/` - Dashboard with overview statistics
- `/queries` - Query list with sorting and filtering
- `/queries/:id` - Query detail with execution plan
- `/schema` - Tables, indexes, and bloat information
- `/suggestions` - Performance recommendations

## Development

### Prerequisites

```bash
# Install development tools
task install:tools
```

### Common Commands

```bash
# Run tests
task test

# Run tests with coverage
task test:coverage

# Run linter
task lint

# Format code
task fmt

# Build (includes CSS)
task build

# Run all checks
task all
```

### CSS Development (Tailwind)

The web UI uses [Tailwind CSS](https://tailwindcss.com/) with a standalone CLI (no Node.js required). The Tailwind CLI is automatically downloaded on first build.

```bash
# Build CSS (downloads Tailwind CLI if needed)
task css

# Watch mode for development (auto-rebuild on changes)
task css:watch
```

**Configuration files:**
- `internal/web/tailwind/tailwind.config.js` - Theme customization
- `internal/web/tailwind/input.css` - Tailwind directives and component classes
- `internal/web/static/style.css` - Generated output (do not edit directly)

### Running Integration Tests

Integration tests require a running PostgreSQL instance with `pg_stat_statements` enabled:

```bash
# Start test PostgreSQL
task docker:up:postgres

# Run integration tests
POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=postgres \
POSTGRES_PASSWORD=postgres POSTGRES_DATABASE=testdb \
go test -v -tags=integration ./tests/integration/...

# Or use the task command
task test:integration:docker
```

## Architecture

```
pganalyzer/
├── cmd/pganalyzer/       # Application entry point
├── internal/
│   ├── analyzer/         # Performance analysis logic
│   ├── api/              # REST API handlers
│   ├── collector/        # Data collection from PostgreSQL
│   ├── config/           # Configuration management
│   ├── models/           # Data models
│   ├── postgres/         # PostgreSQL client
│   ├── scheduler/        # Background job scheduling
│   ├── storage/sqlite/   # SQLite storage layer
│   ├── suggester/        # Recommendation engine
│   └── web/              # Web UI templates and assets
│       ├── templates/    # HTML templates
│       ├── static/       # Static assets (generated CSS)
│       └── tailwind/     # Tailwind CSS configuration
├── configs/              # Configuration files
├── scripts/              # Build scripts (CSS, etc.)
└── tests/integration/    # Integration tests
```

## Collected Metrics

### Query Statistics (from pg_stat_statements)
- Query text and query ID
- Call count, total/mean/min/max execution time
- Rows returned
- Block hits and reads (for cache analysis)
- Plans count

### Table Statistics
- Table size (data + indexes)
- Row counts (live and dead tuples)
- Sequential vs index scan counts
- Last vacuum/analyze timestamps

### Index Statistics
- Index size
- Scan count
- Tuples read/fetched
- Unique/primary key flags

### Database Statistics
- Cache hit ratio

## Analysis Rules

PGAnalyzer detects the following issues:

| Rule | Description | Severity |
|------|-------------|----------|
| slow_query | Mean execution time over the last `slow_query_window` (default 24h) exceeds threshold | Warning/Critical |
| unused_index | No scans for at least `unused_index_days`; skips PK/unique and foreign-key indexes | Info/Warning |
| duplicate_index | Index identical to, or a leading prefix of, another index on the same table | Info/Warning |
| missing_index | High sequential scan ratio on large tables with no concrete index proposal; explains why busy queries' scans got none | Info/Warning |
| index_recommendation | Index proposed from the plans of busy queries that read a large table sequentially, with DDL, size estimate and rollback | Info/Warning |
| table_bloat | High dead tuple percentage | Warning/Critical |
| stale_vacuum | Table not vacuumed recently | Warning |
| low_cache_hit | Database cache hit ratio below threshold | Warning/Critical |
| high_temp_usage | Queries spilling to temporary files | Warning/Critical |
| long_running_query | Queries running longer than expected | Warning/Critical |
| idle_in_transaction | Sessions holding a transaction open while idle | Warning/Critical |
| lock_contention | Queries waiting on locks | Warning/Critical |
| high_deadlocks | Deadlocks detected | Warning/Critical |
| xid_wraparound | Oldest unfrozen transaction ID past 1.5x `autovacuum_freeze_max_age` or 500M (critical at 1B); names what blocks freezing | Warning/Critical |
| multixact_wraparound | Same for multixact IDs | Warning/Critical |
| replication_slot | Inactive slot retaining >1GB of WAL, any slot >10GB or about to be invalidated, slot holding back freezing, standby replay lag >5 min | Warning/Critical |
| sequence_exhaustion | Sequence past 75% (critical at 90%) of its range, including a bigint sequence feeding an `integer` column | Warning/Critical |
| prepared_transaction | Prepared transaction left open for over 1h (critical after 24h) | Warning/Critical |
| disk_growth | Disk forecast to fill within 30 days (critical within 7) given `disk_capacity_gb`; otherwise size doubling within 90 days | Info/Warning/Critical |
| connection_saturation | Peak connections over the last 24h above 80% of `max_connections` (critical at 95%) | Warning/Critical |
| configuration | Server settings review: autovacuum or `track_counts` off (critical), tables with `autovacuum_enabled=false` (critical over 1GB), no idle-in-transaction or statement timeout, slow-query/lock-wait logging off, untuned `shared_buffers`, `work_mem` x `max_connections` over RAM (given `server_memory_gb`), `random_page_cost` for spinning disks, settings waiting for a restart | Info/Warning/Critical |
| stat_statements_capacity | pg_stat_statements at 90% of `pg_stat_statements.max` or evicting statements in the last 24h, so query history is incomplete; `pg_stat_statements.track = none` | Warning |

Scans on read replicas are not visible to PGAnalyzer, which monitors a single
server. Check replicas before acting on `unused_index`.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Code Style

- Follow standard Go conventions
- Run `task lint` before committing
- Add tests for new functionality

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver for Go
- [Echo](https://echo.labstack.com/) - High performance web framework
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - Pure Go SQLite driver
