# PGAnalyzer

A PostgreSQL performance analyzer that collects query statistics, detects performance issues, and provides actionable recommendations.

## Features

- **Query Statistics Collection**: Collects data from `pg_stat_statements` to track query performance
- **Performance Analysis**: Identifies slow queries, poor cache performance, table bloat, and unused indexes
- **Automated Recommendations**: Generates actionable suggestions for performance improvements
- **Web Dashboard**: Server-rendered HTML UI for visualizing metrics and suggestions
- **REST API**: Full API access to all collected data and analysis results
- **Scheduled Collection**: Automated background collection and analysis at configurable intervals
- **Data Retention**: Automatic cleanup of old snapshots based on retention policies

## Prerequisites

- Go 1.22 or later
- PostgreSQL 12+ with `pg_stat_statements` extension enabled
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

See [docs/postgresql-setup.md](docs/postgresql-setup.md) for detailed setup instructions.

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
| scheduler.snapshot_interval | - | 5m | Collection interval |
| scheduler.analysis_interval | - | 15m | Analysis interval |
| server.port | - | 8080 | HTTP server port |
| thresholds.slow_query_ms | - | 1000 | Slow query threshold (ms) |
| thresholds.cache_hit_ratio | - | 95.0 | Cache hit ratio warning threshold (%) |

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
- `POST /api/v1/queries/:id/explain` - Get EXPLAIN plan for a query

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

# Build
task build

# Run all checks
task all
```

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
├── configs/              # Configuration files
├── docs/                 # Documentation
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
| slow_query | Query mean execution time exceeds threshold | Warning/Critical |
| unused_index | Index with zero scans (excludes PK/unique) | Warning |
| missing_index | High sequential scan ratio on large tables | Info/Warning |
| table_bloat | High dead tuple percentage | Warning/Critical |
| stale_vacuum | Table not vacuumed recently | Warning |
| low_cache_hit | Database cache hit ratio below threshold | Warning/Critical |

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
