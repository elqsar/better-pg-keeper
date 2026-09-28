package plans

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// fakePlanner returns a seq-scan plan on public.orders for every query and
// records calls.
type fakePlanner struct {
	hypo      bool
	indexes   []models.IndexKeys
	rows      int64
	planRows  float64
	explained []string
	validated []string
	usesIndex bool
}

func (f *fakePlanner) ExplainGeneric(ctx context.Context, query string) (*models.ExplainPlan, error) {
	f.explained = append(f.explained, query)
	if strings.Contains(query, "broken") {
		return nil, fmt.Errorf("permission denied for table secrets")
	}
	return &models.ExplainPlan{PlanJSON: fmt.Sprintf(`[{"Plan": {"Node Type": "Seq Scan", "Relation Name": "orders",
		"Schema": "public", "Alias": "o", "Filter": "((o.status = $1) AND (o.created_at > $2))",
		"Plan Rows": %g, "Total Cost": 4500}}]`, f.planRows)}, nil
}

func (f *fakePlanner) HypoPGAvailable(ctx context.Context) (bool, error) { return f.hypo, nil }

func (f *fakePlanner) ValidateIndex(ctx context.Context, query, ddl string) (*models.IndexValidation, error) {
	f.validated = append(f.validated, ddl)
	return &models.IndexValidation{CostBefore: 4500, CostAfter: 12, UsesIndex: f.usesIndex}, nil
}

func (f *fakePlanner) GetTableIndexInfo(ctx context.Context, schema, table string, columns []string) (*models.TableIndexInfo, error) {
	return &models.TableIndexInfo{EstimatedRows: f.rows, AvgKeyWidth: 12, Indexes: f.indexes}, nil
}

type fixture struct {
	storage  *sqlite.SQLiteStorage
	instance int64
	planner  *fakePlanner
	coll     *Collector
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	storage, err := sqlite.NewStorage(t.TempDir() + "/plans.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	ctx := context.Background()
	instance, err := storage.CreateInstance(ctx, &models.Instance{Name: "p", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}

	f := &fixture{storage: storage, instance: instance, now: time.Now(),
		planner: &fakePlanner{rows: 1_000_000, planRows: 30}}
	f.coll = NewCollector(Config{Planner: f.planner, Storage: storage, InstanceID: instance, MaxQueries: 2})
	f.coll.now = func() time.Time { return f.now }

	// A query_stats snapshot with lifetime totals.
	snap := f.snapshot(t, "query_stats", f.now.Add(-time.Minute))
	if err := storage.SaveQueryStats(ctx, snap, []models.QueryStat{
		{QueryID: 1, Query: "SELECT * FROM orders o WHERE status = $1 AND created_at > $2", Calls: 10, TotalExecTime: 6000},
		{QueryID: 2, Query: "UPDATE orders SET note = $1 WHERE status = $2 AND created_at > $3", Calls: 10, TotalExecTime: 3000},
		{QueryID: 3, Query: "SELECT broken", Calls: 10, TotalExecTime: 500},
		{QueryID: 4, Query: "VACUUM orders", Calls: 1, TotalExecTime: 500},
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

// snapshot creates a snapshot carrying the given collector's data.
func (f *fixture) snapshot(t *testing.T, collector string, at time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := f.storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: f.instance, CapturedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.storage.RecordCollectorRun(ctx, id, collector, models.CollectorStatusSuccess, at, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) collect(t *testing.T) *models.QueryPlanReport {
	t.Helper()
	ctx := context.Background()
	id, err := f.storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: f.instance, CapturedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.coll.Collect(ctx, id); err != nil {
		t.Fatal(err)
	}
	// Like the coordinator, record the run only after the collector finished.
	if err := f.storage.RecordCollectorRun(ctx, id, CollectorName, models.CollectorStatusSuccess, f.now, ""); err != nil {
		t.Fatal(err)
	}
	report, err := f.storage.GetQueryPlans(ctx, id)
	if err != nil || report == nil {
		t.Fatalf("report = %v, %v", report, err)
	}
	return report
}

func TestCollectProposesIndexes(t *testing.T) {
	f := newFixture(t)
	report := f.collect(t)

	// Top 2 explainable queries by time; VACUUM is never planned.
	if len(report.Queries) != 2 || report.Queries[0].QueryID != 1 || report.Queries[1].QueryID != 2 {
		t.Fatalf("queries = %+v", report.Queries)
	}
	q := report.Queries[0]
	if share := q.TimeShare; share < 0.59 || share > 0.61 {
		t.Errorf("time share = %v, want 0.6 of all 10s", share)
	}
	if len(q.SeqScans) != 1 {
		t.Fatalf("seq scans = %+v", q.SeqScans)
	}
	s := q.SeqScans[0]
	if !reflect.DeepEqual(s.Columns, []string{"status", "created_at"}) || s.SkipReason != "" || s.EstimatedRows != 1_000_000 || s.EstimatedBytes != 28_000_000 {
		t.Errorf("scan = %+v", s)
	}
	if s.Validation != nil || report.HypoPG {
		t.Error("no hypopg: nothing should be validated")
	}

	// The plan is kept for the query page.
	if plan, err := f.storage.GetExplainPlan(context.Background(), 1); err != nil || plan == nil {
		t.Errorf("plan not saved: %v", err)
	}
}

func TestCollectReusesPlansAndRechecksIndexes(t *testing.T) {
	f := newFixture(t)
	f.collect(t)
	if len(f.planner.explained) != 2 {
		t.Fatalf("first run explained %d queries", len(f.planner.explained))
	}

	// An hour later nothing is re-planned, but the index now exists.
	f.now = f.now.Add(time.Hour)
	f.planner.indexes = []models.IndexKeys{{Name: "idx_orders_status_created_at", Columns: []string{"status", "created_at", "id"}}}
	report := f.collect(t)
	if len(f.planner.explained) != 2 {
		t.Errorf("plans should be reused within a day, explained %d", len(f.planner.explained))
	}
	s := report.Queries[0].SeqScans[0]
	if !strings.Contains(s.SkipReason, "idx_orders_status_created_at already covers") {
		t.Errorf("existing index should retire the proposal: %+v", s)
	}

	// After a day the queries are planned again.
	f.now = f.now.Add(25 * time.Hour)
	f.collect(t)
	if len(f.planner.explained) != 4 {
		t.Errorf("stale plans should be re-planned, explained %d", len(f.planner.explained))
	}
}

func TestCollectSkipsUnhelpfulIndexes(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(p *fakePlanner)
		reason string
	}{
		{"small table", func(p *fakePlanner) { p.rows = 500 }, "the table is small"},
		{"unselective filter", func(p *fakePlanner) { p.planRows = 400_000 }, "matches ~40% of the table"},
		{"hypopg says unused", func(p *fakePlanner) { p.hypo = true }, "would not use this index"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f.planner)
			s := f.collect(t).Queries[0].SeqScans[0]
			if !strings.Contains(s.SkipReason, tt.reason) {
				t.Errorf("skip reason = %q, want %q", s.SkipReason, tt.reason)
			}
		})
	}
}

func TestCollectValidatesWithHypoPG(t *testing.T) {
	f := newFixture(t)
	f.planner.hypo = true
	f.planner.usesIndex = true
	report := f.collect(t)
	s := report.Queries[0].SeqScans[0]
	if !report.HypoPG || s.Validation == nil || !s.Validation.UsesIndex || s.SkipReason != "" {
		t.Fatalf("scan = %+v", s)
	}
	if want := `CREATE INDEX ON "public"."orders" ("status", "created_at")`; f.planner.validated[0] != want {
		t.Errorf("validated %q, want %q", f.planner.validated[0], want)
	}
}

func TestCollectRecordsPlanningErrors(t *testing.T) {
	f := newFixture(t)
	f.coll.maxQueries = 10
	report := f.collect(t)
	var found bool
	for _, q := range report.Queries {
		if q.QueryID == 3 {
			found = strings.Contains(q.Error, "permission denied")
		}
		if q.QueryID == 4 {
			t.Error("VACUUM must not be planned")
		}
	}
	if !found {
		t.Errorf("planning error not recorded: %+v", report.Queries)
	}
}

func TestCollectWaitsForQueryStats(t *testing.T) {
	storage, err := sqlite.NewStorage(t.TempDir() + "/empty.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	ctx := context.Background()
	instance, _ := storage.CreateInstance(ctx, &models.Instance{Name: "p", Host: "h", Port: 5432, Database: "d"})
	snap, _ := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instance, CapturedAt: time.Now()})

	coll := NewCollector(Config{Planner: &fakePlanner{}, Storage: storage, InstanceID: instance})
	if err := coll.Collect(ctx, snap); err == nil || !strings.Contains(err.Error(), "retry") {
		t.Errorf("without query statistics the collector should fail to be retried, got %v", err)
	}
}
