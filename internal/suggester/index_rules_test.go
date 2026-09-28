package suggester_test

import (
	"context"
	"strings"
	"testing"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

func planAnalysis(hypo bool, recs ...analyzer.IndexRecommendation) *analyzer.AnalysisResult {
	return &analyzer.AnalysisResult{
		Coverage: analyzer.FullCoverage(),
		QueryPlans: &analyzer.QueryPlanAnalysis{
			Report: &models.QueryPlanReport{HypoPG: hypo, Queries: []models.QueryPlanFinding{
				{QueryID: 9, TimeShare: 0.2, SeqScans: []models.SeqScanFinding{
					{Schema: "public", Table: "audit", SkipReason: "the filter matches ~60% of the table, too much for an index to help"},
				}},
			}},
			Recommendations: recs,
		},
	}
}

func TestIndexRecommendationRule(t *testing.T) {
	rec := analyzer.IndexRecommendation{
		Schema: "public", Table: "orders", Columns: []string{"status", "created_at"},
		TimeShare: 0.3, EstimatedRows: 2_000_000, EstimatedBytes: 64 << 20, Validated: true,
		Queries: []analyzer.RecommendedFor{{QueryID: 42, TimeShare: 0.3, Filter: "(o.status = $1)",
			Validation: &models.IndexValidation{CostBefore: 45000, CostAfter: 12, UsesIndex: true}}},
	}
	small := analyzer.IndexRecommendation{Schema: "public", Table: "events", Columns: []string{"kind"}, TimeShare: 0.01,
		Queries: []analyzer.RecommendedFor{{QueryID: 7, TimeShare: 0.01}}}

	got, err := rules.NewIndexRecommendationRule(suggester.DefaultConfig()).Evaluate(context.Background(), planAnalysis(true, rec, small))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Severity != models.SeverityWarning || got[1].Severity != models.SeverityInfo {
		t.Errorf("severities = %s, %s", got[0].Severity, got[1].Severity)
	}
	if got[0].TargetObject != "index:public.orders(status,created_at)" {
		t.Errorf("target = %s", got[0].TargetObject)
	}
	for _, want := range []string{
		`CREATE INDEX CONCURRENTLY "idx_orders_status_created_at" ON "public"."orders" ("status", "created_at");`,
		`DROP INDEX CONCURRENTLY "public"."idx_orders_status_created_at";`,
		"[Query 42](/queries/42)",
		"45k → 12",
		"Checked with hypopg",
		"INVALID index",
	} {
		if !strings.Contains(got[0].Description, want) {
			t.Errorf("description missing %q:\n%s", want, got[0].Description)
		}
	}
	if !strings.Contains(got[1].Description, "could not confirm") {
		t.Errorf("unvalidated proposal with hypopg present should say so:\n%s", got[1].Description)
	}
}

func TestMissingIndexDefersToRecommendations(t *testing.T) {
	issue := func(table string) analyzer.TableIssue {
		return analyzer.TableIssue{SchemaName: "public", TableName: table, IssueType: analyzer.TableIssueMissingIndex,
			SeqScanRatio: 0.9, TableSize: 1 << 30, NLiveTup: 1e6}
	}
	analysis := planAnalysis(false, analyzer.IndexRecommendation{Schema: "public", Table: "orders", Columns: []string{"status"}})
	analysis.TableIssues = []analyzer.TableIssue{issue("orders"), issue("audit"), issue("logs")}

	rule := rules.NewMissingIndexRule(suggester.DefaultConfig())
	got, err := rule.Evaluate(context.Background(), analysis)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]string{}
	for _, s := range got {
		targets[s.TargetObject] = s.Description
	}
	if _, ok := targets["public.orders"]; ok || len(targets) != 2 {
		t.Fatalf("orders has a concrete recommendation and must be skipped: %v", targets)
	}
	if !strings.Contains(targets["public.audit"], "Query 9 (20% of DB time): the filter matches ~60%") {
		t.Errorf("skipped scans should be explained:\n%s", targets["public.audit"])
	}
	if strings.Contains(targets["public.logs"], "<column>") {
		t.Error("placeholder DDL should be gone")
	}

	// Without usable plans nothing is suppressed.
	delete(analysis.Coverage, analyzer.DomainQueryPlans)
	got, _ = rule.Evaluate(context.Background(), analysis)
	if len(got) != 3 {
		t.Errorf("without plans every table is reported, got %d", len(got))
	}
}
