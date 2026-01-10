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

	// Store in SQLite
	if err := c.Storage().SaveIndexStats(ctx, snapshotID, stats); err != nil {
		return err
	}

	return nil
}

// Ensure IndexStatsCollector implements collector.Collector.
var _ collector.Collector = (*IndexStatsCollector)(nil)
