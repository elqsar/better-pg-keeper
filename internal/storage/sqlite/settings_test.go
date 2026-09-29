package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestServerSettingsRoundTrip(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, err := storage.CreateInstance(ctx, &models.Instance{Name: "s", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}
	snapID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instID, CapturedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	if got, err := storage.GetServerSettings(ctx, snapID); err != nil || got != nil {
		t.Fatalf("empty snapshot: got %+v, %v", got, err)
	}

	reset := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	settings := &models.ServerSettings{
		Settings: map[string]models.Setting{
			"autovacuum":     {Name: "autovacuum", Setting: "off", Context: "sighup"},
			"shared_buffers": {Name: "shared_buffers", Setting: "16384", Unit: "8kB", Context: "postmaster", PendingRestart: true},
		},
		StatStatements:     &models.StatStatementsUsage{Entries: 4990, Max: 5000, Dealloc: 7, StatsReset: &reset},
		AutovacuumDisabled: []models.TableSize{{SchemaName: "public", RelName: "events", TotalBytes: 2 << 30}},
		Unavailable:        map[string]string{"stat_statements": "permission denied"},
	}
	if err := storage.SaveServerSettings(ctx, snapID, settings); err != nil {
		t.Fatal(err)
	}
	// Saving again replaces rather than failing on the primary key.
	settings.StatStatements.Dealloc = 9
	if err := storage.SaveServerSettings(ctx, snapID, settings); err != nil {
		t.Fatal(err)
	}

	got, err := storage.GetServerSettings(ctx, snapID)
	if err != nil {
		t.Fatal(err)
	}
	sb, _ := got.Get("shared_buffers")
	if b, _ := sb.Bytes(); b != 128<<20 || !sb.PendingRestart {
		t.Errorf("shared_buffers = %+v", sb)
	}
	if u := got.StatStatements; u == nil || u.Dealloc != 9 || u.StatsReset == nil || !u.StatsReset.Equal(reset) {
		t.Errorf("stat statements = %+v", got.StatStatements)
	}
	if len(got.AutovacuumDisabled) != 1 || got.AutovacuumDisabled[0].TotalBytes != 2<<30 {
		t.Errorf("autovacuum disabled = %+v", got.AutovacuumDisabled)
	}
	if got.Unavailable["stat_statements"] != "permission denied" {
		t.Errorf("unavailable = %+v", got.Unavailable)
	}

	// Snapshot deletion takes the settings with it.
	if _, err := storage.PurgeOldSnapshots(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if got, err := storage.GetServerSettings(ctx, snapID); err != nil || got != nil {
		t.Errorf("after purge: got %+v, %v", got, err)
	}
}
