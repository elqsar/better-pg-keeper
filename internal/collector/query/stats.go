// Package query provides collectors for query-related statistics.
package query

import (
	"context"
	"log"
	"time"

	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

const (
	// QueryStatsCollectorName is the unique name for this collector.
	QueryStatsCollectorName = "query_stats"

	// DefaultQueryStatsInterval is the default collection interval.
	DefaultQueryStatsInterval = 1 * time.Minute
)

// StatsCollector collects query statistics from pg_stat_statements.
type StatsCollector struct {
	collector.BaseCollector

	// lastStatsReset tracks the last known stats_reset timestamp
	// to detect when statistics have been reset.
	lastStatsReset *time.Time
}

// StatsCollectorConfig holds configuration for StatsCollector.
type StatsCollectorConfig struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Interval   time.Duration
	Logger     *log.Logger
}

// NewStatsCollector creates a new StatsCollector.
func NewStatsCollector(cfg StatsCollectorConfig) *StatsCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultQueryStatsInterval
	}

	return &StatsCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       QueryStatsCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
	}
}

// Collect fetches query statistics from pg_stat_statements and stores them.
func (c *StatsCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting query stats for snapshot %d", snapshotID)

	// Check for stats reset
	resetTime, err := c.PGClient().GetStatsResetTime(ctx)
	if err != nil {
		c.Logf("warning: failed to get stats reset time: %v", err)
	} else if resetTime != nil {
		if c.lastStatsReset != nil && !resetTime.Equal(*c.lastStatsReset) {
			c.Logf("WARNING: pg_stat_statements was reset at %s (previous: %s)",
				resetTime.Format(time.RFC3339),
				c.lastStatsReset.Format(time.RFC3339))
		}
		c.lastStatsReset = resetTime
	}

	// Fetch query statistics from PostgreSQL
	stats, err := c.PGClient().GetStatStatements(ctx)
	if err != nil {
		return err
	}

	c.Logf("collected %d query stats", len(stats))

	// Store in SQLite (historical)
	if err := c.Storage().SaveQueryStats(ctx, snapshotID, stats); err != nil {
		return err
	}
	// Store in SQLite (current - for dashboard)
	if err := c.Storage().SaveCurrentQueryStats(ctx, c.InstanceID(), stats); err != nil {
		c.Logf("warning: failed to save current query stats: %v", err)
	}

	return nil
}

// Ensure StatsCollector implements collector.Collector.
var _ collector.Collector = (*StatsCollector)(nil)
