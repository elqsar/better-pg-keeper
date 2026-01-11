package resource

import (
	"context"
	"log"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

const (
	// IndexStatsCollectorName is the unique name for this collector.
	IndexStatsCollectorName = "index_stats"

	// DefaultIndexStatsInterval is the default collection interval.
	DefaultIndexStatsInterval = 5 * time.Minute
)

// IndexStatsCollector collects index statistics from pg_stat_user_indexes.
type IndexStatsCollector struct {
	collector.BaseCollector
}

// IndexStatsCollectorConfig holds configuration for IndexStatsCollector.
type IndexStatsCollectorConfig struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Interval   time.Duration
	Logger     *log.Logger
}

// NewIndexStatsCollector creates a new IndexStatsCollector.
func NewIndexStatsCollector(cfg IndexStatsCollectorConfig) *IndexStatsCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultIndexStatsInterval
	}

	return &IndexStatsCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       IndexStatsCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
	}
}

// Collect fetches index statistics and stores them.
func (c *IndexStatsCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting index stats for snapshot %d", snapshotID)

	// Fetch index statistics from PostgreSQL
	stats, err := c.PGClient().GetStatIndexes(ctx)
	if err != nil {
		return err
	}

	c.Logf("collected %d index stats", len(stats))

	// Store in SQLite (historical)
	if err := c.Storage().SaveIndexStats(ctx, snapshotID, stats); err != nil {
		return err
	}
	// Store in SQLite (current - for dashboard)
	if err := c.Storage().SaveCurrentIndexStats(ctx, c.InstanceID(), stats); err != nil {
		c.Logf("warning: failed to save current index stats: %v", err)
	}

	return nil
}

// Ensure IndexStatsCollector implements collector.Collector.
var _ collector.Collector = (*IndexStatsCollector)(nil)
