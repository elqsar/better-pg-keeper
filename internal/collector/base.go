package collector

import (
	"log"
	"time"

	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// BaseCollector provides shared functionality for all collectors.
type BaseCollector struct {
	// name is the collector's unique identifier.
	name string

	// interval is how often this collector runs.
	interval time.Duration

	// pgClient is the PostgreSQL client for fetching stats.
	pgClient postgres.Client

	// storage is the SQLite storage for persisting data.
	storage sqlite.Storage

	// instanceID is the monitored PostgreSQL instance ID.
	instanceID int64

	// logger for collector-specific logging.
	logger *log.Logger
}

// BaseCollectorConfig holds configuration for creating a BaseCollector.
type BaseCollectorConfig struct {
	Name       string
	Interval   time.Duration
	PGClient   postgres.Client
	Storage    sqlite.Storage
	InstanceID int64
	Logger     *log.Logger
}

// NewBaseCollector creates a new BaseCollector with the given configuration.
func NewBaseCollector(cfg BaseCollectorConfig) BaseCollector {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	return BaseCollector{
		name:       cfg.Name,
		interval:   cfg.Interval,
		pgClient:   cfg.PGClient,
		storage:    cfg.Storage,
		instanceID: cfg.InstanceID,
		logger:     logger,
	}
}

// Name returns the collector's name.
func (b *BaseCollector) Name() string {
	return b.name
}

// Interval returns the collection interval.
func (b *BaseCollector) Interval() time.Duration {
	return b.interval
}

// PGClient returns the PostgreSQL client.
func (b *BaseCollector) PGClient() postgres.Client {
	return b.pgClient
}

// Storage returns the storage interface.
func (b *BaseCollector) Storage() sqlite.Storage {
	return b.storage
}

// InstanceID returns the instance ID.
func (b *BaseCollector) InstanceID() int64 {
	return b.instanceID
}

// Logger returns the logger.
func (b *BaseCollector) Logger() *log.Logger {
	return b.logger
}

// Logf logs a formatted message with the collector name prefix.
func (b *BaseCollector) Logf(format string, args ...any) {
	b.logger.Printf("[%s] "+format, append([]any{b.name}, args...)...)
}
