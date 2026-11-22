package postgres

import (
	"testing"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestApplyAutovacuumThresholds(t *testing.T) {
	d := autovacuumValues{vacuumBase: 50, vacuumScale: .2, analyzeBase: 50, analyzeScale: .1}
	stat := models.TableStat{RelTuples: 100000}
	applyAutovacuumThresholds(&stat, d, []string{"autovacuum_vacuum_threshold=1000", "autovacuum_vacuum_scale_factor=0.02", "autovacuum_analyze_scale_factor=0.05"})
	if stat.VacuumTrigger == nil || *stat.VacuumTrigger != 3000 {
		t.Fatalf("vacuum trigger = %v, want 3000", stat.VacuumTrigger)
	}
	if stat.AnalyzeTrigger == nil || *stat.AnalyzeTrigger != 5050 {
		t.Fatalf("analyze trigger = %v, want 5050", stat.AnalyzeTrigger)
	}
	if stat.VacuumScaleFactor == nil || *stat.VacuumScaleFactor != .02 {
		t.Fatalf("vacuum scale = %v, want .02", stat.VacuumScaleFactor)
	}
	unknown := models.TableStat{RelTuples: -1}
	applyAutovacuumThresholds(&unknown, d, nil)
	if unknown.VacuumTrigger != nil || unknown.AnalyzeTrigger != nil {
		t.Fatal("unknown reltuples must not produce a trigger")
	}

	if stat.AutovacuumDisabled {
		t.Fatal("autovacuum reported disabled without a reason")
	}
}

func TestApplyAutovacuumThresholdsMaxCap(t *testing.T) {
	// PostgreSQL 18 caps the vacuum trigger; the analyze trigger has no cap.
	d := autovacuumValues{vacuumBase: 50, vacuumScale: .2, analyzeBase: 50, analyzeScale: .1, vacuumMax: 100_000_000, vacuumCapped: true}
	large := models.TableStat{RelTuples: 2e9}
	applyAutovacuumThresholds(&large, d, nil)
	if *large.VacuumTrigger != 100_000_000 {
		t.Fatalf("capped vacuum trigger = %v, want 1e8", *large.VacuumTrigger)
	}
	if *large.AnalyzeTrigger != 50+.1*2e9 {
		t.Fatalf("analyze trigger = %v must not be capped", *large.AnalyzeTrigger)
	}
	small := models.TableStat{RelTuples: 1000}
	applyAutovacuumThresholds(&small, d, nil)
	if *small.VacuumTrigger != 250 {
		t.Fatalf("uncapped vacuum trigger = %v, want 250", *small.VacuumTrigger)
	}
	table := models.TableStat{RelTuples: 2e9}
	applyAutovacuumThresholds(&table, d, []string{"autovacuum_vacuum_max_threshold=5000000"})
	if *table.VacuumTrigger != 5_000_000 {
		t.Fatalf("table cap = %v, want 5e6", *table.VacuumTrigger)
	}
	off := models.TableStat{RelTuples: 2e9}
	applyAutovacuumThresholds(&off, d, []string{"autovacuum_vacuum_max_threshold=-1"})
	if *off.VacuumTrigger != 50+.2*2e9 {
		t.Fatalf("table without cap = %v", *off.VacuumTrigger)
	}
	d.vacuumMax, d.vacuumCapped = 0, false // before PostgreSQL 18
	old := models.TableStat{RelTuples: 2e9}
	applyAutovacuumThresholds(&old, d, nil)
	if *old.VacuumTrigger != 50+.2*2e9 {
		t.Fatalf("pre-18 vacuum trigger = %v", *old.VacuumTrigger)
	}
}

func TestApplyAutovacuumThresholdsDisabled(t *testing.T) {
	d := autovacuumValues{vacuumBase: 50, vacuumScale: .2, analyzeBase: 50, analyzeScale: .1}
	for _, value := range []string{"false", "off", "OFF", "no", "0", "f", "n"} {
		stat := models.TableStat{RelTuples: 1000}
		applyAutovacuumThresholds(&stat, d, []string{"autovacuum_enabled=" + value})
		if !stat.AutovacuumDisabled || stat.VacuumTrigger == nil {
			t.Fatalf("autovacuum_enabled=%s: disabled=%v trigger=%v", value, stat.AutovacuumDisabled, stat.VacuumTrigger)
		}
	}
	on := models.TableStat{RelTuples: 1000}
	applyAutovacuumThresholds(&on, d, []string{"autovacuum_enabled=true", "toast.autovacuum_enabled=false"})
	if on.AutovacuumDisabled {
		t.Fatal("enabled table, or a TOAST-only option, reported disabled")
	}
	d.disabled = true // autovacuum or track_counts off server-wide
	global := models.TableStat{RelTuples: 1000}
	applyAutovacuumThresholds(&global, d, []string{"autovacuum_enabled=true"})
	if !global.AutovacuumDisabled {
		t.Fatal("server-wide autovacuum off not reported")
	}
}
