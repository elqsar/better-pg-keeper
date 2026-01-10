# Task 04: PostgreSQL Client

## Objective
Implement PostgreSQL client for collecting stats and running queries against the monitored database.

## Subtasks

### 4.1 Define Models
Location: `pkg/models/`

- [ ] `query_stat.go` - QueryStat struct matching pg_stat_statements
- [ ] `table_stat.go` - TableStat struct matching pg_stat_user_tables
- [ ] `index_stat.go` - IndexStat struct matching pg_stat_user_indexes
- [ ] `database_stat.go` - DatabaseStats (cache hit ratio, etc.)
- [ ] `bloat.go` - BloatInfo struct
- [ ] `explain.go` - ExplainPlan struct

### 4.2 Define Client Interface
Location: `internal/postgres/client.go`

- [ ] Define `Client` interface (as per tech design section 4.1)
- [ ] Define `ClientConfig` struct for connection options

### 4.3 Implement Connection Management
Location: `internal/postgres/pgx_client.go`

- [ ] Implement `NewClient(cfg ClientConfig) (*PgxClient, error)`
- [ ] Use `pgxpool` for connection pooling
- [ ] Implement `Connect(ctx) error`
- [ ] Implement `Close() error`
- [ ] Implement `Ping(ctx) error`

### 4.4 Implement Stats Collection
- [ ] `GetStatStatements(ctx) ([]QueryStat, error)`
  ```sql
  SELECT queryid, query, calls, total_exec_time, mean_exec_time,
         min_exec_time, max_exec_time, rows, shared_blks_hit,
         shared_blks_read, plans, total_plan_time
  FROM pg_stat_statements
  WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
  ```

- [ ] `GetStatTables(ctx) ([]TableStat, error)`
  ```sql
  SELECT schemaname, relname, seq_scan, seq_tup_read, idx_scan,
         idx_tup_fetch, n_live_tup, n_dead_tup, last_vacuum,
         last_autovacuum, last_analyze,
         pg_table_size(relid), pg_indexes_size(relid)
  FROM pg_stat_user_tables
  ```

- [ ] `GetStatIndexes(ctx) ([]IndexStat, error)`
  ```sql
  SELECT s.schemaname, s.relname, s.indexrelname, s.idx_scan,
         s.idx_tup_read, s.idx_tup_fetch, pg_relation_size(s.indexrelid),
         i.indisunique, i.indisprimary
  FROM pg_stat_user_indexes s
  JOIN pg_index i ON s.indexrelid = i.indexrelid
  ```

- [ ] `GetDatabaseStats(ctx) (*DatabaseStats, error)`
  ```sql
  SELECT blks_hit, blks_read,
         ROUND(100.0 * blks_hit / NULLIF(blks_hit + blks_read, 0), 2)
  FROM pg_stat_database WHERE datname = current_database()
  ```

### 4.5 Implement Schema Analysis
- [ ] `GetTableBloat(ctx) ([]BloatInfo, error)`
  ```sql
  SELECT schemaname, relname, n_dead_tup, n_live_tup,
         ROUND(n_dead_tup::numeric / NULLIF(n_live_tup, 0) * 100, 2)
  FROM pg_stat_user_tables WHERE n_dead_tup > 1000
  ```

- [ ] `GetIndexDetails(ctx) ([]IndexDetail, error)` - extended index info

### 4.6 Implement Query Analysis
- [ ] `Explain(ctx, query, analyze bool) (*ExplainPlan, error)`
  - Use `EXPLAIN (BUFFERS, VERBOSE, SETTINGS, FORMAT JSON)`
  - If analyze=true, add `ANALYZE` with timeout
  - Never auto-analyze write queries (INSERT/UPDATE/DELETE)
  - Return plan text and parsed JSON

### 4.7 Implement Metadata Queries
- [ ] `GetVersion(ctx) (string, error)` - `SELECT version()`
- [ ] `GetStatsResetTime(ctx) (time.Time, error)`
  ```sql
  SELECT stats_reset FROM pg_stat_statements_info
  ```

### 4.8 Write Tests
- [ ] Test connection with valid credentials
- [ ] Test all stat queries return expected structure
- [ ] Test EXPLAIN parsing
- [ ] Mock tests for unit testing without DB

## Acceptance Criteria
- [ ] Client connects to PostgreSQL successfully
- [ ] All stat queries work on PG14+
- [ ] EXPLAIN returns parseable plans
- [ ] Stats reset time is correctly detected
- [ ] Tests pass
