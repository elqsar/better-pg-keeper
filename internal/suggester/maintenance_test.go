package suggester_test

import (
	"context"
	"strings"
	"testing"
	"time"

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

func TestMaintenanceRuleExactUnobservedDoesNotBlockPrefixSibling(t *testing.T) {
	store := newMockStorage()
	store.suggestions[1] = []models.Suggestion{
		{ID: 1, InstanceID: 1, RuleID: "table_maintenance", TargetObject: "vacuum:public.order", Status: models.StatusActive},
		{ID: 2, InstanceID: 1, RuleID: "table_maintenance", TargetObject: "vacuum:public.orders", Status: models.StatusActive},
	}
	s := suggester.NewSuggester(store, nil, nil)
	s.RegisterRule(rules.NewMaintenanceRule())

	result, err := s.Suggest(context.Background(), &analyzer.AnalysisResult{
		InstanceID:                   1,
		Coverage:                     analyzer.FullCoverage(),
		MaintenanceBaselineObserved:  true,
		MaintenanceUnobservedTargets: []string{"vacuum:public.order"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedCount != 1 || store.suggestions[1][0].Status != models.StatusActive || store.suggestions[1][1].Status != models.StatusResolved {
		t.Fatalf("prefix sibling blocked from resolving: result=%+v suggestions=%+v", result, store.suggestions[1])
	}
}

func TestLongRunningQueryRuleWaitEventNilAndCancelPID(t *testing.T) {
	waitType := "Lock"
	rule := rules.NewLongRunningQueryRule(suggester.DefaultConfig())
	items, err := rule.Evaluate(context.Background(), &analyzer.AnalysisResult{
		ActivityStats: &analyzer.ActivityAnalysis{
			LongRunningQueries: []models.LongRunningQuery{
				{
					PID:             4242,
					Username:        "app",
					DatabaseName:    "prod",
					State:           "active",
					WaitEventType:   &waitType,
					WaitEvent:       nil, // must not panic
					QueryStart:      time.Now().Add(-2 * time.Minute),
					DurationSeconds: 120,
					Query:           "SELECT * FROM orders",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(items))
	}
	if !strings.Contains(items[0].Description, "SELECT pg_cancel_backend(4242);") {
		t.Errorf("cancel SQL missing formatted PID: %s", items[0].Description)
	}
	if !strings.Contains(items[0].Description, "- Wait Event: Lock\n") {
		t.Errorf("unexpected Wait Event formatting: %s", items[0].Description)
	}
}

func TestHighDeadlocksAndTempUsageBaselineAndResolution(t *testing.T) {
	ctx := context.Background()
	cfg := suggester.DefaultConfig()
	store := newMockStorage()
	s := suggester.NewSuggester(store, cfg, nil)
	s.RegisterRules(rules.NewHighDeadlocksRule(cfg), rules.NewHighTempUsageRule(cfg))

	// 1. Without a baseline, rules emit nothing even if counters were set.
	res, err := s.Suggest(ctx, &analyzer.AnalysisResult{
		InstanceID: 1,
		Coverage:   analyzer.FullCoverage(),
		TransactionStats: &analyzer.TransactionAnalysis{
			Deadlocks:        10,
			TempBytes:        20 << 30,
			BaselineObserved: false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalSuggestions != 0 {
		t.Fatalf("unobserved baseline emitted %d suggestions, want 0", res.TotalSuggestions)
	}

	// 2. With an observed 1h window delta exceeding thresholds, both fire.
	res, err = s.Suggest(ctx, &analyzer.AnalysisResult{
		InstanceID: 1,
		Coverage:   analyzer.FullCoverage(),
		TransactionStats: &analyzer.TransactionAnalysis{
			Deadlocks:        2,
			TempFiles:        3,
			TempBytes:        2 << 30,
			Window:           time.Hour,
			BaselineObserved: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalSuggestions != 2 {
		t.Fatalf("got %d suggestions, want 2", res.TotalSuggestions)
	}

	// 3. If the next cycle lacks a baseline, active findings stay unresolved.
	res, err = s.Suggest(ctx, &analyzer.AnalysisResult{
		InstanceID:       1,
		Coverage:         analyzer.FullCoverage(),
		TransactionStats: &analyzer.TransactionAnalysis{BaselineObserved: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResolvedCount != 0 {
		t.Fatalf("missing baseline resolved %d active findings, want 0", res.ResolvedCount)
	}

	// 4. Once the window is clean with BaselineObserved=true, both resolve.
	res, err = s.Suggest(ctx, &analyzer.AnalysisResult{
		InstanceID:       1,
		Coverage:         analyzer.FullCoverage(),
		TransactionStats: &analyzer.TransactionAnalysis{Window: time.Hour, BaselineObserved: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResolvedCount != 2 {
		t.Fatalf("clean observed window resolved %d findings, want 2", res.ResolvedCount)
	}
}
