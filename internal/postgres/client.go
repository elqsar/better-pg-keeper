// Package postgres provides PostgreSQL client functionality for collecting
// statistics and running queries against monitored databases.
package postgres

import (
	"context"
	"time"

	"github.com/user/pganalyzer/internal/models"
)

// Client defines the interface for PostgreSQL database operations.
type Client interface {
	// Connection management
	Connect(ctx context.Context) error
	Close() error
	Ping(ctx context.Context) error

	// Stats collection
	GetStatStatements(ctx context.Context) ([]models.QueryStat, error)
	GetStatTables(ctx context.Context) ([]models.TableStat, error)
	GetStatIndexes(ctx context.Context) ([]models.IndexStat, error)
	GetDatabaseStats(ctx context.Context) (*models.DatabaseStats, error)

	// Schema analysis
	GetTableBloat(ctx context.Context) ([]models.BloatInfo, error)
	GetIndexDetails(ctx context.Context) ([]models.IndexDetail, error)

	// Query analysis
	Explain(ctx context.Context, query string, analyze bool) (*models.ExplainPlan, error)

	// Metadata
	GetVersion(ctx context.Context) (string, error)
	GetStatsResetTime(ctx context.Context) (*time.Time, error)
}

// ClientConfig holds configuration for connecting to PostgreSQL.
type ClientConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	SSLMode         string
	ConnectTimeout  time.Duration
	MaxConnections  int
	MinConnections  int
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// DefaultClientConfig returns a ClientConfig with sensible defaults.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Port:            5432,
		SSLMode:         "prefer",
		ConnectTimeout:  10 * time.Second,
		MaxConnections:  5,
		MinConnections:  1,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	}
}
