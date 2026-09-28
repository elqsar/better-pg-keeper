package analyzer

import (
	"reflect"
	"testing"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestRecommendIndexes(t *testing.T) {
	scan := func(table string, cols ...string) models.SeqScanFinding {
		return models.SeqScanFinding{Schema: "public", Table: table, Columns: cols, EstimatedRows: 1e6, Filter: "f"}
	}
	report := &models.QueryPlanReport{Queries: []models.QueryPlanFinding{
		{QueryID: 1, TimeShare: 0.10, SeqScans: []models.SeqScanFinding{scan("orders", "status")}},
		{QueryID: 2, TimeShare: 0.30, SeqScans: []models.SeqScanFinding{scan("orders", "status", "created_at")}},
		{QueryID: 3, TimeShare: 0.05, SeqScans: []models.SeqScanFinding{scan("orders", "customer_id")}},
		{QueryID: 4, TimeShare: 0.20, SeqScans: []models.SeqScanFinding{
			scan("events", "kind"),
			{Schema: "public", Table: "users", Columns: []string{"email"}, SkipReason: "small"},
		}},
		// The same query scanning the same table twice counts once.
		{QueryID: 5, TimeShare: 0.01, SeqScans: []models.SeqScanFinding{scan("orders", "status"), scan("orders", "status")}},
	}}

	recs := recommendIndexes(report)
	if len(recs) != 3 {
		t.Fatalf("recs = %+v", recs)
	}
	// (status) is a prefix of (status, created_at), so queries 1, 2 and 5 share it.
	top := recs[0]
	if top.Table != "orders" || !reflect.DeepEqual(top.Columns, []string{"status", "created_at"}) || len(top.Queries) != 3 {
		t.Fatalf("top = %+v", top)
	}
	if top.Queries[0].QueryID != 2 || top.TimeShare < 0.409 || top.TimeShare > 0.411 {
		t.Errorf("queries should be busiest first with summed share: %+v", top)
	}
	if recs[1].Table != "events" || recs[2].Columns[0] != "customer_id" {
		t.Errorf("order by share: %+v", recs)
	}

	plans := &QueryPlanAnalysis{Recommendations: recs}
	if len(plans.RecommendationsFor("public", "orders")) != 2 || len(plans.RecommendationsFor("public", "users")) != 0 {
		t.Error("RecommendationsFor")
	}
	var nilPlans *QueryPlanAnalysis
	if nilPlans.RecommendationsFor("public", "orders") != nil {
		t.Error("nil analysis should have no recommendations")
	}
}
