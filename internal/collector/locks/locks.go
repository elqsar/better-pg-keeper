// Package locks provides collectors for lock statistics and blocked queries.
package locks

import (
	"context"
	"log"
	"time"

	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

const (
	// LocksCollectorName is the unique name for this collector.
	LocksCollectorName = "locks"

	// DefaultLocksInterval is the default collection interval.
	DefaultLocksInterval = 30 * time.Second
)

// LocksCollector collects lock statistics from pg_locks.
type LocksCollector struct {
	collector.BaseCollector
}

// LocksCollectorConfig holds configuration for LocksCollector.
type LocksCollectorConfig struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Interval   time.Duration
	Logger     *log.Logger
}

// NewLocksCollector creates a new LocksCollector.
func NewLocksCollector(cfg LocksCollectorConfig) *LocksCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultLocksInterval
	}

	return &LocksCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       LocksCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
	}
}

// Collect fetches lock statistics and stores them.
func (c *LocksCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting lock stats for snapshot %d", snapshotID)

	// Fetch lock statistics
	stats, err := c.PGClient().GetLockStats(ctx)
	if err != nil {
		return err
	}

	c.Logf("lock stats: %d total, %d granted, %d waiting",
		stats.TotalLocks, stats.GrantedLocks, stats.WaitingLocks)

	// Store lock statistics
	if err := c.Storage().SaveLockStats(ctx, snapshotID, stats); err != nil {
		return err
	}

	// Fetch and store blocked queries
	blocked, err := c.PGClient().GetBlockedQueries(ctx)
	if err != nil {
		c.Logf("warning: failed to get blocked queries: %v", err)
	} else {
		if len(blocked) > 0 {
			c.Logf("found %d blocked queries", len(blocked))
			for _, b := range blocked {
				c.Logf("  blocked PID %d waiting %.1fs for %s lock on %v (held by PID %d)",
					b.BlockedPID, b.WaitDuration, b.LockMode, b.Relation, b.BlockingPID)
			}
		}
		if err := c.Storage().SaveBlockedQueries(ctx, snapshotID, blocked); err != nil {
			return err
		}
	}

	return nil
}

// Ensure LocksCollector implements collector.Collector.
var _ collector.Collector = (*LocksCollector)(nil)
