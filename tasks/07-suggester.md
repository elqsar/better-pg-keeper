# Task 07: Suggestion Engine

## Objective
Implement rules-based recommendation engine that generates actionable suggestions from analysis results.

## Subtasks

### 7.1 Define Rule Interface
Location: `internal/suggester/rule.go`

- [ ] Define `Rule` interface:
  ```go
  type Rule interface {
      ID() string
      Name() string
      Evaluate(ctx context.Context, analysis *AnalysisResult) ([]Suggestion, error)
  }
  ```
- [ ] Define `Suggestion` struct:
  ```go
  type Suggestion struct {
      RuleID       string
      Severity     Severity  // critical, warning, info
      Title        string
      Description  string
      TargetObject string    // table/index/query identifier
      Metadata     map[string]interface{}
  }
  ```

### 7.2 Implement SlowQueryRule
Location: `internal/suggester/rules/slow_query.go`

- [ ] Rule ID: `slow_query`
- [ ] Trigger: `mean_exec_time > threshold`
- [ ] Severity: warning (>1s), critical (>5s)
- [ ] Title: "Slow query detected: {query_preview}"
- [ ] Description: Include execution stats, optimization hints
- [ ] Metadata: queryid, mean_time, call_count

### 7.3 Implement UnusedIndexRule
Location: `internal/suggester/rules/unused_index.go`

- [ ] Rule ID: `unused_index`
- [ ] Trigger: `idx_scan = 0` for N consecutive snapshots
- [ ] Exclude: primary keys, unique constraints
- [ ] Severity: warning
- [ ] Title: "Unused index: {index_name}"
- [ ] Description: Include size, days unused, DROP statement
- [ ] Metadata: index_size, days_unused, table_name

### 7.4 Implement MissingIndexRule
Location: `internal/suggester/rules/missing_index.go`

- [ ] Rule ID: `missing_index`
- [ ] Trigger: high seq_scan ratio on large tables
- [ ] Skip: tables below min size threshold
- [ ] Severity: info (moderate ratio), warning (high ratio)
- [ ] Title: "Consider index on {table_name}"
- [ ] Description: Seq scan stats, table size
- [ ] Metadata: seq_scan_count, table_size

### 7.5 Implement BloatRule
Location: `internal/suggester/rules/bloat.go`

- [ ] Rule ID: `table_bloat`
- [ ] Trigger: dead_tup_ratio > threshold
- [ ] Severity: warning (>20%), critical (>50%)
- [ ] Title: "High bloat on {table_name}"
- [ ] Description: Dead tuple %, VACUUM recommendation
- [ ] Metadata: dead_tuples, live_tuples, ratio

### 7.6 Implement VacuumRule
Location: `internal/suggester/rules/vacuum.go`

- [ ] Rule ID: `stale_vacuum`
- [ ] Trigger: last_vacuum older than threshold, high dead tuples
- [ ] Severity: warning
- [ ] Title: "VACUUM needed on {table_name}"
- [ ] Description: Last vacuum time, dead tuple count
- [ ] Metadata: last_vacuum, dead_tuples

### 7.7 Implement CacheRule
Location: `internal/suggester/rules/cache.go`

- [ ] Rule ID: `low_cache_hit`
- [ ] Trigger: cache_hit_ratio < threshold
- [ ] Severity: warning (<95%), critical (<90%)
- [ ] Title: "Low cache hit ratio"
- [ ] Description: Current ratio, memory recommendations
- [ ] Metadata: hit_ratio, blks_hit, blks_read

### 7.8 Implement Suggester
Location: `internal/suggester/suggester.go`

- [ ] Register all rules
- [ ] Run rules against analysis results
- [ ] Deduplicate suggestions (same rule + target)
- [ ] Upsert to storage (update last_seen_at)
- [ ] Mark resolved suggestions (issue no longer detected)

### 7.9 Write Tests
- [ ] Test each rule with triggering data
- [ ] Test each rule with non-triggering data
- [ ] Test deduplication logic
- [ ] Test severity calculation

## Acceptance Criteria
- [ ] All rules are implemented
- [ ] Suggestions have actionable descriptions
- [ ] Deduplication prevents duplicates
- [ ] Resolved issues are marked
- [ ] Tests pass
