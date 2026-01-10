# Task 06: Analyzer

## Objective
Implement analysis logic that processes snapshots and identifies performance issues.

## Subtasks

### 6.1 Define Analyzer Interface
Location: `internal/analyzer/analyzer.go`

- [ ] Define `Analyzer` interface:
  ```go
  type Analyzer interface {
      Analyze(ctx context.Context, snapshotID int64) (*AnalysisResult, error)
  }
  ```
- [ ] Define `AnalysisResult` struct with all issue types

### 6.2 Implement Delta Calculator
Location: `internal/analyzer/delta.go`

- [ ] Calculate query stats deltas between snapshots:
  ```go
  type QueryStatDelta struct {
      QueryID        int64
      Query          string
      DeltaCalls     int64
      DeltaTotalTime float64
      DeltaRows      int64
      MeanExecTime   float64  // computed from delta
  }
  ```
- [ ] Handle stats reset (negative delta = reset occurred)
- [ ] Handle missing queryids (new queries, evicted queries)

### 6.3 Implement SlowQueryAnalyzer
Location: `internal/analyzer/slow_queries.go`

- [ ] Identify queries where `mean_exec_time > threshold`
- [ ] Use delta values for "recent" analysis
- [ ] Include absolute values for historical context
- [ ] Return `[]SlowQuery` with:
  - QueryID, Query text
  - Mean/Max execution time
  - Call count in period
  - Cache hit ratio

### 6.4 Implement CacheAnalyzer
Location: `internal/analyzer/cache.go`

- [ ] Calculate database-level cache hit ratio
- [ ] Calculate per-query cache hit ratio
- [ ] Flag when ratio < threshold (default 95%)
- [ ] Return `CacheAnalysis` with:
  - Overall hit ratio
  - Queries with poor cache performance

### 6.5 Implement TableIssueAnalyzer
Location: `internal/analyzer/tables.go`

- [ ] Detect high dead tuple ratio (bloat indicator)
- [ ] Detect stale vacuum (last_vacuum too old)
- [ ] Detect tables needing analyze (last_analyze stale)
- [ ] Return `[]TableIssue` with:
  - Table name
  - Issue type
  - Current value vs threshold

### 6.6 Implement IndexIssueAnalyzer
Location: `internal/analyzer/indexes.go`

- [ ] Detect unused indexes (idx_scan = 0 for N days)
- [ ] Exclude primary keys and unique constraints
- [ ] Detect duplicate indexes (same columns, different names)
- [ ] Calculate potential space savings
- [ ] Detect missing indexes:
  - High seq_scan / (seq_scan + idx_scan) ratio
  - Table size above threshold
- [ ] Return `[]IndexIssue` with details

### 6.7 Implement Main Analyzer
Location: `internal/analyzer/main.go`

- [ ] Orchestrate all sub-analyzers
- [ ] Fetch required data from storage
- [ ] Aggregate results into `AnalysisResult`
- [ ] Handle partial failures gracefully

### 6.8 Write Tests
- [ ] Test delta calculation with various scenarios
- [ ] Test stats reset handling
- [ ] Test each analyzer with mock data
- [ ] Test threshold configurations

## Acceptance Criteria
- [ ] Delta calculation is accurate
- [ ] Stats reset is properly handled
- [ ] All issue types are detected
- [ ] Thresholds are configurable
- [ ] Tests pass
