# Task 08: Scheduler

## Objective
Implement job scheduler that coordinates data collection and analysis at configured intervals.

## Subtasks

### 8.1 Define Scheduler Interface
Location: `internal/scheduler/scheduler.go`

- [ ] Define `Scheduler` struct:
  ```go
  type Scheduler struct {
      config     *config.SchedulerConfig
      collectors []collector.Collector
      analyzer   analyzer.Analyzer
      suggester  suggester.Suggester
      storage    storage.Storage
      pgClient   postgres.Client
  }
  ```
- [ ] Define lifecycle methods:
  - `Start(ctx context.Context) error`
  - `Stop() error`
  - `TriggerSnapshot(ctx context.Context) error`

### 8.2 Implement Ticker-Based Scheduling
Location: `internal/scheduler/ticker.go`

- [ ] Create ticker for each unique interval
- [ ] Group collectors by interval
- [ ] Run collectors concurrently within group
- [ ] Use `time.Ticker` for simplicity

### 8.3 Implement Collection Job
Location: `internal/scheduler/jobs/collect.go`

- [ ] Create snapshot at job start
- [ ] Run all due collectors
- [ ] Handle individual collector failures
- [ ] Log collection results
- [ ] Record collection duration

### 8.4 Implement Analysis Job
Location: `internal/scheduler/jobs/analyze.go`

- [ ] Run after collection completes
- [ ] Fetch latest snapshot
- [ ] Run analyzer
- [ ] Run suggester with analysis results
- [ ] Log analysis results

### 8.5 Implement Maintenance Job
Location: `internal/scheduler/jobs/maintenance.go`

- [ ] Run daily (or configurable)
- [ ] Purge old snapshots based on retention
- [ ] Clean up orphaned data
- [ ] Log purge results

### 8.6 Implement Manual Trigger
- [ ] `TriggerSnapshot()` runs collection immediately
- [ ] Useful for API endpoint
- [ ] Prevent concurrent manual triggers

### 8.7 Implement Graceful Shutdown
- [ ] Stop all tickers
- [ ] Wait for in-progress jobs to complete
- [ ] Use context cancellation
- [ ] Timeout for forced shutdown

### 8.8 Implement Health Status
- [ ] Track last successful collection time
- [ ] Track last successful analysis time
- [ ] Expose for health check endpoint

### 8.9 Write Tests
- [ ] Test scheduler start/stop lifecycle
- [ ] Test job execution at intervals
- [ ] Test graceful shutdown
- [ ] Test manual trigger

## Acceptance Criteria
- [ ] Scheduler runs jobs at configured intervals
- [ ] Collection and analysis are coordinated
- [ ] Failures are isolated and logged
- [ ] Graceful shutdown works correctly
- [ ] Tests pass
