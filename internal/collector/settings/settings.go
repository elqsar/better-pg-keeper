// Package settings collects the server configuration for review: pg_settings,
// pg_stat_statements capacity and tables with autovacuum turned off.
package settings

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
	CollectorName = "server_settings"

	// DefaultInterval is the default collection interval. Settings change
	// rarely, and a review an hour late is still useful.
	DefaultInterval = time.Hour
)

// Collector collects server settings.
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

// NewCollector creates a new server-settings Collector.
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

// Collect fetches the server settings and stores them with the snapshot.
// Checks that fail are logged and left out rather than failing the collector,
// as in the outage-risk collector.
func (c *Collector) Collect(ctx context.Context, snapshotID int64) error {
	c.Logf("collecting server settings for snapshot %d", snapshotID)

	settings, err := c.PGClient().GetServerSettings(ctx)
	if err != nil {
		return err
	}
	if settings == nil {
		return fmt.Errorf("server settings returned no data")
	}

	checks := make([]string, 0, len(settings.Unavailable))
	for check := range settings.Unavailable {
		checks = append(checks, check)
	}
	sort.Strings(checks)
	for _, check := range checks {
		c.Logf("warning: settings check %s unavailable: %s", check, settings.Unavailable[check])
	}

	return c.Storage().SaveServerSettings(ctx, snapshotID, settings)
}

// Ensure Collector implements collector.Collector.
var _ collector.Collector = (*Collector)(nil)
