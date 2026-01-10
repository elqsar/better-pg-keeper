# Task 05: Data Collectors

## Objective
Implement collectors that periodically fetch metrics from PostgreSQL and store them in SQLite.

## Subtasks

### 5.1 Define Collector Interface
Location: `internal/collector/collector.go`

- [ ] Define `Collector` interface:
  ```go
  type Collector interface {
      Name() string
      Collect(ctx context.Context) error
      Interval() time.Duration
  }
  ```
- [ ] Define `CollectorConfig` struct for common settings

### 5.2 Implement Base Collector
Location: `internal/collector/base.go`

- [ ] Create `BaseCollector` with shared logic:
  - PostgreSQL client reference
  - Storage reference
  - Instance ID
  - Snapshot management (get or create current snapshot)

### 5.3 Implement QueryStatsCollector
Location: `internal/collector/query/stats.go`

- [ ] Interval: 1 minute
- [ ] Fetch from `pg_stat_statements`
- [ ] Store in `query_stats` table
- [ ] Detect stats reset by comparing `stats_reset` timestamp
- [ ] Log warning on stats reset detection

### 5.4 Implement TableStatsCollector
Location: `internal/collector/resource/tables.go`

- [ ] Interval: 5 minutes
- [ ] Fetch from `pg_stat_user_tables`
- [ ] Store in `table_stats` table
- [ ] Include table and index sizes

### 5.5 Implement IndexStatsCollector
Location: `internal/collector/resource/indexes.go`

- [ ] Interval: 5 minutes
- [ ] Fetch from `pg_stat_user_indexes`
- [ ] Store in `index_stats` table
- [ ] Include unique/primary flags

### 5.6 Implement DatabaseStatsCollector
Location: `internal/collector/resource/database.go`

- [ ] Interval: 1 minute
- [ ] Fetch cache hit ratio from `pg_stat_database`
- [ ] Store in snapshot metadata or separate table

### 5.7 Implement BloatCollector
Location: `internal/collector/schema/bloat.go`

- [ ] Interval: 1 hour
- [ ] Calculate dead tuple ratio
- [ ] Filter tables with significant bloat (>1000 dead tuples)
- [ ] Store bloat metrics

### 5.8 Implement Snapshot Coordinator
Location: `internal/collector/coordinator.go`

- [ ] Manage snapshot lifecycle across collectors
- [ ] Create single snapshot per collection cycle
- [ ] Pass snapshot ID to all collectors
- [ ] Handle partial failures (some collectors fail)

### 5.9 Write Tests
- [ ] Test each collector in isolation
- [ ] Test snapshot coordination
- [ ] Test stats reset detection
- [ ] Mock PostgreSQL client for unit tests

## Acceptance Criteria
- [ ] All collectors implement the interface
- [ ] Collectors run at configured intervals
- [ ] Data is correctly stored in SQLite
- [ ] Stats reset is detected and handled
- [ ] Tests pass
