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
	issues := maintenanceBacklog(now, before)
	if len(issues) != 2 || issues[0].IssueType != TableIssueVacuumBacklog || issues[1].IssueType != TableIssueAnalyzeBacklog {
		t.Fatalf("issues = %+v, want both vacuum and analyze backlogs", issues)
	}
	now.AutovacuumCount++
	issues = maintenanceBacklog(now, before)
	if len(issues) != 1 || issues[0].IssueType != TableIssueAnalyzeBacklog {
		t.Fatalf("issues = %+v, want only analyze backlog after vacuum advanced", issues)
	}
	now.AutoanalyzeCount++
	if issues = maintenanceBacklog(now, before); len(issues) != 0 {
		t.Fatalf("issues = %+v despite maintenance counters advancing", issues)
	}
}

func TestMaintenanceBacklogAndTuningCoexist(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}

	// Table with sustained vacuum backlog, sustained analyze backlog, AND high trigger tuning issue.
	stat := models.TableStat{
		SchemaName: "public", RelName: "huge_events", HeapBytes: 2 << 30,
		RelTuples: 1e6, NLiveTup: 1e6,
		NDeadTup: 500000, NModSinceAnalyze: 500000,
		VacuumTrigger: ptrFloat(200050), AnalyzeTrigger: ptrFloat(100050),
	}
	store.tableStats[1] = []models.TableStat{stat}
	store.tableStats[2] = []models.TableStat{stat}

	result := &AnalysisResult{
		InstanceID: 1,
		Coverage: map[string]DomainCoverage{
			DomainTableStats: {Present: true, SnapshotID: 2, CapturedAt: now},
		},
	}
	if err := NewMainAnalyzer(store, nil).analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, iss := range result.MaintenanceIssues {
		seen[iss.IssueType] = true
	}
	for _, want := range []string{TableIssueVacuumBacklog, TableIssueAnalyzeBacklog, TableIssueAutovacuumTuning} {
		if !seen[want] {
			t.Errorf("missing expected maintenance issue %q in %+v", want, result.MaintenanceIssues)
		}
	}
}

func TestStaleBloatDoesNotSuppressMaintenanceIssues(t *testing.T) {
	ctx := context.Background()
	store := newMockStorage()
	now := time.Now()

	cfg := DefaultConfig()
	cfg.CollectorIntervals = map[string]time.Duration{
		DomainBloat:      time.Hour,
		DomainTableStats: 5 * time.Minute,
	}
	// Bloat snapshot is 6h old (beyond 3x 1h staleness budget).
	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-6 * time.Hour)}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	store.snapshots[3] = &models.Snapshot{ID: 3, InstanceID: 1, CapturedAt: now}

	store.bloatStats[1] = []models.BloatInfo{{
		SchemaName: "public", RelName: "orders", NDeadTup: 500000, NLiveTup: 1000000, BloatPercent: 50, HeapBytes: 1 << 30,
	}}
	stat := models.TableStat{
		SchemaName: "public", RelName: "orders", HeapBytes: 1 << 30,
		NLiveTup: 1000000, NDeadTup: 30000,
		VacuumTrigger: ptrFloat(10000), AnalyzeTrigger: ptrFloat(10000),
		LastAutovacuum: &now, LastAutoanalyze: &now,
	}
	store.tableStats[2] = []models.TableStat{stat}
	store.tableStats[3] = []models.TableStat{stat}

	res, err := NewMainAnalyzer(store, cfg).Analyze(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.DomainsUsable(DomainBloat) {
		t.Fatalf("expected DomainBloat to be stale, got %+v", res.Coverage[DomainBloat])
	}
	for _, iss := range res.TableIssues {
		if iss.IssueType == TableIssueHighBloat {
			t.Fatalf("stale bloat snapshot leaked into TableIssues: %+v", iss)
		}
	}
	if len(res.MaintenanceIssues) != 1 || res.MaintenanceIssues[0].IssueType != TableIssueVacuumBacklog {
		t.Fatalf("expected fresh MaintenanceIssues not suppressed by stale bloat, got %+v", res.MaintenanceIssues)
	}
}

func TestSlowQueryAndQueryWindowStatsReset(t *testing.T) {
	ctx := context.Background()
	store := newMockStorage()
	now := time.Now().UTC().Truncate(time.Second)
	resetBefore := now.Add(-48 * time.Hour)
	resetAfter := now.Add(-30 * time.Minute)

	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-25 * time.Hour), StatsReset: &resetBefore}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now, StatsReset: &resetAfter}
	store.queryStats[1] = []models.QueryStat{{QueryID: 1, Query: "SELECT 1", Calls: 100, TotalExecTime: 500, MeanExecTime: 5}}
	store.queryStats[2] = []models.QueryStat{{QueryID: 1, Query: "SELECT 1", Calls: 20, TotalExecTime: 40000, MeanExecTime: 2000}}
	// If delta were called despite reset, it would return an empty delta list.
	deltaCalled := false
	store.onDelta = func(from, to int64) { deltaCalled = true }

	sq, err := NewSlowQueryAnalyzer(store, DefaultConfig()).Analyze(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deltaCalled {
		t.Error("SlowQueryAnalyzer called GetQueryStatsDelta across a stats_reset")
	}
	if len(sq) != 1 || sq[0].Window != 0 || sq[0].MeanExecTime != 2000 {
		t.Fatalf("expected cumulative post-reset slow query with Window=0, got %+v", sq)
	}

	qw, err := RecentQueryStats(ctx, store, 1, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if qw != nil {
		t.Fatalf("expected nil QueryWindow across stats_reset, got %+v", qw)
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
	// pg_stat_statements_reset() (Snapshot.StatsReset) must NOT invalidate
	// pg_stat_user_tables maintenance baselines.
	reset := now.Add(-time.Hour)
	store.snapshots[2].StatsReset = &reset
	result.MaintenanceIssues = nil
	if err := a.analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if !result.MaintenanceBaselineObserved || len(result.MaintenanceIssues) != 1 {
		t.Fatalf("pg_stat_statements reset falsely invalidated table baseline: %+v", result)
	}

	// A real pg_stat_reset() lowers maintenance counters on the table itself,
	// suppressing the backlog and marking the table unobserved.
	baseWithCounts := stat
	baseWithCounts.AutovacuumCount = 5
	store.tableStats[1] = []models.TableStat{baseWithCounts}
	afterReset := stat
	afterReset.AutovacuumCount = 0
	store.tableStats[2] = []models.TableStat{afterReset}
	result.MaintenanceIssues = nil
	if err := a.analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if len(result.MaintenanceIssues) != 0 || len(result.MaintenanceUnobservedTargets) != 1 || result.MaintenanceUnobservedTargets[0] != "vacuum:public.large" {
		t.Fatalf("table counter reset should suppress backlog and mark vacuum unobserved: issues=%+v unobserved=%+v",
			result.MaintenanceIssues, result.MaintenanceUnobservedTargets)
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

func TestMaintenanceNotSuppressedByStaleAnalyzeAndSkipsSmallTablesUnobserved(t *testing.T) {
	store := newMockStorage()
	now := time.Now()
	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}

	big := models.TableStat{
		SchemaName: "public", RelName: "orders", HeapBytes: 2 << 30,
		RelTuples: 1e6, NLiveTup: 1e6, NDeadTup: 30000,
		VacuumTrigger: ptrFloat(10000), AnalyzeTrigger: ptrFloat(10000),
	}
	tinyNew := models.TableStat{
		SchemaName: "public", RelName: "order", HeapBytes: 1 << 20, // < 64 MiB
		NLiveTup: 100, VacuumTrigger: ptrFloat(100), AnalyzeTrigger: ptrFloat(100),
	}
	store.tableStats[1] = []models.TableStat{big}
	store.tableStats[2] = []models.TableStat{big, tinyNew}

	result := &AnalysisResult{
		InstanceID: 1,
		TableIssues: []TableIssue{
			{SchemaName: "public", TableName: "orders", IssueType: TableIssueStaleAnalyze},
		},
		Coverage: map[string]DomainCoverage{
			DomainTableStats: {Present: true, SnapshotID: 2, CapturedAt: now},
		},
	}
	if err := NewMainAnalyzer(store, nil).analyzeMaintenance(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if len(result.MaintenanceIssues) != 1 || result.MaintenanceIssues[0].IssueType != TableIssueVacuumBacklog {
		t.Fatalf("StaleAnalyze suppressed maintenance backlog: %+v", result.MaintenanceIssues)
	}
	if len(result.MaintenanceUnobservedTargets) != 0 {
		t.Fatalf("small table emitted unobserved targets: %+v", result.MaintenanceUnobservedTargets)
	}
}

func TestAnalyzeTransactionsWindowedDeltas(t *testing.T) {
	ctx := context.Background()
	store := newMockStorage()
	now := time.Now()
	store.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	store.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	store.extendedStats[1] = &models.ExtendedDatabaseStats{
		XactCommit: 1000, XactRollback: 10, TempFiles: 5, TempBytes: 5 << 30, Deadlocks: 3,
	}
	store.extendedStats[2] = &models.ExtendedDatabaseStats{
		XactCommit: 1500, XactRollback: 12, TempFiles: 7, TempBytes: 6 << 30, Deadlocks: 4,
	}

	a := NewMainAnalyzer(store, nil)

	// Without a baseline snapshot >= 1h old, lifetime totals must not be emitted.
	noBase, err := a.analyzeTransactions(ctx, 1, 1, store.snapshots[1].CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if noBase.BaselineObserved || noBase.Deadlocks != 0 || noBase.TempBytes != 0 {
		t.Fatalf("without baseline = %+v, want unobserved zeroes", noBase)
	}

	// With a 2h baseline, deltas are computed cleanly.
	tx, err := a.analyzeTransactions(ctx, 1, 2, store.snapshots[2].CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !tx.BaselineObserved || tx.Deadlocks != 1 || tx.TempBytes != 1<<30 || tx.TempFiles != 2 || tx.XactCommit != 500 || tx.XactRollback != 2 {
		t.Fatalf("windowed delta = %+v", tx)
	}
	if tx.Window != 2*time.Hour {
		t.Fatalf("window = %v, want 2h", tx.Window)
	}

	// After pg_stat_reset() (counters drop below baseline), use post-reset totals.
	store.extendedStats[2] = &models.ExtendedDatabaseStats{
		XactCommit: 200, XactRollback: 1, TempFiles: 1, TempBytes: 2 << 30, Deadlocks: 2,
	}
	resetTx, err := a.analyzeTransactions(ctx, 1, 2, store.snapshots[2].CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !resetTx.BaselineObserved || resetTx.Deadlocks != 2 || resetTx.TempBytes != 2<<30 || resetTx.XactCommit != 200 {
		t.Fatalf("post-reset delta = %+v", resetTx)
	}
}
