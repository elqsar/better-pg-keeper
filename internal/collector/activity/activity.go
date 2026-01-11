// Package activity provides collectors for connection activity and session state.
package activity

import (
	"context"
	"log"
	"time"

	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

const (
	// ActivityCollectorName is the unique name for this collector.
	ActivityCollectorName = "activity"

	// DefaultActivityInterval is the default collection interval.
	DefaultActivityInterval = 30 * time.Second

	// DefaultLongRunningThreshold is the default threshold for long-running queries.
	DefaultLongRunningThreshold = 60.0 // seconds

	// DefaultIdleInTxThreshold is the default threshold for idle-in-transaction connections.
	DefaultIdleInTxThreshold = 60.0 // seconds
)

// ActivityCollector collects connection activity from pg_stat_activity.
type ActivityCollector struct {
	collector.BaseCollector
	longRunningThreshold float64
	idleInTxThreshold    float64
}

// ActivityCollectorConfig holds configuration for ActivityCollector.
type ActivityCollectorConfig struct {
	PGClient             postgres.Client
	Storage              sqlite.Storage
	InstanceID           int64
	Interval             time.Duration
	LongRunningThreshold float64
	IdleInTxThreshold    float64
	Logger               *log.Logger
}

// NewActivityCollector creates a new ActivityCollector.
func NewActivityCollector(cfg ActivityCollectorConfig) *ActivityCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultActivityInterval
	}

	longRunningThreshold := cfg.LongRunningThreshold
	if longRunningThreshold == 0 {
		longRunningThreshold = DefaultLongRunningThreshold
	}

	idleInTxThreshold := cfg.IdleInTxThreshold
	if idleInTxThreshold == 0 {
		idleInTxThreshold = DefaultIdleInTxThreshold
	}

	return &ActivityCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       ActivityCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
		longRunningThreshold: longRunningThreshold,
		idleInTxThreshold:    idleInTxThreshold,
	}
}

// Collect fetches connection activity and stores it.
func (c *ActivityCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting connection activity for snapshot %d", snapshotID)

	// Fetch connection activity summary
	activity, err := c.PGClient().GetConnectionActivity(ctx)
	if err != nil {
		return err
	}

	c.Logf("connection activity: %d active, %d idle, %d idle-in-tx, %d waiting (total: %d/%d)",
		activity.ActiveCount, activity.IdleCount, activity.IdleInTxCount,
		activity.WaitingCount, activity.TotalConnections, activity.MaxConnections)

	// Store connection activity (historical)
	if err := c.Storage().SaveConnectionActivity(ctx, snapshotID, activity); err != nil {
		return err
	}
	// Store connection activity (current - for dashboard)
	if err := c.Storage().SaveCurrentConnectionActivity(ctx, c.InstanceID(), activity); err != nil {
		c.Logf("warning: failed to save current connection activity: %v", err)
	}

	// Fetch and store long-running queries
	longRunning, err := c.PGClient().GetLongRunningQueries(ctx, c.longRunningThreshold)
	if err != nil {
		c.Logf("warning: failed to get long-running queries: %v", err)
	} else {
		if len(longRunning) > 0 {
			c.Logf("found %d long-running queries (>%.0fs)", len(longRunning), c.longRunningThreshold)
		}
		// Historical
		if err := c.Storage().SaveLongRunningQueries(ctx, snapshotID, longRunning); err != nil {
			return err
		}
		// Current (for dashboard)
		if err := c.Storage().SaveCurrentLongRunningQueries(ctx, c.InstanceID(), longRunning); err != nil {
			c.Logf("warning: failed to save current long-running queries: %v", err)
		}
	}

	// Fetch and store idle-in-transaction connections
	idleInTx, err := c.PGClient().GetIdleInTransaction(ctx, c.idleInTxThreshold)
	if err != nil {
		c.Logf("warning: failed to get idle-in-transaction: %v", err)
	} else {
		if len(idleInTx) > 0 {
			c.Logf("found %d idle-in-transaction connections (>%.0fs)", len(idleInTx), c.idleInTxThreshold)
		}
		// Historical
		if err := c.Storage().SaveIdleInTransaction(ctx, snapshotID, idleInTx); err != nil {
			return err
		}
		// Current (for dashboard)
		if err := c.Storage().SaveCurrentIdleInTransaction(ctx, c.InstanceID(), idleInTx); err != nil {
			c.Logf("warning: failed to save current idle-in-transaction: %v", err)
		}
	}

	return nil
}

// Ensure ActivityCollector implements collector.Collector.
var _ collector.Collector = (*ActivityCollector)(nil)
