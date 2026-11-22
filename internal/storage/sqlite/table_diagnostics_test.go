package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestTableDiagnosticsRoundTripAndLegacyNulls(t *testing.T) {
	s := setupTestStorage(t)
	ctx := context.Background()
	instanceID, err := s.CreateInstance(ctx, &models.Instance{Name: "maintenance", Host: "localhost", Port: 5432, Database: "db"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := s.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now(), PGVersion: "17"})
	if err != nil {
		t.Fatal(err)
	}
	vacuum, analyze, scale := 2000.0, 1000.0, .02
	modified := time.Now().UTC().Round(0)
	table := models.TableStat{SchemaName: "public", RelName: "events", HeapBytes: 1 << 30,
		RelTuples: 10000, NModSinceAnalyze: 200, AutovacuumCount: 3,
		LastAutoanalyze: &modified, VacuumTrigger: &vacuum,
		AnalyzeTrigger: &analyze, VacuumScaleFactor: &scale, AutovacuumDisabled: true}
	if err := s.SaveTableStats(ctx, snapshotID, []models.TableStat{table}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCurrentTableStats(ctx, instanceID, []models.TableStat{table}); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() ([]models.TableStat, error){
		func() ([]models.TableStat, error) { return s.GetTableStats(ctx, snapshotID) },
		func() ([]models.TableStat, error) { return s.GetCurrentTableStats(ctx, instanceID) },
	} {
		got, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].HeapBytes != table.HeapBytes || got[0].VacuumTrigger == nil || *got[0].VacuumTrigger != vacuum || got[0].LastAutoanalyze == nil || !got[0].AutovacuumDisabled {
			t.Fatalf("table round trip = %+v", got)
		}
	}
	dead, free := int64(10<<20), int64(20<<20)
	b := models.BloatInfo{SchemaName: "public", RelName: "events", HeapBytes: 1 << 30,
		DeadTupleBytes: &dead, FreeBytes: &free, MeasurementSource: "pgstattuple_approx", MeasuredAt: &modified}
	if err := s.SaveBloatStats(ctx, snapshotID, []models.BloatInfo{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCurrentBloatStats(ctx, instanceID, []models.BloatInfo{b}); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() ([]models.BloatInfo, error){
		func() ([]models.BloatInfo, error) { return s.GetBloatStats(ctx, snapshotID) },
		func() ([]models.BloatInfo, error) { return s.GetCurrentBloatStats(ctx, instanceID) },
	} {
		got, err := read()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].DeadTupleBytes == nil || *got[0].DeadTupleBytes != dead || got[0].FreeBytes == nil || *got[0].FreeBytes != free || got[0].MeasurementSource != "pgstattuple_approx" {
			t.Fatalf("space round trip = %+v", got)
		}
	}
	// A row written before this feature has no fabricated measurements.
	_, err = s.writeDB.ExecContext(ctx, `INSERT INTO bloat_stats(snapshot_id,schemaname,relname,n_dead_tup,n_live_tup,bloat_percent) VALUES (?,?,?,?,?,?)`, snapshotID, "public", "legacy", 100, 1000, 10)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBloatStats(ctx, snapshotID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range got {
		if row.RelName == "legacy" && (row.DeadTupleBytes != nil || row.FreeBytes != nil || row.MeasurementSource != "") {
			t.Fatalf("legacy row invented a measurement: %+v", row)
		}
	}
}
