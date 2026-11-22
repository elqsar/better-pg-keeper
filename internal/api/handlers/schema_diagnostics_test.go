package handlers

import (
	"testing"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestSchemaDiagnosticResponseFields(t *testing.T) {
	vacuum, analyze := 2200.0, 1200.0
	table := tableStatToResponse(models.TableStat{SchemaName: "public", RelName: "events",
		HeapBytes: 1 << 30, VacuumTrigger: &vacuum, AnalyzeTrigger: &analyze,
		NModSinceAnalyze: 1500, AutovacuumCount: 2, AutovacuumDisabled: true})
	if table.HeapBytes != 1<<30 || table.VacuumTrigger == nil || *table.VacuumTrigger != vacuum || table.ModifiedSinceAnalyze != 1500 || !table.AutovacuumDisabled {
		t.Fatalf("table response = %+v", table)
	}
	dead := int64(100 << 20)
	b := models.BloatInfo{HeapBytes: 1 << 30, DeadTupleBytes: &dead, MeasurementSource: "stats_estimate"}
	pct := deadHeapPercent(b)
	if pct == nil || *pct < 9 || *pct > 10 {
		t.Fatalf("dead heap percent = %v", pct)
	}
	if p := deadHeapPercent(models.BloatInfo{}); p != nil {
		t.Fatalf("unknown percent = %v", p)
	}
}
