package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestQueryHistorySurvivesSnapshotRetention(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "history", Host: "localhost", Port: 5432, Database: "test"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := storage.CreateSnapshot(ctx, &models.Snapshot{
		InstanceID: instanceID, CapturedAt: time.Now().Add(-48 * time.Hour), PGVersion: "16",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, calls := range []int64{1, 2} {
		if err := storage.SaveQueryStats(ctx, snapshotID, []models.QueryStat{{
			QueryID: 42, Query: "SELECT 1", Calls: calls, TotalExecTime: float64(calls), MeanExecTime: 1,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	current, err := storage.GetQueryStats(ctx, snapshotID)
	if err != nil || len(current) != 1 || current[0].Calls != 2 {
		t.Fatalf("expected latest snapshot row, got %+v: %v", current, err)
	}
	history, err := storage.GetQueryHistory(ctx, instanceID, 42, time.Time{}, time.Time{}, 10, 0)
	if err != nil || len(history) != 2 || history[0].Calls != 2 || history[1].Calls != 1 {
		t.Fatalf("expected both history samples newest first, got %+v: %v", history, err)
	}
	page, err := storage.GetQueryHistory(ctx, instanceID, 42, time.Time{}, time.Time{}, 1, 1)
	if err != nil || len(page) != 1 || page[0].Calls != 1 {
		t.Fatalf("expected older page, got %+v: %v", page, err)
	}
	zone := time.FixedZone("offset", 2*60*60)
	localFrom := history[0].SampledAt.Add(-time.Second).In(zone)
	localTo := history[0].SampledAt.Add(time.Second).In(zone)
	bounded, err := storage.GetQueryHistory(ctx, instanceID, 42, localFrom, localTo, 10, 0)
	if err != nil || len(bounded) != 2 {
		t.Fatalf("timezone-adjusted bounds lost samples, got %+v: %v", bounded, err)
	}
	from := history[0].SampledAt.Add(time.Second)
	filtered, err := storage.GetQueryHistory(ctx, instanceID, 42, from, time.Time{}, 10, 0)
	if err != nil || len(filtered) != 0 {
		t.Fatalf("expected bounded history to be empty, got %+v: %v", filtered, err)
	}
	deleted, err := storage.PurgeOldSnapshots(ctx, 24*time.Hour)
	if err != nil || deleted != 1 {
		t.Fatalf("expected old snapshot to be purged, got %d: %v", deleted, err)
	}
	history, err = storage.GetQueryHistory(ctx, instanceID, 42, time.Time{}, time.Time{}, 10, 0)
	if err != nil || len(history) != 2 {
		t.Fatalf("history must survive snapshot purge, got %+v: %v", history, err)
	}
	deleted, err = storage.PurgeOldQueryHistory(ctx, -time.Hour)
	if err != nil || deleted != 2 {
		t.Fatalf("expected history purge to remove two rows, got %d: %v", deleted, err)
	}
}

func TestFailedCollectorRunInvalidatesGroupedSnapshot(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "coverage", Host: "localhost", Port: 5432, Database: "test"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now(), PGVersion: "16"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.RecordCollectorRun(ctx, snapshotID, "query_stats", models.CollectorStatusSuccess, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if err := storage.RecordCollectorRun(ctx, snapshotID, "query_stats", models.CollectorStatusError, time.Now(), "read failed"); err != nil {
		t.Fatal(err)
	}
	covered, err := storage.GetLatestSnapshotWithCollector(ctx, instanceID, "query_stats", time.Time{})
	if err != nil || covered != nil {
		t.Fatalf("failed rerun must invalidate coverage, got %+v: %v", covered, err)
	}
}

func TestCoverageLookupIncludesAnchorSnapshot(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "anchor", Host: "localhost", Port: 5432, Database: "test"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now(), PGVersion: "16"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.RecordCollectorRun(ctx, snapshotID, "query_stats", models.CollectorStatusSuccess, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	anchor, err := storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	covered, err := storage.GetLatestSnapshotWithCollector(ctx, instanceID, "query_stats", anchor.CapturedAt)
	if err != nil || covered == nil || covered.ID != snapshotID {
		t.Fatalf("anchor snapshot must be included, got %+v: %v", covered, err)
	}
}

func TestTimestampMigrationRepairsExistingRows(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "upgrade", Host: "localhost", Port: 5432, Database: "test"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now(), PGVersion: "16"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.RecordCollectorRun(ctx, snapshotID, "query_stats", models.CollectorStatusSuccess, time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveQueryStats(ctx, snapshotID, []models.QueryStat{{QueryID: 5, Query: "SELECT 1", Calls: 1}}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE snapshots SET captured_at = captured_at || ' m=+1'",
		"UPDATE snapshot_collectors SET collected_at = collected_at || ' m=+1'",
		"UPDATE query_history SET sampled_at = sampled_at || ' m=+1'",
		"DELETE FROM _migrations WHERE version = 15",
	} {
		if _, err := storage.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, storage.DB()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT instr(captured_at, ' m=') FROM snapshots",
		"SELECT instr(collected_at, ' m=') FROM snapshot_collectors",
		"SELECT instr(sampled_at, ' m=') FROM query_history",
	} {
		var suffix int
		if err := storage.DB().QueryRowContext(ctx, query).Scan(&suffix); err != nil || suffix != 0 {
			t.Fatalf("monotonic suffix remains for %s: %d: %v", query, suffix, err)
		}
	}
	anchor, err := storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	covered, err := storage.GetLatestSnapshotWithCollector(ctx, instanceID, "query_stats", anchor.CapturedAt)
	if err != nil || covered == nil || covered.ID != snapshotID {
		t.Fatalf("coverage lookup after migration: %+v: %v", covered, err)
	}
}

func TestQueryHistoryMigrationBackfillsLegacySnapshots(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "backfill", Host: "localhost", Port: 5432, Database: "test"})
	if err != nil {
		t.Fatal(err)
	}
	capturedAt := time.Now().Add(-time.Hour).Round(0)
	snapshotID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: capturedAt, PGVersion: "16"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveQueryStats(ctx, snapshotID, []models.QueryStat{{QueryID: 9, Query: "SELECT 9", Calls: 3}}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP TABLE query_history",
		"UPDATE snapshots SET captured_at = captured_at || ' m=+123'",
		"DELETE FROM _migrations WHERE version IN (14, 15)",
	} {
		if _, err := storage.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, storage.DB()); err != nil {
		t.Fatal(err)
	}
	from := capturedAt.Add(-time.Second)
	to := capturedAt.Add(time.Second)
	history, err := storage.GetQueryHistory(ctx, instanceID, 9, from, to, 10, 0)
	if err != nil || len(history) != 1 || history[0].Calls != 3 || !history[0].SampledAt.Equal(capturedAt) {
		t.Fatalf("legacy query sample was not backfilled: %+v: %v", history, err)
	}
}
