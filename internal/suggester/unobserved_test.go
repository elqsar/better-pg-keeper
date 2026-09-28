package suggester_test

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

// A failed outage-risk check must not resolve the findings built from it,
// while findings from checks that succeeded keep resolving normally.
func TestSuggest_UnobservedTargetsAreNotResolved(t *testing.T) {
	ctx := context.Background()
	cfg := suggester.DefaultConfig()
	storage := newMockStorage()
	sug := suggester.NewSuggester(storage, cfg, log.New(io.Discard, "", 0))
	sug.RegisterRules(rules.NewReplicationSlotRule(cfg), rules.NewSequenceExhaustionRule(cfg))

	retained := int64(20 << 30)
	first := &models.OutageRisk{
		Slots:     []models.ReplicationSlot{{Name: "old", Type: "physical", RetainedBytes: &retained, WALStatus: "extended"}},
		Sequences: []models.SequenceUsage{{SchemaName: "public", SequenceName: "s", MaxValue: 100, LastValue: 95, UsedFraction: 0.95}},
	}
	analysis := func(risk *models.OutageRisk) *analyzer.AnalysisResult {
		return &analyzer.AnalysisResult{InstanceID: 1, AnalyzedAt: time.Now(), Coverage: analyzer.FullCoverage(),
			Risk: &analyzer.RiskAnalysis{OutageRisk: risk}}
	}
	if _, err := sug.Suggest(ctx, analysis(first)); err != nil {
		t.Fatal(err)
	}
	if active, _ := storage.GetSuggestionsByStatus(ctx, 1, models.StatusActive); len(active) != 2 {
		t.Fatalf("active = %+v, want slot and sequence", active)
	}

	// The slot check now fails, and the sequence was fixed.
	second := &models.OutageRisk{Unavailable: map[string]string{"replication_slots": "permission denied"}}
	res, err := sug.Suggest(ctx, analysis(second))
	if err != nil {
		t.Fatal(err)
	}
	if res.ResolvedCount != 1 || len(res.Errors) != 0 {
		t.Errorf("resolved %d, errors %v; want only the sequence resolved", res.ResolvedCount, res.Errors)
	}
	active, _ := storage.GetSuggestionsByStatus(ctx, 1, models.StatusActive)
	if len(active) != 1 || active[0].TargetObject != "slot:old" {
		t.Errorf("active = %+v, want slot:old kept", active)
	}
}
