package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestOutageRiskRoundTrip(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, err := storage.CreateInstance(ctx, &models.Instance{Name: "r", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}
	snapID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instID, CapturedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	if got, err := storage.GetOutageRisk(ctx, snapID); err != nil || got != nil {
		t.Fatalf("empty snapshot: got %+v, %v", got, err)
	}

	retained := int64(5 << 30)
	risk := &models.OutageRisk{
		Database:     "d",
		FreezeMaxAge: 200_000_000,
		Databases:    []models.DatabaseAge{{Name: "d", XIDAge: 123, MXIDAge: 4}},
		Slots:        []models.ReplicationSlot{{Name: "s1", Type: "physical", RetainedBytes: &retained}},
		Sequences:    []models.SequenceUsage{{SchemaName: "public", SequenceName: "t_id_seq", LastValue: 9, MaxValue: 10, UsedFraction: 0.9}},
		Unavailable:  map[string]string{"replication": "permission denied"},
	}
	if err := storage.SaveOutageRisk(ctx, snapID, risk); err != nil {
		t.Fatal(err)
	}
	// Saving again replaces rather than failing on the primary key.
	risk.Databases[0].XIDAge = 456
	if err := storage.SaveOutageRisk(ctx, snapID, risk); err != nil {
		t.Fatal(err)
	}

	got, err := storage.GetOutageRisk(ctx, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Databases[0].XIDAge != 456 || got.FreezeMaxAge != 200_000_000 ||
		got.Slots[0].RetainedBytes == nil || *got.Slots[0].RetainedBytes != retained ||
		got.Sequences[0].UsedFraction != 0.9 || got.Unavailable["replication"] == "" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestSizeHistory(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, _ := storage.CreateInstance(ctx, &models.Instance{Name: "r", Host: "h", Port: 5432, Database: "d"})

	hour := time.Now().Truncate(time.Hour)
	// Two samples in the same hour collapse into the later one.
	for _, s := range []struct {
		at    time.Time
		bytes int64
	}{
		{hour.Add(-100 * 24 * time.Hour), 50},
		{hour.Add(-2 * time.Hour), 100},
		{hour.Add(-time.Hour + time.Minute), 200},
		{hour.Add(-time.Hour + 30*time.Minute), 250},
	} {
		if err := storage.RecordSize(ctx, instID, s.at, s.bytes); err != nil {
			t.Fatal(err)
		}
	}

	samples, err := storage.GetSizeHistory(ctx, instID, hour.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].ClusterBytes != 100 || samples[1].ClusterBytes != 250 {
		t.Fatalf("samples = %+v, want [100 250] oldest first", samples)
	}

	purged, err := storage.PurgeOldSizeHistory(ctx, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if purged != 1 {
		t.Errorf("purged %d samples, want 1", purged)
	}
}

func TestConnectionPeak(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, _ := storage.CreateInstance(ctx, &models.Instance{Name: "r", Host: "h", Port: 5432, Database: "d"})

	now := time.Now()
	for _, s := range []struct {
		ago   time.Duration
		total int
	}{
		{30 * time.Hour, 99}, // outside the window
		{3 * time.Hour, 90},
		{time.Hour, 40},
	} {
		id, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instID, CapturedAt: now.Add(-s.ago)})
		if err != nil {
			t.Fatal(err)
		}
		if err := storage.SaveConnectionActivity(ctx, id, &models.ConnectionActivity{
			TotalConnections: s.total, IdleCount: s.total / 2, MaxConnections: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}

	peak, err := storage.GetConnectionPeak(ctx, instID, now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if peak == nil || peak.TotalConnections != 90 || peak.IdleCount != 45 || peak.MaxConnections != 100 {
		t.Fatalf("peak = %+v, want 90/100", peak)
	}

	if peak, _ := storage.GetConnectionPeak(ctx, instID, now.Add(-10*time.Minute), now); peak != nil {
		t.Errorf("empty window returned %+v", peak)
	}
}
