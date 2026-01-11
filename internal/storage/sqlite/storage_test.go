package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/pganalyzer/internal/models"
)

func setupTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	storage, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	t.Cleanup(func() {
		storage.Close()
	})

	return storage
}

func TestNewStorage(t *testing.T) {
	t.Run("creates database and applies migrations", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		storage, err := NewStorage(dbPath)
		if err != nil {
			t.Fatalf("NewStorage failed: %v", err)
		}
		defer storage.Close()

		// Verify database file exists
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Error("Database file was not created")
		}

		// Verify migrations were applied
		ctx := context.Background()
		status, err := GetMigrationStatus(ctx, storage.DB())
		if err != nil {
			t.Fatalf("GetMigrationStatus failed: %v", err)
		}

		if len(status) != 10 {
			t.Errorf("Expected 10 migrations applied, got %d", len(status))
		}
	})

	t.Run("creates directory if not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "nested", "dir", "test.db")

		storage, err := NewStorage(dbPath)
		if err != nil {
			t.Fatalf("NewStorage failed: %v", err)
		}
		defer storage.Close()

		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Error("Database file was not created in nested directory")
		}
	})
}

func TestInstanceOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	t.Run("CreateInstance", func(t *testing.T) {
		inst := &models.Instance{
			Name:     "test-instance",
			Host:     "localhost",
			Port:     5432,
			Database: "testdb",
		}

		id, err := storage.CreateInstance(ctx, inst)
		if err != nil {
			t.Fatalf("CreateInstance failed: %v", err)
		}
		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}
	})

	t.Run("GetInstance", func(t *testing.T) {
		inst := &models.Instance{
			Name:     "get-test",
			Host:     "localhost",
			Port:     5433,
			Database: "getdb",
		}
		id, _ := storage.CreateInstance(ctx, inst)

		retrieved, err := storage.GetInstance(ctx, id)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		if retrieved == nil {
			t.Fatal("GetInstance returned nil")
		}
		if retrieved.Name != inst.Name {
			t.Errorf("Name mismatch: got %s, want %s", retrieved.Name, inst.Name)
		}
		if retrieved.Host != inst.Host {
			t.Errorf("Host mismatch: got %s, want %s", retrieved.Host, inst.Host)
		}
	})

	t.Run("GetInstance not found returns nil", func(t *testing.T) {
		retrieved, err := storage.GetInstance(ctx, 99999)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		if retrieved != nil {
			t.Error("Expected nil for non-existent instance")
		}
	})

	t.Run("GetInstanceByName", func(t *testing.T) {
		inst := &models.Instance{
			Name:     "named-instance",
			Host:     "localhost",
			Port:     5434,
			Database: "nameddb",
		}
		storage.CreateInstance(ctx, inst)

		retrieved, err := storage.GetInstanceByName(ctx, "named-instance")
		if err != nil {
			t.Fatalf("GetInstanceByName failed: %v", err)
		}
		if retrieved == nil {
			t.Fatal("GetInstanceByName returned nil")
		}
		if retrieved.Name != "named-instance" {
			t.Errorf("Name mismatch: got %s", retrieved.Name)
		}
	})

	t.Run("GetOrCreateInstance creates new", func(t *testing.T) {
		inst := &models.Instance{
			Name:     "getorcreate-new",
			Host:     "newhost",
			Port:     5435,
			Database: "newdb",
		}

		id, err := storage.GetOrCreateInstance(ctx, inst)
		if err != nil {
			t.Fatalf("GetOrCreateInstance failed: %v", err)
		}
		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}
	})

	t.Run("GetOrCreateInstance returns existing", func(t *testing.T) {
		inst := &models.Instance{
			Name:     "existing",
			Host:     "existhost",
			Port:     5436,
			Database: "existdb",
		}

		id1, _ := storage.CreateInstance(ctx, inst)
		id2, err := storage.GetOrCreateInstance(ctx, inst)
		if err != nil {
			t.Fatalf("GetOrCreateInstance failed: %v", err)
		}
		if id1 != id2 {
			t.Errorf("Expected same ID %d, got %d", id1, id2)
		}
	})

	t.Run("ListInstances", func(t *testing.T) {
		instances, err := storage.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(instances) == 0 {
			t.Error("Expected at least one instance")
		}
	})
}

func TestSnapshotOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	// Create instance first
	inst := &models.Instance{
		Name:     "snapshot-test",
		Host:     "localhost",
		Port:     5432,
		Database: "snapdb",
	}
	instanceID, _ := storage.CreateInstance(ctx, inst)

	t.Run("CreateSnapshot", func(t *testing.T) {
		statsReset := time.Now().Add(-24 * time.Hour)
		snap := &models.Snapshot{
			InstanceID: instanceID,
			CapturedAt: time.Now(),
			PGVersion:  "15.1",
			StatsReset: &statsReset,
		}

		id, err := storage.CreateSnapshot(ctx, snap)
		if err != nil {
			t.Fatalf("CreateSnapshot failed: %v", err)
		}
		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}
	})

	t.Run("GetSnapshotByID", func(t *testing.T) {
		snap := &models.Snapshot{
			InstanceID: instanceID,
			CapturedAt: time.Now(),
			PGVersion:  "15.2",
		}
		id, _ := storage.CreateSnapshot(ctx, snap)

		retrieved, err := storage.GetSnapshotByID(ctx, id)
		if err != nil {
			t.Fatalf("GetSnapshotByID failed: %v", err)
		}
		if retrieved == nil {
			t.Fatal("GetSnapshotByID returned nil")
		}
		if retrieved.PGVersion != "15.2" {
			t.Errorf("PGVersion mismatch: got %s", retrieved.PGVersion)
		}
	})

	t.Run("GetLatestSnapshot", func(t *testing.T) {
		// Create older snapshot
		storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instanceID,
			CapturedAt: time.Now().Add(-1 * time.Hour),
			PGVersion:  "old",
		})

		// Create newer snapshot
		storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instanceID,
			CapturedAt: time.Now(),
			PGVersion:  "latest",
		})

		latest, err := storage.GetLatestSnapshot(ctx, instanceID)
		if err != nil {
			t.Fatalf("GetLatestSnapshot failed: %v", err)
		}
		if latest == nil {
			t.Fatal("GetLatestSnapshot returned nil")
		}
		if latest.PGVersion != "latest" {
			t.Errorf("Expected latest version, got %s", latest.PGVersion)
		}
	})

	t.Run("ListSnapshots", func(t *testing.T) {
		snapshots, err := storage.ListSnapshots(ctx, instanceID, 10)
		if err != nil {
			t.Fatalf("ListSnapshots failed: %v", err)
		}
		if len(snapshots) == 0 {
			t.Error("Expected at least one snapshot")
		}
	})
}

func TestQueryStatsOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	// Setup instance and snapshot
	instID, _ := storage.CreateInstance(ctx, &models.Instance{
		Name: "query-test", Host: "localhost", Port: 5432, Database: "querydb",
	})
	snapID, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
		InstanceID: instID, CapturedAt: time.Now(), PGVersion: "15",
	})

	t.Run("SaveQueryStats", func(t *testing.T) {
		stats := []models.QueryStat{
			{
				QueryID:        12345,
				Query:          "SELECT * FROM users",
				Calls:          100,
				TotalExecTime:  500.5,
				MeanExecTime:   5.005,
				MinExecTime:    1.0,
				MaxExecTime:    50.0,
				Rows:           1000,
				SharedBlksHit:  5000,
				SharedBlksRead: 100,
			},
			{
				QueryID:       67890,
				Query:         "SELECT * FROM orders",
				Calls:         50,
				TotalExecTime: 250.0,
				MeanExecTime:  5.0,
			},
		}

		err := storage.SaveQueryStats(ctx, snapID, stats)
		if err != nil {
			t.Fatalf("SaveQueryStats failed: %v", err)
		}
	})

	t.Run("GetQueryStats", func(t *testing.T) {
		stats, err := storage.GetQueryStats(ctx, snapID)
		if err != nil {
			t.Fatalf("GetQueryStats failed: %v", err)
		}
		if len(stats) != 2 {
			t.Errorf("Expected 2 stats, got %d", len(stats))
		}
	})

	t.Run("GetQueryStatsDelta", func(t *testing.T) {
		// Create another snapshot with incremented stats
		snapID2, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instID, CapturedAt: time.Now().Add(time.Hour), PGVersion: "15",
		})

		stats2 := []models.QueryStat{
			{
				QueryID:        12345,
				Query:          "SELECT * FROM users",
				Calls:          200, // +100
				TotalExecTime:  1000.5,
				MeanExecTime:   5.0025,
				Rows:           2000,
				SharedBlksHit:  10000,
				SharedBlksRead: 200,
			},
		}
		storage.SaveQueryStats(ctx, snapID2, stats2)

		deltas, err := storage.GetQueryStatsDelta(ctx, snapID, snapID2)
		if err != nil {
			t.Fatalf("GetQueryStatsDelta failed: %v", err)
		}

		if len(deltas) == 0 {
			t.Fatal("Expected at least one delta")
		}

		// Find the delta for query 12345
		var found bool
		for _, d := range deltas {
			if d.QueryID == 12345 {
				found = true
				if d.DeltaCalls != 100 {
					t.Errorf("Expected delta_calls=100, got %d", d.DeltaCalls)
				}
				break
			}
		}
		if !found {
			t.Error("Delta for query 12345 not found")
		}
	})

	t.Run("GetQueryStatsDelta handles stats reset", func(t *testing.T) {
		snapID3, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instID, CapturedAt: time.Now().Add(2 * time.Hour), PGVersion: "15",
		})

		// Simulate stats reset (lower values than before)
		statsReset := []models.QueryStat{
			{
				QueryID:       12345,
				Query:         "SELECT * FROM users",
				Calls:         10, // Lower than previous - indicates reset
				TotalExecTime: 50.0,
				MeanExecTime:  5.0,
			},
		}
		storage.SaveQueryStats(ctx, snapID3, statsReset)

		// Create a high-value snapshot to compare against
		snapID4, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instID, CapturedAt: time.Now().Add(3 * time.Hour), PGVersion: "15",
		})
		storage.SaveQueryStats(ctx, snapID4, []models.QueryStat{
			{QueryID: 12345, Query: "SELECT * FROM users", Calls: 500, TotalExecTime: 2500.0},
		})

		// Delta from high to low should detect reset and use current values
		deltas, err := storage.GetQueryStatsDelta(ctx, snapID4, snapID3)
		if err != nil {
			t.Fatalf("GetQueryStatsDelta failed: %v", err)
		}

		for _, d := range deltas {
			if d.QueryID == 12345 {
				// When reset detected, delta equals current value
				if d.DeltaCalls != 10 {
					t.Errorf("Expected delta_calls=10 (current value on reset), got %d", d.DeltaCalls)
				}
				break
			}
		}
	})
}

func TestTableStatsOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	instID, _ := storage.CreateInstance(ctx, &models.Instance{
		Name: "table-test", Host: "localhost", Port: 5432, Database: "tabledb",
	})
	snapID, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
		InstanceID: instID, CapturedAt: time.Now(), PGVersion: "15",
	})

	t.Run("SaveTableStats and GetTableStats", func(t *testing.T) {
		lastVac := time.Now().Add(-1 * time.Hour)
		stats := []models.TableStat{
			{
				SchemaName:  "public",
				RelName:     "users",
				SeqScan:     100,
				SeqTupRead:  10000,
				IdxScan:     500,
				IdxTupFetch: 5000,
				NLiveTup:    1000,
				NDeadTup:    50,
				LastVacuum:  &lastVac,
				TableSize:   1024 * 1024,
				IndexSize:   512 * 1024,
			},
		}

		err := storage.SaveTableStats(ctx, snapID, stats)
		if err != nil {
			t.Fatalf("SaveTableStats failed: %v", err)
		}

		retrieved, err := storage.GetTableStats(ctx, snapID)
		if err != nil {
			t.Fatalf("GetTableStats failed: %v", err)
		}
		if len(retrieved) != 1 {
			t.Errorf("Expected 1 stat, got %d", len(retrieved))
		}
		if retrieved[0].RelName != "users" {
			t.Errorf("RelName mismatch: got %s", retrieved[0].RelName)
		}
	})
}

func TestIndexStatsOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	instID, _ := storage.CreateInstance(ctx, &models.Instance{
		Name: "index-test", Host: "localhost", Port: 5432, Database: "indexdb",
	})
	snapID, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
		InstanceID: instID, CapturedAt: time.Now(), PGVersion: "15",
	})

	t.Run("SaveIndexStats and GetIndexStats", func(t *testing.T) {
		stats := []models.IndexStat{
			{
				SchemaName:   "public",
				RelName:      "users",
				IndexRelName: "users_pkey",
				IdxScan:      1000,
				IdxTupRead:   1000,
				IdxTupFetch:  1000,
				IndexSize:    256 * 1024,
				IsUnique:     true,
				IsPrimary:    true,
			},
			{
				SchemaName:   "public",
				RelName:      "users",
				IndexRelName: "idx_users_email",
				IdxScan:      0, // Unused index
				IndexSize:    128 * 1024,
				IsUnique:     false,
				IsPrimary:    false,
			},
		}

		err := storage.SaveIndexStats(ctx, snapID, stats)
		if err != nil {
			t.Fatalf("SaveIndexStats failed: %v", err)
		}

		retrieved, err := storage.GetIndexStats(ctx, snapID)
		if err != nil {
			t.Fatalf("GetIndexStats failed: %v", err)
		}
		if len(retrieved) != 2 {
			t.Errorf("Expected 2 stats, got %d", len(retrieved))
		}
	})
}

func TestSuggestionOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	instID, _ := storage.CreateInstance(ctx, &models.Instance{
		Name: "suggest-test", Host: "localhost", Port: 5432, Database: "suggestdb",
	})

	t.Run("UpsertSuggestion creates new", func(t *testing.T) {
		sug := &models.Suggestion{
			InstanceID:   instID,
			RuleID:       "unused_index",
			Severity:     models.SeverityWarning,
			Title:        "Unused index detected",
			Description:  "Index idx_users_email has 0 scans",
			TargetObject: "public.users.idx_users_email",
			Metadata:     `{"days_unused": 30}`,
		}

		err := storage.UpsertSuggestion(ctx, sug)
		if err != nil {
			t.Fatalf("UpsertSuggestion failed: %v", err)
		}

		active, _ := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
		if len(active) != 1 {
			t.Errorf("Expected 1 active suggestion, got %d", len(active))
		}
	})

	t.Run("UpsertSuggestion updates existing", func(t *testing.T) {
		sug := &models.Suggestion{
			InstanceID:   instID,
			RuleID:       "unused_index",
			Severity:     models.SeverityCritical, // Changed severity
			Title:        "Unused index detected (updated)",
			Description:  "Index idx_users_email has 0 scans for 60 days",
			TargetObject: "public.users.idx_users_email",
			Metadata:     `{"days_unused": 60}`,
		}

		err := storage.UpsertSuggestion(ctx, sug)
		if err != nil {
			t.Fatalf("UpsertSuggestion update failed: %v", err)
		}

		active, _ := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
		if len(active) != 1 {
			t.Errorf("Expected still 1 active suggestion, got %d", len(active))
		}
		if active[0].Severity != models.SeverityCritical {
			t.Errorf("Severity not updated: got %s", active[0].Severity)
		}
	})

	t.Run("DismissSuggestion", func(t *testing.T) {
		active, _ := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
		if len(active) == 0 {
			t.Skip("No active suggestions to dismiss")
		}

		err := storage.DismissSuggestion(ctx, active[0].ID)
		if err != nil {
			t.Fatalf("DismissSuggestion failed: %v", err)
		}

		// Should no longer be in active list
		activeAfter, _ := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
		if len(activeAfter) != 0 {
			t.Error("Dismissed suggestion still in active list")
		}

		// But should still be retrievable by ID
		dismissed, _ := storage.GetSuggestionByID(ctx, active[0].ID)
		if dismissed.Status != models.StatusDismissed {
			t.Errorf("Status not updated: got %s", dismissed.Status)
		}
	})

	t.Run("UpsertSuggestion reactivates resolved", func(t *testing.T) {
		// Create and resolve a suggestion
		sug := &models.Suggestion{
			InstanceID:   instID,
			RuleID:       "slow_query",
			Severity:     models.SeverityWarning,
			Title:        "Slow query detected",
			Description:  "Query takes > 1000ms",
			TargetObject: "query:12345",
		}
		storage.UpsertSuggestion(ctx, sug)

		active, _ := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
		var slowQueryID int64
		for _, a := range active {
			if a.RuleID == "slow_query" {
				slowQueryID = a.ID
				break
			}
		}

		storage.ResolveSuggestion(ctx, slowQueryID)

		// Upserting again should reactivate
		storage.UpsertSuggestion(ctx, sug)

		reactivated, _ := storage.GetSuggestionByID(ctx, slowQueryID)
		if reactivated.Status != models.StatusActive {
			t.Errorf("Expected status 'active', got %s", reactivated.Status)
		}
	})

	t.Run("GetSuggestionsByStatus orders by severity", func(t *testing.T) {
		// Create suggestions with different severities
		storage.UpsertSuggestion(ctx, &models.Suggestion{
			InstanceID: instID, RuleID: "info_rule", Severity: models.SeverityInfo,
			Title: "Info", Description: "Info desc", TargetObject: "target1",
		})
		storage.UpsertSuggestion(ctx, &models.Suggestion{
			InstanceID: instID, RuleID: "critical_rule", Severity: models.SeverityCritical,
			Title: "Critical", Description: "Critical desc", TargetObject: "target2",
		})
		storage.UpsertSuggestion(ctx, &models.Suggestion{
			InstanceID: instID, RuleID: "warning_rule", Severity: models.SeverityWarning,
			Title: "Warning", Description: "Warning desc", TargetObject: "target3",
		})

		active, _ := storage.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
		if len(active) < 3 {
			t.Skip("Not enough suggestions for ordering test")
		}

		// First should be critical
		if active[0].Severity != models.SeverityCritical {
			t.Errorf("Expected first suggestion to be critical, got %s", active[0].Severity)
		}
	})
}

func TestExplainPlanOperations(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	t.Run("SaveExplainPlan and GetExplainPlan", func(t *testing.T) {
		plan := &models.ExplainPlan{
			QueryID:    12345,
			PlanText:   "Seq Scan on users  (cost=0.00..10.00 rows=1000 width=100)",
			PlanJSON:   `{"Plan": {"Node Type": "Seq Scan"}}`,
			CapturedAt: time.Now(),
		}

		id, err := storage.SaveExplainPlan(ctx, plan)
		if err != nil {
			t.Fatalf("SaveExplainPlan failed: %v", err)
		}
		if id <= 0 {
			t.Errorf("Expected positive ID, got %d", id)
		}

		retrieved, err := storage.GetExplainPlan(ctx, 12345)
		if err != nil {
			t.Fatalf("GetExplainPlan failed: %v", err)
		}
		if retrieved == nil {
			t.Fatal("GetExplainPlan returned nil")
		}
		if retrieved.PlanText != plan.PlanText {
			t.Errorf("PlanText mismatch")
		}
	})

	t.Run("GetExplainPlan returns latest", func(t *testing.T) {
		// Save older plan
		storage.SaveExplainPlan(ctx, &models.ExplainPlan{
			QueryID: 99999, PlanText: "old plan", CapturedAt: time.Now().Add(-time.Hour),
		})
		// Save newer plan
		storage.SaveExplainPlan(ctx, &models.ExplainPlan{
			QueryID: 99999, PlanText: "new plan", CapturedAt: time.Now(),
		})

		retrieved, _ := storage.GetExplainPlan(ctx, 99999)
		if retrieved.PlanText != "new plan" {
			t.Errorf("Expected latest plan, got %s", retrieved.PlanText)
		}
	})
}

func TestPurgeOldSnapshots(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	instID, _ := storage.CreateInstance(ctx, &models.Instance{
		Name: "purge-test", Host: "localhost", Port: 5432, Database: "purgedb",
	})

	// Create old snapshots
	for i := 0; i < 5; i++ {
		snapID, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instID,
			CapturedAt: time.Now().Add(-time.Duration(i+10) * 24 * time.Hour), // 10-14 days old
			PGVersion:  "15",
		})
		// Add some stats
		storage.SaveQueryStats(ctx, snapID, []models.QueryStat{
			{QueryID: int64(i), Query: "SELECT 1", Calls: 1, TotalExecTime: 1, MeanExecTime: 1},
		})
	}

	// Create new snapshots
	for i := 0; i < 3; i++ {
		storage.CreateSnapshot(ctx, &models.Snapshot{
			InstanceID: instID,
			CapturedAt: time.Now().Add(-time.Duration(i) * time.Hour), // 0-2 hours old
			PGVersion:  "15",
		})
	}

	// Purge snapshots older than 7 days
	deleted, err := storage.PurgeOldSnapshots(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeOldSnapshots failed: %v", err)
	}
	if deleted != 5 {
		t.Errorf("Expected 5 deleted, got %d", deleted)
	}

	// Verify remaining snapshots
	remaining, _ := storage.ListSnapshots(ctx, instID, 100)
	if len(remaining) != 3 {
		t.Errorf("Expected 3 remaining snapshots, got %d", len(remaining))
	}
}

func TestMigrations(t *testing.T) {
	t.Run("migrations are idempotent", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// First run
		storage1, err := NewStorage(dbPath)
		if err != nil {
			t.Fatalf("First NewStorage failed: %v", err)
		}
		storage1.Close()

		// Second run should not fail
		storage2, err := NewStorage(dbPath)
		if err != nil {
			t.Fatalf("Second NewStorage failed: %v", err)
		}
		defer storage2.Close()
	})

	t.Run("rollback removes migration", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		storage, _ := NewStorage(dbPath)
		ctx := context.Background()

		statusBefore, _ := GetMigrationStatus(ctx, storage.DB())
		beforeCount := len(statusBefore)

		err := Rollback(ctx, storage.DB())
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}

		statusAfter, _ := GetMigrationStatus(ctx, storage.DB())
		if len(statusAfter) != beforeCount-1 {
			t.Errorf("Expected %d migrations after rollback, got %d", beforeCount-1, len(statusAfter))
		}

		storage.Close()
	})
}

func TestCascadeDeletes(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()

	instID, _ := storage.CreateInstance(ctx, &models.Instance{
		Name: "cascade-test", Host: "localhost", Port: 5432, Database: "cascadedb",
	})

	snapID, _ := storage.CreateSnapshot(ctx, &models.Snapshot{
		InstanceID: instID, CapturedAt: time.Now().Add(-30 * 24 * time.Hour), PGVersion: "15",
	})

	// Add related data
	storage.SaveQueryStats(ctx, snapID, []models.QueryStat{
		{QueryID: 1, Query: "SELECT 1", Calls: 1, TotalExecTime: 1, MeanExecTime: 1},
	})
	storage.SaveTableStats(ctx, snapID, []models.TableStat{
		{SchemaName: "public", RelName: "test"},
	})
	storage.SaveIndexStats(ctx, snapID, []models.IndexStat{
		{SchemaName: "public", RelName: "test", IndexRelName: "test_pkey"},
	})

	// Purge should cascade delete all related stats
	deleted, err := storage.PurgeOldSnapshots(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeOldSnapshots failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1 deleted, got %d", deleted)
	}

	// Verify stats are gone
	queryStats, _ := storage.GetQueryStats(ctx, snapID)
	if len(queryStats) != 0 {
		t.Error("Query stats should be cascade deleted")
	}

	tableStats, _ := storage.GetTableStats(ctx, snapID)
	if len(tableStats) != 0 {
		t.Error("Table stats should be cascade deleted")
	}

	indexStats, _ := storage.GetIndexStats(ctx, snapID)
	if len(indexStats) != 0 {
		t.Error("Index stats should be cascade deleted")
	}
}
