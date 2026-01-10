// Package collector provides data collection from PostgreSQL databases.
package collector

import (
	"context"
	"time"
)

// Collector defines the interface for collecting metrics from PostgreSQL.
type Collector interface {
	// Name returns the collector's unique name.
	Name() string

	// Collect fetches metrics from PostgreSQL and stores them.
	// The snapshotID is provided for storing collected data.
	Collect(ctx context.Context, snapshotID int64) error

	// Interval returns how often this collector should run.
	Interval() time.Duration
}

// CollectorConfig holds common configuration for all collectors.
type CollectorConfig struct {
	// Enabled indicates whether this collector is active.
	Enabled bool

	// Interval overrides the default collection interval.
	Interval time.Duration
}

// DefaultCollectorConfig returns a CollectorConfig with default values.
func DefaultCollectorConfig() CollectorConfig {
	return CollectorConfig{
		Enabled:  true,
		Interval: 0, // Use collector's default
	}
}
