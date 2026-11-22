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

func TestMaintenanceRuleKeepsBacklogWhenBaselineMissing(t *testing.T) {
	store := newMockStorage()
	store.suggestions[1] = []models.Suggestion{{ID: 1, InstanceID: 1,
		RuleID: "table_maintenance", TargetObject: "vacuum:public.events", Status: models.StatusActive}}
	s := suggester.NewSuggester(store, nil, nil)
	s.RegisterRule(rules.NewMaintenanceRule())
	result, err := s.Suggest(context.Background(), &analyzer.AnalysisResult{InstanceID: 1, Coverage: analyzer.FullCoverage()})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedCount != 0 || store.suggestions[1][0].Status != models.StatusActive {
		t.Fatalf("backlog resolved without a baseline: %+v", result)
	}
}

func TestMaintenanceRuleKeepsOnlyTargetsWithoutTableBaseline(t *testing.T) {
	store := newMockStorage()
	store.suggestions[1] = []models.Suggestion{
		{ID: 1, InstanceID: 1, RuleID: "table_maintenance", TargetObject: "vacuum:public.events", Status: models.StatusActive},
		{ID: 2, InstanceID: 1, RuleID: "table_maintenance", TargetObject: "vacuum:public.jobs", Status: models.StatusActive},
	}
	s := suggester.NewSuggester(store, nil, nil)
	s.RegisterRule(rules.NewMaintenanceRule())
	result, err := s.Suggest(context.Background(), &analyzer.AnalysisResult{InstanceID: 1,
		Coverage: analyzer.FullCoverage(), MaintenanceBaselineObserved: true,
		MaintenanceUnobservedTargets: []string{"vacuum:public.events"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedCount != 1 || store.suggestions[1][0].Status != models.StatusActive || store.suggestions[1][1].Status != models.StatusResolved {
		t.Fatalf("selective resolution = %+v, suggestions = %+v", result, store.suggestions[1])
	}
}

func TestBloatAdviceDoesNotPrescribeVacuumFull(t *testing.T) {
	rule := rules.NewBloatRule(suggester.DefaultConfig())
	dead, free := int64(500<<20), int64(200<<20)
	items, err := rule.Evaluate(context.Background(), &analyzer.AnalysisResult{
		TableIssues: []analyzer.TableIssue{{SchemaName: "odd schema", TableName: "events", IssueType: analyzer.TableIssueHighBloat,
			CurrentValue: 60, NDeadTup: 600000, NLiveTup: 1000000, HeapBytes: 1 << 30,
			DeadTupleBytes: &dead, FreeBytes: &free, MeasurementSource: "pgstattuple_approx"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || strings.Contains(items[0].Description, "VACUUM FULL") ||
		!strings.Contains(items[0].Description, `VACUUM (ANALYZE) "odd schema"."events";`) ||
		!strings.Contains(items[0].Description, "reusable inside this table") {
		t.Fatalf("advice = %+v", items)
	}
}
