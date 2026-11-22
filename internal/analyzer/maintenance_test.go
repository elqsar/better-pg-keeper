package analyzer

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func ptrFloat(v float64) *float64 { return &v }

func TestMaintenanceBacklogRequiresSustainedCounters(t *testing.T) {
	before := models.TableStat{SchemaName: "public", RelName: "events", HeapBytes: 1 << 30,
		NDeadTup: 25000, NModSinceAnalyze: 25000,
		VacuumTrigger: ptrFloat(10000), AnalyzeTrigger: ptrFloat(10000),
		AutovacuumCount: 5, AutoanalyzeCount: 5}
	now := before
	now.NDeadTup = 30000
	if issue := maintenanceBacklog(now, before); issue == nil || issue.IssueType != TableIssueVacuumBacklog {
		t.Fatalf("issue = %+v, want vacuum backlog", issue)
	}
	now.AutovacuumCount++
	if issue := maintenanceBacklog(now, before); issue == nil || issue.IssueType != TableIssueAnalyzeBacklog {
		t.Fatalf("issue = %+v, want analyze backlog after vacuum advanced", issue)
	}
	now.AutoanalyzeCount++
	if issue := maintenanceBacklog(now, before); issue != nil {
		t.Fatalf("issue = %+v despite maintenance counters advancing", issue)
	}
}

func TestMaintenanceTuningAndBaseline(t *testing.T) {
	stat := models.TableStat{SchemaName: "public", RelName: "large", HeapBytes: 2 << 30,
		RelTuples: 1e6, NLiveTup: 1e6, NDeadTup: 100000, VacuumTrigger: ptrFloat(200050)}
	if issue := maintenanceTuning(stat); issue == nil || issue.DeadTupleBytes == nil {
		t.Fatalf("tuning issue = %+v", issue)
	}
	stat.NDeadTup = 100
	if issue := maintenanceTuning(stat); issue != nil {
		t.Fatalf("small footprint: %+v", issue)
	}

	store := newMockStorage()
	now := time.Now()
	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	stat.NDeadTup = 30000
	stat.VacuumTrigger = ptrFloat(10000)
	stat.AnalyzeTrigger = ptrFloat(10000)
	store.tableStats[1] = []models.TableStat{stat}
	store.tableStats[2] = []models.TableStat{stat}
	a := NewMainAnalyzer(store, nil)
	result := &AnalysisResult{InstanceID: 1, Coverage: map[string]DomainCoverage{
		DomainTableStats: {Present: true, SnapshotID: 2, CapturedAt: now},
	}}
	if err := a.analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if !result.MaintenanceBaselineObserved || len(result.MaintenanceIssues) != 1 || result.MaintenanceIssues[0].IssueType != TableIssueVacuumBacklog {
		t.Fatalf("result = %+v", result)
	}
	newTable := stat
	newTable.RelName = "new_table"
	store.tableStats[2] = append(store.tableStats[2], newTable)
	result.MaintenanceIssues = nil
	if err := a.analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if len(result.MaintenanceUnobservedTargets) != 2 {
		t.Fatalf("missing per-table baseline protection: %+v", result.MaintenanceUnobservedTargets)
	}
	reset := now.Add(-time.Hour)
	store.snapshots[2].StatsReset = &reset
	result.MaintenanceIssues = nil
	result.MaintenanceBaselineObserved = false
	if err := a.analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if result.MaintenanceBaselineObserved || len(result.MaintenanceIssues) != 0 {
		t.Fatalf("reset should suppress backlog: %+v", result)
	}
}

func TestMeasuredHeapOverridesRowRatioAndAutoanalyzeCounts(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	dead := int64(1 << 20)
	store.tableStats[1] = []models.TableStat{{SchemaName: "public", RelName: "events",
		NLiveTup: 100000, NDeadTup: 50000, LastAutoanalyze: &now}}
	store.bloatStats[2] = []models.BloatInfo{{SchemaName: "public", RelName: "events",
		NLiveTup: 100000, NDeadTup: 50000, BloatPercent: 50,
		HeapBytes: 100 << 20, DeadTupleBytes: &dead, MeasurementSource: "pgstattuple_approx"}}
	issues, err := NewTableAnalyzer(store, nil).AnalyzeSnapshots(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range issues {
		if issue.IssueType == TableIssueHighBloat || issue.IssueType == TableIssueStaleAnalyze {
			t.Fatalf("false finding despite measurement/autoanalyze: %+v", issue)
		}
	}
}

func TestMaintenanceSkipsTablesWithAutovacuumDisabled(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	// A sustained backlog that would be reported if autovacuum were on, on a
	// table large enough for trigger tuning advice too.
	stat := models.TableStat{SchemaName: "public", RelName: "frozen", HeapBytes: 2 << 30,
		RelTuples: 1e6, NLiveTup: 1e6, NDeadTup: 500000, NModSinceAnalyze: 500000,
		VacuumTrigger: ptrFloat(200050), AnalyzeTrigger: ptrFloat(100050)}
	store.tableStats[1] = []models.TableStat{stat}
	stat.AutovacuumDisabled = true
	store.tableStats[2] = []models.TableStat{stat}
	result := &AnalysisResult{InstanceID: 1, Coverage: map[string]DomainCoverage{
		DomainTableStats: {Present: true, SnapshotID: 2, CapturedAt: now},
	}}
	if err := NewMainAnalyzer(store, nil).analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if len(result.MaintenanceIssues) != 0 {
		t.Fatalf("issues for a table with autovacuum off: %+v", result.MaintenanceIssues)
	}
	// Observed, so an earlier backlog finding resolves in favour of the
	// configuration rule's finding.
	if !result.MaintenanceBaselineObserved || len(result.MaintenanceUnobservedTargets) != 0 {
		t.Fatalf("baseline=%v unobserved=%v", result.MaintenanceBaselineObserved, result.MaintenanceUnobservedTargets)
	}
}
