// Package resource provides collectors for table and index statistics.
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
	// TableStatsCollectorName is the unique name for this collector.
	TableStatsCollectorName = "table_stats"

	// DefaultTableStatsInterval is the default collection interval.
	DefaultTableStatsInterval = 5 * time.Minute
)

// TableStatsCollector collects table statistics from pg_stat_user_tables.
type TableStatsCollector struct {
	collector.BaseCollector
}

// TableStatsCollectorConfig holds configuration for TableStatsCollector.
type TableStatsCollectorConfig struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Interval   time.Duration
	Logger     *log.Logger
}

// NewTableStatsCollector creates a new TableStatsCollector.
func NewTableStatsCollector(cfg TableStatsCollectorConfig) *TableStatsCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultTableStatsInterval
	}

	return &TableStatsCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       TableStatsCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
	}
}

// Collect fetches table statistics and stores them.
func (c *TableStatsCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting table stats for snapshot %d", snapshotID)

	// Fetch table statistics from PostgreSQL
	stats, err := c.PGClient().GetStatTables(ctx)
	if err != nil {
		return err
	}

	c.Logf("collected %d table stats", len(stats))

	// Store in SQLite (historical)
	if err := c.Storage().SaveTableStats(ctx, snapshotID, stats); err != nil {
		return err
	}
	// Store in SQLite (current - for dashboard)
	if err := c.Storage().SaveCurrentTableStats(ctx, c.InstanceID(), stats); err != nil {
		c.Logf("warning: failed to save current table stats: %v", err)
	}

	return nil
}

// Ensure TableStatsCollector implements collector.Collector.
var _ collector.Collector = (*TableStatsCollector)(nil)
