package collector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// Coordinator manages the lifecycle of snapshots and coordinates collector execution.
type Coordinator struct {
	pgClient   postgres.Client
	storage    sqlite.Storage
	instanceID int64
	collectors []Collector
	logger     *log.Logger
	mu         sync.RWMutex

	// lastRun tracks the last execution time for each collector by name.
	lastRun map[string]time.Time
}

// CoordinatorConfig holds configuration for creating a Coordinator.
type CoordinatorConfig struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Logger     *log.Logger
}

// NewCoordinator creates a new Coordinator.
func NewCoordinator(cfg CoordinatorConfig) *Coordinator {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	return &Coordinator{
		pgClient:   cfg.PGClient,
		storage:    cfg.Storage,
		instanceID: cfg.InstanceID,
		logger:     logger,
		lastRun:    make(map[string]time.Time),
	}
}

// RegisterCollector adds a collector to the coordinator.
func (c *Coordinator) RegisterCollector(collector Collector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectors = append(c.collectors, collector)
}

// RegisterCollectors adds multiple collectors to the coordinator.
func (c *Coordinator) RegisterCollectors(collectors ...Collector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectors = append(c.collectors, collectors...)
}

// Collectors returns a copy of the registered collectors.
func (c *Coordinator) Collectors() []Collector {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]Collector, len(c.collectors))
	copy(result, c.collectors)
	return result
}

// CollectionResult represents the result of a collection cycle.
type CollectionResult struct {
	SnapshotID int64
	StartedAt  time.Time
	FinishedAt time.Time
	Errors     map[string]error // collector name -> error
}

// HasErrors returns true if any collector failed.
func (r *CollectionResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// Error returns a combined error message if any collectors failed.
func (r *CollectionResult) Error() error {
	if !r.HasErrors() {
		return nil
	}

	var errMsgs []string
	for name, err := range r.Errors {
		errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", name, err))
	}
	return fmt.Errorf("collection errors: %v", errMsgs)
}

// Collect runs due collectors and returns the result.
// It reuses a recent snapshot or creates a new one, then passes it to all due collectors.
// Collectors are run concurrently and partial failures are tracked but don't stop others.
// A collector is "due" if enough time has passed since its last run based on its Interval().
func (c *Coordinator) Collect(ctx context.Context) (*CollectionResult, error) {
	c.mu.RLock()
	collectors := c.collectors
	c.mu.RUnlock()

	result := &CollectionResult{
		StartedAt: time.Now(),
		Errors:    make(map[string]error),
	}

	if len(collectors) == 0 {
		result.FinishedAt = time.Now()
		return result, nil
	}

	// Determine which collectors are due to run
	dueCollectors := c.getDueCollectors(collectors, result.StartedAt)

	if len(dueCollectors) == 0 {
		c.logger.Printf("[coordinator] no collectors due to run")
		result.FinishedAt = time.Now()
		return result, nil
	}

	c.logger.Printf("[coordinator] %d/%d collectors due to run", len(dueCollectors), len(collectors))

	// Reuse a recent snapshot (within 1 minute) to avoid fragmented data
	// This ensures all collectors contribute to the same snapshot instead of
	// creating separate snapshots that only have partial data
	snapshotID, err := c.GetOrCreateSnapshot(ctx, time.Minute)
	if err != nil {
		return nil, fmt.Errorf("getting or creating snapshot: %w", err)
	}
	result.SnapshotID = snapshotID
	c.logger.Printf("[coordinator] using snapshot %d for instance %d", snapshotID, c.instanceID)

	// Run due collectors concurrently
	var wg sync.WaitGroup
	var errorsMu sync.Mutex

	for _, coll := range dueCollectors {
		wg.Add(1)
		go func(collector Collector) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				errorsMu.Lock()
				result.Errors[collector.Name()] = ctx.Err()
				errorsMu.Unlock()
				return
			default:
			}

			if err := collector.Collect(ctx, snapshotID); err != nil {
				c.logger.Printf("[coordinator] collector %s failed: %v", collector.Name(), err)
				errorsMu.Lock()
				result.Errors[collector.Name()] = err
				errorsMu.Unlock()
			} else {
				// Update last run time on success
				c.mu.Lock()
				c.lastRun[collector.Name()] = time.Now()
				c.mu.Unlock()
			}
		}(coll)
	}

	wg.Wait()
	result.FinishedAt = time.Now()

	if result.HasErrors() {
		c.logger.Printf("[coordinator] collection completed with %d errors in %v",
			len(result.Errors), result.FinishedAt.Sub(result.StartedAt))
	} else {
		c.logger.Printf("[coordinator] collection completed successfully in %v",
			result.FinishedAt.Sub(result.StartedAt))
	}

	return result, nil
}

// getDueCollectors returns collectors that are due to run based on their intervals.
// A collector is due if it has never run, or if its interval has elapsed since last run.
func (c *Coordinator) getDueCollectors(collectors []Collector, now time.Time) []Collector {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var due []Collector
	for _, coll := range collectors {
		lastRun, hasRun := c.lastRun[coll.Name()]
		interval := coll.Interval()

		// If never run, or interval has elapsed, collector is due
		if !hasRun || now.Sub(lastRun) >= interval {
			due = append(due, coll)
			c.logger.Printf("[coordinator] collector %s is due (interval=%v, last_run=%v)",
				coll.Name(), interval, lastRun)
		}
	}
	return due
}

// CollectWithTimeout runs collection with a timeout.
func (c *Coordinator) CollectWithTimeout(ctx context.Context, timeout time.Duration) (*CollectionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Collect(ctx)
}

// CollectAll runs ALL registered collectors regardless of their intervals.
// This is useful for manual triggers where the user explicitly wants a full snapshot.
// Collectors are run concurrently and partial failures are tracked.
func (c *Coordinator) CollectAll(ctx context.Context) (*CollectionResult, error) {
	c.mu.RLock()
	collectors := c.collectors
	c.mu.RUnlock()

	result := &CollectionResult{
		StartedAt: time.Now(),
		Errors:    make(map[string]error),
	}

	if len(collectors) == 0 {
		result.FinishedAt = time.Now()
		return result, nil
	}

	c.logger.Printf("[coordinator] running all %d collectors (forced)", len(collectors))

	// Get PostgreSQL version for snapshot metadata
	pgVersion, err := c.pgClient.GetVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting PostgreSQL version: %w", err)
	}

	// Get stats reset time
	statsReset, _ := c.pgClient.GetStatsResetTime(ctx)

	// Create a new snapshot
	snapshot := &models.Snapshot{
		InstanceID: c.instanceID,
		CapturedAt: result.StartedAt,
		PGVersion:  pgVersion,
		StatsReset: statsReset,
	}

	snapshotID, err := c.storage.CreateSnapshot(ctx, snapshot)
	if err != nil {
		return nil, fmt.Errorf("creating snapshot: %w", err)
	}
	result.SnapshotID = snapshotID

	c.logger.Printf("[coordinator] created snapshot %d for instance %d", snapshotID, c.instanceID)

	// Run all collectors concurrently
	var wg sync.WaitGroup
	var errorsMu sync.Mutex

	for _, coll := range collectors {
		wg.Add(1)
		go func(collector Collector) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				errorsMu.Lock()
				result.Errors[collector.Name()] = ctx.Err()
				errorsMu.Unlock()
				return
			default:
			}

			if err := collector.Collect(ctx, snapshotID); err != nil {
				c.logger.Printf("[coordinator] collector %s failed: %v", collector.Name(), err)
				errorsMu.Lock()
				result.Errors[collector.Name()] = err
				errorsMu.Unlock()
			} else {
				// Update last run time on success
				c.mu.Lock()
				c.lastRun[collector.Name()] = time.Now()
				c.mu.Unlock()
			}
		}(coll)
	}

	wg.Wait()
	result.FinishedAt = time.Now()

	if result.HasErrors() {
		c.logger.Printf("[coordinator] collection completed with %d errors in %v",
			len(result.Errors), result.FinishedAt.Sub(result.StartedAt))
	} else {
		c.logger.Printf("[coordinator] collection completed successfully in %v",
			result.FinishedAt.Sub(result.StartedAt))
	}

	return result, nil
}

// RunCollector runs a specific collector by name.
func (c *Coordinator) RunCollector(ctx context.Context, name string, snapshotID int64) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, collector := range c.collectors {
		if collector.Name() == name {
			return collector.Collect(ctx, snapshotID)
		}
	}

	return fmt.Errorf("collector not found: %s", name)
}

// GetLatestSnapshot returns the most recent snapshot for the instance.
func (c *Coordinator) GetLatestSnapshot(ctx context.Context) (*models.Snapshot, error) {
	return c.storage.GetLatestSnapshot(ctx, c.instanceID)
}

// GetOrCreateSnapshot gets the latest snapshot or creates a new one if none exists
// or if the latest is older than maxAge.
func (c *Coordinator) GetOrCreateSnapshot(ctx context.Context, maxAge time.Duration) (int64, error) {
	latest, err := c.storage.GetLatestSnapshot(ctx, c.instanceID)
	if err != nil {
		return 0, fmt.Errorf("getting latest snapshot: %w", err)
	}

	// If we have a recent snapshot, return its ID
	if latest != nil && time.Since(latest.CapturedAt) < maxAge {
		return latest.ID, nil
	}

	// Create a new snapshot
	pgVersion, _ := c.pgClient.GetVersion(ctx)
	statsReset, _ := c.pgClient.GetStatsResetTime(ctx)

	snapshot := &models.Snapshot{
		InstanceID: c.instanceID,
		CapturedAt: time.Now(),
		PGVersion:  pgVersion,
		StatsReset: statsReset,
	}

	return c.storage.CreateSnapshot(ctx, snapshot)
}

// CollectorIntervals returns a map of collector names to their intervals.
func (c *Coordinator) CollectorIntervals() map[string]time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	intervals := make(map[string]time.Duration)
	for _, collector := range c.collectors {
		intervals[collector.Name()] = collector.Interval()
	}
	return intervals
}

// MinInterval returns the minimum interval among all registered collectors.
// This is useful for determining how often the scheduler should check for due collectors.
// Returns 0 if no collectors are registered.
func (c *Coordinator) MinInterval() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.collectors) == 0 {
		return 0
	}

	minInterval := c.collectors[0].Interval()
	for _, coll := range c.collectors[1:] {
		if interval := coll.Interval(); interval < minInterval {
			minInterval = interval
		}
	}
	return minInterval
}

// CollectError represents an error from a specific collector.
type CollectError struct {
	CollectorName string
	Err           error
}

func (e *CollectError) Error() string {
	return fmt.Sprintf("collector %s: %v", e.CollectorName, e.Err)
}

func (e *CollectError) Unwrap() error {
	return e.Err
}

// MultiError represents multiple collection errors.
type MultiError struct {
	Errors []error
}

func (e *MultiError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}
	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}
	return fmt.Sprintf("%d errors: %v", len(e.Errors), e.Errors)
}

func (e *MultiError) Unwrap() []error {
	return e.Errors
}

// Is reports whether any error in the chain matches target.
func (e *MultiError) Is(target error) bool {
	for _, err := range e.Errors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
