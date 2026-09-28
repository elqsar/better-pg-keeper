package suggester_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

func int64p(v int64) *int64       { return &v }
func float64p(v float64) *float64 { return &v }

func riskAnalysis(risk *models.OutageRisk) *analyzer.AnalysisResult {
	return &analyzer.AnalysisResult{
		AnalyzedAt: time.Now(),
		Coverage:   analyzer.FullCoverage(),
		Risk:       &analyzer.RiskAnalysis{OutageRisk: risk},
	}
}

// severities evaluates a rule and returns target -> severity.
func severities(t *testing.T, rule suggester.Rule, analysis *analyzer.AnalysisResult) map[string]string {
	t.Helper()
	got, err := rule.Evaluate(context.Background(), analysis)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(got))
	for _, s := range got {
		if s.RuleID != rule.ID() || s.Title == "" || s.Description == "" {
			t.Errorf("incomplete suggestion: %+v", s)
		}
		out[s.TargetObject] = s.Severity
	}
	return out
}

func TestWraparoundRules(t *testing.T) {
	cfg := suggester.DefaultConfig()
	risk := &models.OutageRisk{
		Database:              "app",
		FreezeMaxAge:          200_000_000,
		MultixactFreezeMaxAge: 400_000_000,
		Databases: []models.DatabaseAge{
			{Name: "app", XIDAge: 1_200_000_000, MXIDAge: 10},
			{Name: "reports", XIDAge: 350_000_000, MXIDAge: 600_000_000},
			{Name: "fine", XIDAge: 150_000_000, MXIDAge: 10},
		},
		OldestTables: []models.TableAge{{SchemaName: "public", RelName: "Events", XIDAge: 1_200_000_000}},
		XminBackends: []models.XminBackend{{PID: 42, Username: "etl", Database: "app", State: "idle in transaction", XminAge: 900_000_000, Query: "SELECT 1"}},
		Slots:        []models.ReplicationSlot{{Name: "old_standby", Type: "physical", XminAge: int64p(1_100_000_000)}},
	}

	// 1.5 x freeze_max_age (300M) comes before the 500M default.
	xid := severities(t, rules.NewXIDWraparoundRule(cfg), riskAnalysis(risk))
	want := map[string]string{"database:app": models.SeverityCritical, "database:reports": models.SeverityWarning}
	if len(xid) != len(want) || xid["database:app"] != want["database:app"] || xid["database:reports"] != want["database:reports"] {
		t.Errorf("xid severities = %v, want %v", xid, want)
	}

	mxid := severities(t, rules.NewMultixactWraparoundRule(cfg), riskAnalysis(risk))
	if len(mxid) != 1 || mxid["database:reports"] != models.SeverityWarning {
		t.Errorf("multixact severities = %v, want reports warning", mxid)
	}

	got, _ := rules.NewXIDWraparoundRule(cfg).Evaluate(context.Background(), riskAnalysis(risk))
	for _, s := range got {
		if s.TargetObject != "database:app" {
			continue
		}
		for _, want := range []string{`VACUUM (FREEZE, VERBOSE) "public"."Events"`, "Replication slot `old_standby`", "PID 42"} {
			if !strings.Contains(s.Description, want) {
				t.Errorf("description missing %q:\n%s", want, s.Description)
			}
		}
		// The slot is older than the session, so it is listed first.
		if strings.Index(s.Description, "old_standby") > strings.Index(s.Description, "PID 42") {
			t.Error("holders should be listed oldest first")
		}
	}
}

func TestReplicationSlotRule(t *testing.T) {
	cfg := suggester.DefaultConfig()
	risk := &models.OutageRisk{
		Slots: []models.ReplicationSlot{
			{Name: "healthy", Type: "physical", Active: true, WALStatus: "reserved", RetainedBytes: int64p(100 << 20)},
			{Name: "abandoned", Type: "logical", Active: false, WALStatus: "extended", RetainedBytes: int64p(2 << 30)},
			{Name: "huge", Type: "physical", Active: true, WALStatus: "extended", RetainedBytes: int64p(20 << 30)},
			{Name: "doomed", Type: "physical", WALStatus: "unreserved"},
			{Name: "gone", Type: "logical", WALStatus: "lost"},
			{Name: "standby_view", Type: "physical"}, // on a standby: no retained bytes
		},
		Replicas: []models.ReplicaLag{
			{ApplicationName: "replica1", State: "streaming", ReplayLagSecs: float64p(900)},
			{ApplicationName: "replica2", State: "streaming", ReplayLagSecs: float64p(2)},
			{ApplicationName: "replica3", State: "streaming"}, // lag hidden without pg_monitor
		},
	}
	got := severities(t, rules.NewReplicationSlotRule(cfg), riskAnalysis(risk))
	want := map[string]string{
		"slot:abandoned":   models.SeverityWarning,
		"slot:huge":        models.SeverityCritical,
		"slot:doomed":      models.SeverityCritical,
		"slot:gone":        models.SeverityWarning,
		"replica:replica1": models.SeverityWarning,
	}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for target, sev := range want {
		if got[target] != sev {
			t.Errorf("%s = %q, want %q", target, got[target], sev)
		}
	}
}

func TestSequenceExhaustionRule(t *testing.T) {
	cfg := suggester.DefaultConfig()
	risk := &models.OutageRisk{Sequences: []models.SequenceUsage{
		{SchemaName: "public", SequenceName: "orders_id_seq", TableName: "orders", ColumnName: "id", ColumnType: "integer",
			LastValue: 2_000_000_000, MaxValue: 2147483647, UsedFraction: 2_000_000_000.0 / 2147483647},
		{SchemaName: "public", SequenceName: "ticket_seq", LastValue: 80, MaxValue: 100, UsedFraction: 0.8},
		{SchemaName: "public", SequenceName: "small_seq", LastValue: 10, MaxValue: 100, UsedFraction: 0.1},
	}}
	rule := rules.NewSequenceExhaustionRule(cfg)
	got := severities(t, rule, riskAnalysis(risk))
	if len(got) != 2 || got["sequence:public.orders_id_seq"] != models.SeverityCritical || got["sequence:public.ticket_seq"] != models.SeverityWarning {
		t.Errorf("got %v", got)
	}

	sugs, _ := rule.Evaluate(context.Background(), riskAnalysis(risk))
	for _, s := range sugs {
		if s.TargetObject == "sequence:public.orders_id_seq" &&
			!strings.Contains(s.Description, `ALTER TABLE "public"."orders" ALTER COLUMN "id" TYPE bigint;`) {
			t.Errorf("missing ALTER TABLE advice:\n%s", s.Description)
		}
	}
}

func TestPreparedTransactionRule(t *testing.T) {
	cfg := suggester.DefaultConfig()
	now := time.Now()
	analysis := riskAnalysis(&models.OutageRisk{PreparedXacts: []models.PreparedXact{
		{GID: "fresh", Database: "app", Prepared: now.Add(-time.Minute)},
		{GID: "stuck", Database: "app", Prepared: now.Add(-3 * time.Hour)},
		{GID: "o'rphan", Database: "app", Prepared: now.Add(-72 * time.Hour)},
	}})
	analysis.Coverage[analyzer.DomainOutageRisk] = analyzer.DomainCoverage{Present: true, CapturedAt: now}

	rule := rules.NewPreparedTransactionRule(cfg)
	got := severities(t, rule, analysis)
	if len(got) != 2 || got["prepared:stuck"] != models.SeverityWarning || got["prepared:o'rphan"] != models.SeverityCritical {
		t.Errorf("got %v", got)
	}
	sugs, _ := rule.Evaluate(context.Background(), analysis)
	for _, s := range sugs {
		if s.TargetObject == "prepared:o'rphan" && !strings.Contains(s.Description, "ROLLBACK PREPARED 'o''rphan';") {
			t.Errorf("GID not quoted as a literal:\n%s", s.Description)
		}
	}
}

func TestDiskGrowthRule(t *testing.T) {
	trend := func(current int64, perDay float64) *analyzer.AnalysisResult {
		a := riskAnalysis(&models.OutageRisk{})
		a.Risk.SizeTrend = &analyzer.SizeTrend{CurrentBytes: current, GrowthBytesPerDay: perDay, Span: 7 * 24 * time.Hour, Samples: 168}
		return a
	}
	withCapacity := func(gb float64) *suggester.Config {
		return suggester.ConfigFromThresholds(config.ThresholdsConfig{DiskCapacityGB: gb})
	}
	const gb = 1 << 30

	tests := []struct {
		name     string
		cfg      *suggester.Config
		analysis *analyzer.AnalysisResult
		want     string
	}{
		{"full in 5 days", withCapacity(100), trend(90*gb, 2*gb), models.SeverityCritical},
		{"full in 20 days", withCapacity(100), trend(60*gb, 2*gb), models.SeverityWarning},
		{"full in 200 days", withCapacity(500), trend(100*gb, 2*gb), ""},
		{"shrinking", withCapacity(100), trend(99*gb, -gb), ""},
		{"no capacity, doubling fast", suggester.DefaultConfig(), trend(10*gb, gb), models.SeverityInfo},
		{"no capacity, slow growth", suggester.DefaultConfig(), trend(1000*gb, gb), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := severities(t, rules.NewDiskGrowthRule(tt.cfg), tt.analysis)
			if got["instance:disk"] != tt.want || (tt.want == "" && len(got) != 0) {
				t.Errorf("got %v, want %q", got, tt.want)
			}
		})
	}
}

func TestConnectionSaturationRule(t *testing.T) {
	cfg := suggester.DefaultConfig()
	activity := func(total, idle int, peak *analyzer.ConnectionPeakAnalysis) *analyzer.AnalysisResult {
		return &analyzer.AnalysisResult{
			Coverage: analyzer.FullCoverage(),
			ActivityStats: &analyzer.ActivityAnalysis{
				TotalConnections: total, IdleCount: idle, MaxConnections: 100, Peak: peak,
			},
		}
	}
	peak := func(total, idle int) *analyzer.ConnectionPeakAnalysis {
		return &analyzer.ConnectionPeakAnalysis{
			ConnectionPeak: models.ConnectionPeak{TotalConnections: total, IdleCount: idle, MaxConnections: 100, CapturedAt: time.Now()},
			Window:         24 * time.Hour,
		}
	}

	rule := rules.NewConnectionSaturationRule(cfg)
	if got := severities(t, rule, activity(30, 10, nil)); len(got) != 0 {
		t.Errorf("30%% should be fine, got %v", got)
	}
	if got := severities(t, rule, activity(85, 10, nil)); got["instance:connections"] != models.SeverityWarning {
		t.Errorf("85%% now: got %v", got)
	}
	// A quiet moment now does not hide a saturated peak earlier in the day.
	if got := severities(t, rule, activity(30, 10, peak(97, 80))); got["instance:connections"] != models.SeverityCritical {
		t.Errorf("97%% peak: got %v", got)
	}

	sugs, _ := rule.Evaluate(context.Background(), activity(30, 10, peak(97, 80)))
	if len(sugs) != 1 || !strings.Contains(sugs[0].Description, "PgBouncer") || !strings.Contains(sugs[0].Description, "Most connections are idle") {
		t.Errorf("mostly idle peak should recommend a pooler: %+v", sugs)
	}
}

func TestConfigFromThresholdsKeepsCriticalAboveWarning(t *testing.T) {
	c := suggester.ConfigFromThresholds(config.ThresholdsConfig{
		SlowQueryMs:          8000,
		CacheHitRatioWarning: 0.85,
		BloatPercentWarning:  60,
		SeqScanRatioWarning:  0.9,
		DiskCapacityGB:       2,
	})
	if c.SlowQueryMs != 8000 || c.SlowQueryCriticalMs < c.SlowQueryMs {
		t.Errorf("slow query warning/critical = %v/%v", c.SlowQueryMs, c.SlowQueryCriticalMs)
	}
	if c.CacheHitRatioCritical > c.CacheHitRatioWarning {
		t.Errorf("cache critical %v above warning %v", c.CacheHitRatioCritical, c.CacheHitRatioWarning)
	}
	if c.BloatPercentCritical < c.BloatPercentWarning || c.SeqScanRatioCritical < c.SeqScanRatioWarning {
		t.Errorf("critical below warning: %+v", c)
	}
	if c.DiskCapacityBytes != 2<<30 {
		t.Errorf("disk capacity = %d", c.DiskCapacityBytes)
	}
	if d := suggester.ConfigFromThresholds(config.Default().Thresholds); *d != *withoutDisk(suggester.DefaultConfig()) {
		t.Errorf("default thresholds should give the default config:\n%+v\n%+v", d, suggester.DefaultConfig())
	}
}

func withoutDisk(c *suggester.Config) *suggester.Config {
	c.DiskCapacityBytes = 0
	return c
}
