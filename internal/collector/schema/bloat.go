// Package schema provides collectors for schema-related statistics.
package schema

import (
	"context"
	"log"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

const (
	// BloatCollectorName is the unique name for this collector.
	BloatCollectorName = "bloat"

	// DefaultBloatInterval is the default collection interval.
	DefaultBloatInterval = 1 * time.Hour

	// MinDeadTuples is the minimum number of dead tuples to consider a table bloated.
	MinDeadTuples = 1000
)

// BloatCollector collects table bloat information.
type BloatCollector struct {
	collector.BaseCollector

	// minDeadTuples is the threshold for filtering significant bloat.
	minDeadTuples int64
}

// BloatCollectorConfig holds configuration for BloatCollector.
type BloatCollectorConfig struct {
	PGClient      postgres.Client
	Storage       sqlite.Storage
	InstanceID    int64
	Interval      time.Duration
	MinDeadTuples int64
	Logger        *log.Logger
}

// NewBloatCollector creates a new BloatCollector.
func NewBloatCollector(cfg BloatCollectorConfig) *BloatCollector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultBloatInterval
	}

	minDeadTuples := cfg.MinDeadTuples
	if minDeadTuples == 0 {
		minDeadTuples = MinDeadTuples
	}

	return &BloatCollector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       BloatCollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
		minDeadTuples: minDeadTuples,
	}
}

// Collect fetches table bloat information and stores significant bloat.
func (c *BloatCollector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting bloat stats for snapshot %d", snapshotID)

	// Fetch bloat information from PostgreSQL
	bloatInfo, err := c.PGClient().GetTableBloat(ctx)
	if err != nil {
		return err
	}

	// Filter tables with significant bloat
	var significantBloat []models.BloatInfo
	for _, info := range bloatInfo {
		if info.NDeadTup >= c.minDeadTuples {
			significantBloat = append(significantBloat, info)
			c.Logf("bloat detected: %s.%s - %d dead tuples (%.2f%%)",
				info.SchemaName, info.RelName, info.NDeadTup, info.BloatPercent)
		}
	}

	c.Logf("collected %d total tables, %d with significant bloat", len(bloatInfo), len(significantBloat))

	// Store significant bloat in SQLite (historical)
	if err := c.Storage().SaveBloatStats(ctx, snapshotID, significantBloat); err != nil {
		return err
	}
	// Store bloat in SQLite (current - for dashboard)
	if err := c.Storage().SaveCurrentBloatStats(ctx, c.InstanceID(), significantBloat); err != nil {
		c.Logf("warning: failed to save current bloat stats: %v", err)
	}

	return nil
}

// Ensure BloatCollector implements collector.Collector.
var _ collector.Collector = (*BloatCollector)(nil)
