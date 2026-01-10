package resource

import (
	"context"
	"log"
	"time"

	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

const (
	// DatabaseStatsCollectorName is the unique name for this collector.
	DatabaseStatsCollectorName = "database_stats"

	// DefaultDatabaseStatsInterval is the default collection interval.
	DefaultDatabaseStatsInterval = 1 * time.Minute
)

// DatabaseStatsCollector collects database-level statistics (cache hit ratio).
type DatabaseStatsCollector struct {
	collector.BaseCollector
}

// DatabaseStatsCollectorConfig holds configuration for DatabaseStatsCollector.
type DatabaseStatsCollectorConfig struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Interval   time.Duration
	Logger     *log.Logger
}

// NewDatabaseStatsCollector creates a new DatabaseStatsCollector.
func NewDatabaseStatsCollector(cfg DatabaseStatsCollectorConfig) *DatabaseStatsCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultDatabaseStatsInterval
	}

	return &DatabaseStatsCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       DatabaseStatsCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
	}
}

// Collect fetches database statistics and stores the cache hit ratio.
func (c *DatabaseStatsCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting database stats for snapshot %d", snapshotID)

	// Fetch database statistics from PostgreSQL
	stats, err := c.PGClient().GetDatabaseStats(ctx)
	if err != nil {
		return err
	}

	c.Logf("cache hit ratio: %.2f%%", stats.CacheHitRatio)

	// Store cache hit ratio in the snapshot
	if err := c.Storage().UpdateSnapshotCacheHitRatio(ctx, snapshotID, stats.CacheHitRatio); err != nil {
		return err
	}

	return nil
}

// Ensure DatabaseStatsCollector implements collector.Collector.
var _ collector.Collector = (*DatabaseStatsCollector)(nil)
