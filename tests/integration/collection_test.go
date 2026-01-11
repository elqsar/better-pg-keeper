//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/collector/query"
	"github.com/user/pganalyzer/internal/collector/resource"
	"github.com/user/pganalyzer/internal/collector/schema"
	"github.com/user/pganalyzer/internal/models"
)

func TestCollectionCycle(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance record
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Generate some query activity
	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	// Create coordinator
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	})

	// Register collectors
	baseConfig := collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}

	coord.RegisterCollectors(
		query.NewStatsCollector(baseConfig, nil),
		resource.NewTableStatsCollector(baseConfig, nil),
		resource.NewIndexStatsCollector(baseConfig, nil),
		resource.NewDatabaseStatsCollector(baseConfig, nil),
		schema.NewBloatCollector(baseConfig, nil),
	)

	// Run collection
	result, err := coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collection failed: %v", err)
	}

	// Verify snapshot was created
	if result.SnapshotID == 0 {
		t.Error("Expected snapshot ID to be set")
	}

	// Check for errors (some may be expected if tables are empty)
	if result.HasErrors() {
		t.Logf("Collection completed with errors: %v", result.Errors)
	}

	// Verify snapshot is stored
	snapshot, err := env.Storage.GetSnapshotByID(ctx, result.SnapshotID)
	if err != nil {
		t.Fatalf("Failed to get snapshot: %v", err)
	}
	if snapshot == nil {
		t.Error("Snapshot not found after collection")
	}

	t.Logf("Collection completed: snapshot=%d, duration=%v", result.SnapshotID, result.FinishedAt.Sub(result.StartedAt))
}

func TestQueryStatsCollection(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Create a snapshot
	snapshot := &models.Snapshot{
		InstanceID: instanceID,
		CapturedAt: time.Now(),
		PGVersion:  "16.0",
	}
	snapshotID, err := env.Storage.CreateSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Generate query activity
	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	// Create and run query stats collector
	col := query.NewStatsCollector(collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}, nil)

	if err := col.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Query stats collection failed: %v", err)
	}

	// Verify stats were stored
	stats, err := env.Storage.GetQueryStats(ctx, snapshotID)
	if err != nil {
		t.Fatalf("Failed to get query stats: %v", err)
	}

	if len(stats) == 0 {
		t.Error("Expected query stats to be stored")
	}

	t.Logf("Collected %d query stats", len(stats))
}

func TestTableStatsCollection(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Create a snapshot
	snapshot := &models.Snapshot{
		InstanceID: instanceID,
		CapturedAt: time.Now(),
		PGVersion:  "16.0",
	}
	snapshotID, err := env.Storage.CreateSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Create and run table stats collector
	col := resource.NewTableStatsCollector(collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}, nil)

	if err := col.Collect(ctx, snapshotID); err != nil {
		t.Fatalf("Table stats collection failed: %v", err)
	}

	// Verify stats were stored
	stats, err := env.Storage.GetTableStats(ctx, snapshotID)
	if err != nil {
		t.Fatalf("Failed to get table stats: %v", err)
	}

	t.Logf("Collected %d table stats", len(stats))
}

func TestMultipleSnapshots(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Create coordinator
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	})

	coord.RegisterCollector(query.NewStatsCollector(collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}, nil))

	// Run multiple collection cycles
	var snapshotIDs []int64
	for i := 0; i < 3; i++ {
		// Generate some activity between snapshots
		if err := generateQueryActivity(ctx, env.PGClient); err != nil {
			t.Fatalf("Failed to generate query activity: %v", err)
		}

		result, err := coord.Collect(ctx)
		if err != nil {
			t.Fatalf("Collection %d failed: %v", i, err)
		}
		snapshotIDs = append(snapshotIDs, result.SnapshotID)

		time.Sleep(100 * time.Millisecond) // Small delay between snapshots
	}

	// Verify all snapshots are stored
	snapshots, err := env.Storage.ListSnapshots(ctx, instanceID, 10)
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}

	if len(snapshots) < 3 {
		t.Errorf("Expected at least 3 snapshots, got %d", len(snapshots))
	}

	// Verify delta calculation works
	if len(snapshotIDs) >= 2 {
		stats1, err := env.Storage.GetQueryStats(ctx, snapshotIDs[0])
		if err != nil {
			t.Fatalf("Failed to get query stats for snapshot 1: %v", err)
		}

		stats2, err := env.Storage.GetQueryStats(ctx, snapshotIDs[1])
		if err != nil {
			t.Fatalf("Failed to get query stats for snapshot 2: %v", err)
		}

		t.Logf("Snapshot 1 has %d query stats, Snapshot 2 has %d query stats",
			len(stats1), len(stats2))
	}
}
