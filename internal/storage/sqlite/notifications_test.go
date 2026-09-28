package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestNotificationState(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, err := storage.CreateInstance(ctx, &models.Instance{Name: "n", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}

	notified := time.Now().Add(-time.Hour)
	st := &models.NotificationState{
		InstanceID: instID, RuleID: "slow_query", TargetObject: "queryid:1",
		Severity: models.SeverityCritical, NotifiedAt: notified,
	}
	if err := storage.UpsertNotificationState(ctx, st); err != nil {
		t.Fatal(err)
	}

	cleared := time.Now()
	st.ClearedAt = &cleared
	st.Severity = models.SeverityWarning
	if err := storage.UpsertNotificationState(ctx, st); err != nil {
		t.Fatal(err)
	}

	states, err := storage.ListNotificationStates(ctx, instID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("got %d states, want 1", len(states))
	}
	got := states[0]
	if got.Severity != models.SeverityWarning || !got.NotifiedAt.Equal(notified.Round(0)) ||
		got.ClearedAt == nil || !got.ClearedAt.Equal(cleared.Round(0)) {
		t.Errorf("state = %+v", got)
	}

	if err := storage.DeleteNotificationState(ctx, instID, "slow_query", "queryid:1"); err != nil {
		t.Fatal(err)
	}
	if states, _ := storage.ListNotificationStates(ctx, instID); len(states) != 0 {
		t.Errorf("state not deleted: %+v", states)
	}
}

func TestNotifierTime(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, _ := storage.CreateInstance(ctx, &models.Instance{Name: "n", Host: "h", Port: 5432, Database: "d"})

	if _, ok, err := storage.GetNotifierTime(ctx, instID, "k"); ok || err != nil {
		t.Fatalf("unset key: ok=%v err=%v", ok, err)
	}
	want := time.Date(2026, 9, 28, 9, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	for range 2 { // second write exercises the update path
		if err := storage.SetNotifierTime(ctx, instID, "k", want); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := storage.GetNotifierTime(ctx, instID, "k")
	if !ok || err != nil || !got.Equal(want) {
		t.Errorf("GetNotifierTime = %v, %v, %v; want %v", got, ok, err, want)
	}
}

func TestGetEarliestSnapshotWithCollector(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, _ := storage.CreateInstance(ctx, &models.Instance{Name: "n", Host: "h", Port: 5432, Database: "d"})

	base := time.Now().Add(-10 * time.Hour)
	var ids []int64
	for i := range 3 {
		at := base.Add(time.Duration(i) * time.Hour)
		id, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instID, CapturedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		status := models.CollectorStatusSuccess
		if i == 1 {
			status = models.CollectorStatusError
		}
		if err := storage.RecordCollectorRun(ctx, id, "query_stats", status, at, ""); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// The snapshot at base+1h failed, so the earliest usable one after base+30m is base+2h.
	snap, err := storage.GetEarliestSnapshotWithCollector(ctx, instID, "query_stats", base.Add(30*time.Minute))
	if err != nil || snap == nil || snap.ID != ids[2] {
		t.Fatalf("got %+v, %v; want snapshot %d", snap, err, ids[2])
	}
	snap, err = storage.GetEarliestSnapshotWithCollector(ctx, instID, "query_stats", base.Add(3*time.Hour))
	if err != nil || snap != nil {
		t.Fatalf("got %+v, %v; want none", snap, err)
	}
}
