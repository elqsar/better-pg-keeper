//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/user/pganalyzer/internal/config"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

// TestEnv holds the test environment configuration.
type TestEnv struct {
	PGClient postgres.Client
	Storage  sqlite.Storage
	Config   *config.Config
}

// SetupTestEnv creates a test environment with real PostgreSQL connection.
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	cfg := loadTestConfig(t)

	// Create storage with in-memory SQLite for faster tests
	storage, err := sqlite.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create PostgreSQL client
	pgClient, err := postgres.NewClient(postgres.Config{
		Host:     cfg.Postgres.Host,
		Port:     cfg.Postgres.Port,
		Database: cfg.Postgres.Database,
		User:     cfg.Postgres.User,
		Password: cfg.Postgres.Password,
		SSLMode:  cfg.Postgres.SSLMode,
	}, postgres.DefaultClientConfig())
	if err != nil {
		t.Fatalf("Failed to create PostgreSQL client: %v", err)
	}

	// Connect to PostgreSQL
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := pgClient.Connect(ctx); err != nil {
		t.Skipf("Skipping integration test: cannot connect to PostgreSQL: %v", err)
	}

	// Verify pg_stat_statements is available
	if err := verifyPgStatStatements(ctx, pgClient); err != nil {
		t.Skipf("Skipping integration test: pg_stat_statements not available: %v", err)
	}

	return &TestEnv{
		PGClient: pgClient,
		Storage:  storage,
		Config:   cfg,
	}
}

// Cleanup closes connections.
func (env *TestEnv) Cleanup() {
	if env.PGClient != nil {
		env.PGClient.Close()
	}
	if env.Storage != nil {
		env.Storage.Close()
	}
}

// loadTestConfig loads configuration from environment variables.
func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := config.Default()

	// Override with environment variables
	if host := os.Getenv("POSTGRES_HOST"); host != "" {
		cfg.Postgres.Host = host
	} else {
		cfg.Postgres.Host = "localhost"
	}

	if port := os.Getenv("POSTGRES_PORT"); port != "" {
		var p int
		fmt.Sscanf(port, "%d", &p)
		cfg.Postgres.Port = p
	} else {
		cfg.Postgres.Port = 5432
	}

	if db := os.Getenv("POSTGRES_DATABASE"); db != "" {
		cfg.Postgres.Database = db
	} else {
		cfg.Postgres.Database = "testdb"
	}

	if user := os.Getenv("POSTGRES_USER"); user != "" {
		cfg.Postgres.User = user
	} else {
		cfg.Postgres.User = "postgres"
	}

	if pass := os.Getenv("POSTGRES_PASSWORD"); pass != "" {
		cfg.Postgres.Password = pass
	} else {
		cfg.Postgres.Password = "postgres"
	}

	if sslmode := os.Getenv("POSTGRES_SSLMODE"); sslmode != "" {
		cfg.Postgres.SSLMode = sslmode
	} else {
		cfg.Postgres.SSLMode = "disable"
	}

	return cfg
}

// verifyPgStatStatements checks if pg_stat_statements extension is available.
func verifyPgStatStatements(ctx context.Context, client postgres.Client) error {
	stats, err := client.GetStatStatements(ctx, 1)
	if err != nil {
		return fmt.Errorf("pg_stat_statements query failed: %w", err)
	}
	// At least some activity should be recorded
	_ = stats
	return nil
}

// generateQueryActivity runs some queries to generate pg_stat_statements data.
func generateQueryActivity(ctx context.Context, client postgres.Client) error {
	// Run some queries to generate activity
	for i := 0; i < 10; i++ {
		_, err := client.GetStatStatements(ctx, 100)
		if err != nil {
			return err
		}
		_, err = client.GetStatTables(ctx)
		if err != nil {
			return err
		}
		_, err = client.GetStatIndexes(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
