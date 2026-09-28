// Package risk collects the signals that predict outages rather than slowness:
// transaction ID wraparound, replication slots, sequences, prepared transactions
// and database size.
package risk

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

const (
	// CollectorName is the unique name for this collector.
	CollectorName = "outage_risk"

	// DefaultInterval is the default collection interval. These signals move over
	// hours or days, so there is no need to sample them often.
	DefaultInterval = 5 * time.Minute
)

// Collector collects outage-risk signals.
type Collector struct {
	collector.BaseCollector
}

// Config holds configuration for Collector.
type Config struct {
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Interval   time.Duration
	Logger     *log.Logger
}

// NewCollector creates a new outage-risk Collector.
func NewCollector(cfg Config) *Collector {
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultInterval
	}

	return &Collector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       CollectorName,
			Interval:   interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
	}
}

// Collect fetches outage-risk signals and stores them with the snapshot, plus a
// size sample for the long-term growth trend.
//
// Individual checks that fail (usually a missing privilege) are logged and left
// out rather than failing the collector: a permanently failing collector would
// raise a collection-failure alert on every instance without pg_monitor.
func (c *Collector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting outage risk for snapshot %d", snapshotID)

	risk, err := c.PGClient().GetOutageRisk(ctx)
	if err != nil {
		return err
	}
	if risk == nil {
		return fmt.Errorf("outage risk returned no data")
	}

	checks := make([]string, 0, len(risk.Unavailable))
	for check := range risk.Unavailable {
		checks = append(checks, check)
	}
	sort.Strings(checks)
	for _, check := range checks {
		c.Logf("warning: outage-risk check %s unavailable: %s", check, risk.Unavailable[check])
	}

	if err := c.Storage().SaveOutageRisk(ctx, snapshotID, risk); err != nil {
		return err
	}

	if _, failed := risk.Unavailable["size"]; !failed && risk.ClusterSize > 0 {
		if err := c.Storage().RecordSize(ctx, c.InstanceID(), time.Now(), risk.ClusterSize); err != nil {
			c.Logf("warning: failed to record size history: %v", err)
		}
	}

	return nil
}

// Ensure Collector implements collector.Collector.
var _ collector.Collector = (*Collector)(nil)
