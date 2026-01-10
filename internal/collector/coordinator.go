package collector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/user/pganalyzer/internal/models"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

// Coordinator manages the lifecycle of snapshots and coordinates collector execution.
type Coordinator struct {
	pgClient   postgres.Client
	storage    sqlite.Storage
	instanceID int64
	collectors []Collector
	logger     *log.Logger
	mu         sync.RWMutex
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

// Collect runs all registered collectors and returns the result.
// It creates a single snapshot for the collection cycle and passes it to all collectors.
// Partial failures are tracked but don't stop other collectors from running.
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

	// Run all collectors
	for _, collector := range collectors {
		select {
		case <-ctx.Done():
			result.Errors[collector.Name()] = ctx.Err()
			continue
		default:
		}

		if err := collector.Collect(ctx, snapshotID); err != nil {
			c.logger.Printf("[coordinator] collector %s failed: %v", collector.Name(), err)
			result.Errors[collector.Name()] = err
		}
	}

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

// CollectWithTimeout runs collection with a timeout.
func (c *Coordinator) CollectWithTimeout(ctx context.Context, timeout time.Duration) (*CollectionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Collect(ctx)
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
