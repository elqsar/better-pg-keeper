package collector_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/collector/query"
	"github.com/elqsar/pganalyzer/internal/collector/resource"
	"github.com/elqsar/pganalyzer/internal/collector/schema"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// mockPGClient is a mock PostgreSQL client for testing.
type mockPGClient struct {
	version          string
	statsResetTime   *time.Time
	statStatements   []models.QueryStat
	statTables       []models.TableStat
	statIndexes      []models.IndexStat
	databaseStats    *models.DatabaseStats
	tableBloat       []models.BloatInfo
	indexDetails     []models.IndexDetail
	explainPlan      *models.ExplainPlan
	connectErr       error
	getStatementsErr error
	getTablesErr     error
	getIndexesErr    error
	getDatabaseErr   error
	getBloatErr      error
}

func (m *mockPGClient) Connect(ctx context.Context) error {
	return m.connectErr
}

func (m *mockPGClient) Close() error {
	return nil
}

func (m *mockPGClient) Ping(ctx context.Context) error {
	return nil
}

func (m *mockPGClient) GetStatStatements(ctx context.Context) ([]models.QueryStat, error) {
	if m.getStatementsErr != nil {
		return nil, m.getStatementsErr
	}
	return m.statStatements, nil
}

func (m *mockPGClient) GetStatTables(ctx context.Context) ([]models.TableStat, error) {
	if m.getTablesErr != nil {
		return nil, m.getTablesErr
	}
	return m.statTables, nil
}

func (m *mockPGClient) GetStatIndexes(ctx context.Context) ([]models.IndexStat, error) {
	if m.getIndexesErr != nil {
		return nil, m.getIndexesErr
	}
	return m.statIndexes, nil
}

func (m *mockPGClient) GetDatabaseStats(ctx context.Context) (*models.DatabaseStats, error) {
	if m.getDatabaseErr != nil {
		return nil, m.getDatabaseErr
	}
	return m.databaseStats, nil
}

func (m *mockPGClient) GetTableBloat(ctx context.Context) ([]models.BloatInfo, error) {
	if m.getBloatErr != nil {
		return nil, m.getBloatErr
	}
	return m.tableBloat, nil
}

func (m *mockPGClient) GetIndexDetails(ctx context.Context) ([]models.IndexDetail, error) {
	return m.indexDetails, nil
}

func (m *mockPGClient) Explain(ctx context.Context, query string, analyze bool) (*models.ExplainPlan, error) {
	return m.explainPlan, nil
}

func (m *mockPGClient) ExplainWithParams(ctx context.Context, query string, params []any, analyze bool) (*models.ExplainPlan, error) {
	return m.explainPlan, nil
}

func (m *mockPGClient) GetVersion(ctx context.Context) (string, error) {
	return m.version, nil
}

func (m *mockPGClient) GetStatsResetTime(ctx context.Context) (*time.Time, error) {
	return m.statsResetTime, nil
}

// Operational stats methods
func (m *mockPGClient) GetConnectionActivity(ctx context.Context) (*models.ConnectionActivity, error) {
	return nil, nil
}

func (m *mockPGClient) GetLongRunningQueries(ctx context.Context, thresholdSeconds float64) ([]models.LongRunningQuery, error) {
	return nil, nil
}

func (m *mockPGClient) GetIdleInTransaction(ctx context.Context, thresholdSeconds float64) ([]models.IdleInTransaction, error) {
	return nil, nil
}

func (m *mockPGClient) GetLockStats(ctx context.Context) (*models.LockStats, error) {
	return nil, nil
}

func (m *mockPGClient) GetBlockedQueries(ctx context.Context) ([]models.BlockedQuery, error) {
	return nil, nil
}

func (m *mockPGClient) GetExtendedDatabaseStats(ctx context.Context) (*models.ExtendedDatabaseStats, error) {
	return nil, nil
}

// setupTestStorage creates a temporary SQLite storage for testing.
func setupTestStorage(t *testing.T) sqlite.Storage {
	t.Helper()
	storage, err := sqlite.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { storage.Close() })
	return storage
}

// setupTestInstance creates a test instance and returns its ID.
func setupTestInstance(t *testing.T, storage sqlite.Storage) int64 {
	t.Helper()
	ctx := context.Background()
	inst := &models.Instance{
		Name:     "test",
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
	}
	id, err := storage.GetOrCreateInstance(ctx, inst)
	if err != nil {
		t.Fatalf("failed to create instance: %v", err)
	}
	return id
}

// setupTestSnapshot creates a test snapshot and returns its ID.
func setupTestSnapshot(t *testing.T, storage sqlite.Storage, instanceID int64) int64 {
	t.Helper()
	ctx := context.Background()
	snap := &models.Snapshot{
		InstanceID: instanceID,
		CapturedAt: time.Now(),
		PGVersion:  "PostgreSQL 14.5",
	}
	id, err := storage.CreateSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}
	return id
}

func TestCollectorInterface(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	mock := &mockPGClient{version: "PostgreSQL 14.5"}

	// Test that all collectors implement the Collector interface
	collectors := []collector.Collector{
		query.NewStatsCollector(query.StatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewIndexStatsCollector(resource.IndexStatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		schema.NewBloatCollector(schema.BloatCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
	}

	for _, c := range collectors {
		if c.Name() == "" {
			t.Errorf("collector has empty name")
		}
		if c.Interval() <= 0 {
			t.Errorf("collector %s has non-positive interval: %v", c.Name(), c.Interval())
		}
	}
}

func TestQueryStatsCollector(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
		statStatements: []models.QueryStat{
			{
				QueryID:       12345,
				Query:         "SELECT * FROM users",
				Calls:         100,
				TotalExecTime: 1000.5,
				MeanExecTime:  10.005,
			},
			{
				QueryID:       67890,
				Query:         "UPDATE users SET name = $1",
				Calls:         50,
				TotalExecTime: 500.0,
				MeanExecTime:  10.0,
			},
		},
	}

	c := query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	ctx := context.Background()
	if err := c.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Verify stats were stored
	stats, err := storage.GetQueryStats(ctx, snapshotID)
	if err != nil {
		t.Fatalf("GetQueryStats failed: %v", err)
	}

	if len(stats) != 2 {
		t.Errorf("expected 2 query stats, got %d", len(stats))
	}
}

func TestQueryStatsCollectorError(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	mock := &mockPGClient{
		version:          "PostgreSQL 14.5",
		getStatementsErr: errors.New("connection refused"),
	}

	c := query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	ctx := context.Background()
	err := c.Collect(ctx, snapshotID)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestTableStatsCollector(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
		statTables: []models.TableStat{
			{
				SchemaName: "public",
				RelName:    "users",
				SeqScan:    100,
				IdxScan:    500,
				NLiveTup:   10000,
				NDeadTup:   100,
				TableSize:  1024 * 1024,
				IndexSize:  512 * 1024,
			},
		},
	}

	c := resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	ctx := context.Background()
	if err := c.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Verify stats were stored
	stats, err := storage.GetTableStats(ctx, snapshotID)
	if err != nil {
		t.Fatalf("GetTableStats failed: %v", err)
	}

	if len(stats) != 1 {
		t.Errorf("expected 1 table stat, got %d", len(stats))
	}

	if stats[0].RelName != "users" {
		t.Errorf("expected relname 'users', got '%s'", stats[0].RelName)
	}
}

func TestIndexStatsCollector(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
		statIndexes: []models.IndexStat{
			{
				SchemaName:   "public",
				RelName:      "users",
				IndexRelName: "users_pkey",
				IdxScan:      1000,
				IndexSize:    256 * 1024,
				IsUnique:     true,
				IsPrimary:    true,
			},
			{
				SchemaName:   "public",
				RelName:      "users",
				IndexRelName: "idx_users_email",
				IdxScan:      500,
				IndexSize:    128 * 1024,
				IsUnique:     true,
				IsPrimary:    false,
			},
		},
	}

	c := resource.NewIndexStatsCollector(resource.IndexStatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	ctx := context.Background()
	if err := c.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Verify stats were stored
	stats, err := storage.GetIndexStats(ctx, snapshotID)
	if err != nil {
		t.Fatalf("GetIndexStats failed: %v", err)
	}

	if len(stats) != 2 {
		t.Errorf("expected 2 index stats, got %d", len(stats))
	}
}

func TestDatabaseStatsCollector(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
		databaseStats: &models.DatabaseStats{
			DatabaseName:  "testdb",
			BlksHit:       100000,
			BlksRead:      1000,
			CacheHitRatio: 99.01,
		},
	}

	c := resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	ctx := context.Background()
	if err := c.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Verify cache hit ratio was stored in snapshot
	snap, err := storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatalf("GetSnapshotByID failed: %v", err)
	}

	if snap.CacheHitRatio == nil {
		t.Error("expected cache hit ratio to be set")
	} else if *snap.CacheHitRatio != 99.01 {
		t.Errorf("expected cache hit ratio 99.01, got %f", *snap.CacheHitRatio)
	}
}

func TestBloatCollector(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
		tableBloat: []models.BloatInfo{
			{
				SchemaName:   "public",
				RelName:      "large_table",
				NDeadTup:     5000,
				NLiveTup:     100000,
				BloatPercent: 5.0,
			},
			{
				SchemaName:   "public",
				RelName:      "small_table",
				NDeadTup:     50, // Below threshold
				NLiveTup:     1000,
				BloatPercent: 5.0,
			},
		},
	}

	c := schema.NewBloatCollector(schema.BloatCollectorConfig{
		PGClient:      mock,
		Storage:       storage,
		InstanceID:    instanceID,
		MinDeadTuples: 1000, // Only large_table should be stored
	})

	ctx := context.Background()
	if err := c.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	// Verify only significant bloat was stored
	stats, err := storage.GetBloatStats(ctx, snapshotID)
	if err != nil {
		t.Fatalf("GetBloatStats failed: %v", err)
	}

	if len(stats) != 1 {
		t.Errorf("expected 1 bloat stat, got %d", len(stats))
	}

	if len(stats) > 0 && stats[0].RelName != "large_table" {
		t.Errorf("expected relname 'large_table', got '%s'", stats[0].RelName)
	}
}

func TestCoordinator(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
		statStatements: []models.QueryStat{
			{QueryID: 1, Query: "SELECT 1", Calls: 10, TotalExecTime: 100},
		},
		statTables: []models.TableStat{
			{SchemaName: "public", RelName: "test", SeqScan: 5},
		},
		databaseStats: &models.DatabaseStats{CacheHitRatio: 98.5},
	}

	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	// Register collectors
	coord.RegisterCollectors(
		query.NewStatsCollector(query.StatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
	)

	ctx := context.Background()
	result, err := coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if result.SnapshotID == 0 {
		t.Error("expected snapshot ID to be set")
	}

	if result.HasErrors() {
		t.Errorf("expected no errors, got: %v", result.Error())
	}

	// Verify data was collected
	stats, err := storage.GetQueryStats(ctx, result.SnapshotID)
	if err != nil {
		t.Fatalf("GetQueryStats failed: %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 query stat, got %d", len(stats))
	}
}

func TestCoordinatorPartialFailure(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)

	mock := &mockPGClient{
		version:          "PostgreSQL 14.5",
		getStatementsErr: errors.New("query stats error"),
		statTables: []models.TableStat{
			{SchemaName: "public", RelName: "test", SeqScan: 5},
		},
	}

	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	// Register collectors - one will fail
	coord.RegisterCollectors(
		query.NewStatsCollector(query.StatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
			PGClient:   mock,
			Storage:    storage,
			InstanceID: instanceID,
		}),
	)

	ctx := context.Background()
	result, err := coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect failed with unexpected error: %v", err)
	}

	// Should have partial errors
	if !result.HasErrors() {
		t.Error("expected errors from partial failure")
	}

	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}

	if _, ok := result.Errors["query_stats"]; !ok {
		t.Error("expected error from query_stats collector")
	}

	// Table stats should still have been collected
	stats, err := storage.GetTableStats(ctx, result.SnapshotID)
	if err != nil {
		t.Fatalf("GetTableStats failed: %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 table stat, got %d", len(stats))
	}
}

func TestCoordinatorContextCancellation(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)

	mock := &mockPGClient{
		version: "PostgreSQL 14.5",
	}

	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	coord.RegisterCollector(query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := coord.Collect(ctx)

	// When context is cancelled before any work starts, the coordinator
	// may fail when trying to create a snapshot, which is expected behavior
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled error, got: %v", err)
		}
		return // Test passes - context cancellation detected early
	}

	// If we got a result, it should have context cancellation errors
	if !result.HasErrors() {
		t.Error("expected context cancellation error")
	}
}

func TestStatsResetDetection(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)

	resetTime1 := time.Now().Add(-1 * time.Hour)
	resetTime2 := time.Now() // Stats were reset

	mock := &mockPGClient{
		version:        "PostgreSQL 14.5",
		statsResetTime: &resetTime1,
		statStatements: []models.QueryStat{
			{QueryID: 1, Query: "SELECT 1", Calls: 100},
		},
	}

	c := query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
	})

	ctx := context.Background()
	snapshotID := setupTestSnapshot(t, storage, instanceID)

	// First collection
	if err := c.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("First Collect failed: %v", err)
	}

	// Simulate stats reset
	mock.statsResetTime = &resetTime2
	mock.statStatements = []models.QueryStat{
		{QueryID: 1, Query: "SELECT 1", Calls: 10}, // Lower count after reset
	}

	snapshotID2 := setupTestSnapshot(t, storage, instanceID)

	// Second collection - should detect reset (logged as warning)
	if err := c.Collect(ctx, snapshotID2); err != nil {
		t.Fatalf("Second Collect failed: %v", err)
	}

	// Verify stats were still collected
	stats, err := storage.GetQueryStats(ctx, snapshotID2)
	if err != nil {
		t.Fatalf("GetQueryStats failed: %v", err)
	}
	if len(stats) != 1 {
		t.Errorf("expected 1 query stat, got %d", len(stats))
	}
}

func TestCollectorIntervals(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		expected time.Duration
	}{
		{
			name:     "query_stats",
			interval: 0, // Default
			expected: 1 * time.Minute,
		},
		{
			name:     "table_stats",
			interval: 0, // Default
			expected: 5 * time.Minute,
		},
		{
			name:     "index_stats",
			interval: 0, // Default
			expected: 5 * time.Minute,
		},
		{
			name:     "database_stats",
			interval: 0, // Default
			expected: 1 * time.Minute,
		},
		{
			name:     "bloat",
			interval: 0, // Default
			expected: 1 * time.Hour,
		},
	}

	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	mock := &mockPGClient{version: "PostgreSQL 14.5"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c collector.Collector
			switch tt.name {
			case "query_stats":
				c = query.NewStatsCollector(query.StatsCollectorConfig{
					PGClient:   mock,
					Storage:    storage,
					InstanceID: instanceID,
					Interval:   tt.interval,
				})
			case "table_stats":
				c = resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
					PGClient:   mock,
					Storage:    storage,
					InstanceID: instanceID,
					Interval:   tt.interval,
				})
			case "index_stats":
				c = resource.NewIndexStatsCollector(resource.IndexStatsCollectorConfig{
					PGClient:   mock,
					Storage:    storage,
					InstanceID: instanceID,
					Interval:   tt.interval,
				})
			case "database_stats":
				c = resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
					PGClient:   mock,
					Storage:    storage,
					InstanceID: instanceID,
					Interval:   tt.interval,
				})
			case "bloat":
				c = schema.NewBloatCollector(schema.BloatCollectorConfig{
					PGClient:   mock,
					Storage:    storage,
					InstanceID: instanceID,
					Interval:   tt.interval,
				})
			}

			if c.Interval() != tt.expected {
				t.Errorf("expected interval %v, got %v", tt.expected, c.Interval())
			}
		})
	}
}

func TestCollectorCustomInterval(t *testing.T) {
	storage := setupTestStorage(t)
	instanceID := setupTestInstance(t, storage)
	mock := &mockPGClient{version: "PostgreSQL 14.5"}

	customInterval := 30 * time.Second
	c := query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   mock,
		Storage:    storage,
		InstanceID: instanceID,
		Interval:   customInterval,
	})

	if c.Interval() != customInterval {
		t.Errorf("expected custom interval %v, got %v", customInterval, c.Interval())
	}
}
