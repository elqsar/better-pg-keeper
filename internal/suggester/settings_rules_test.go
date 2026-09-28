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

// tunedSettings is a configuration the review has nothing to say about.
func tunedSettings() map[string]models.Setting {
	list := []models.Setting{
		{Name: "autovacuum", Setting: "on", Context: "sighup"},
		{Name: "track_counts", Setting: "on", Context: "superuser"},
		{Name: "idle_in_transaction_session_timeout", Setting: "300000", Unit: "ms", Context: "user"},
		{Name: "statement_timeout", Setting: "30000", Unit: "ms", Context: "user"},
		{Name: "log_min_duration_statement", Setting: "1000", Unit: "ms", Context: "superuser"},
		{Name: "log_lock_waits", Setting: "on", Context: "superuser"},
		{Name: "log_temp_files", Setting: "10240", Unit: "kB", Context: "superuser"},
		{Name: "track_io_timing", Setting: "on", Context: "superuser"},
		{Name: "shared_buffers", Setting: "524288", Unit: "8kB", Context: "postmaster"}, // 4GB
		{Name: "work_mem", Setting: "16384", Unit: "kB", Context: "user"},               // 16MB
		{Name: "max_connections", Setting: "200", Context: "postmaster"},
		{Name: "random_page_cost", Setting: "1.1", Context: "user"},
		{Name: "pg_stat_statements.max", Setting: "10000", Context: "postmaster"},
		{Name: "pg_stat_statements.track", Setting: "top", Context: "superuser"},
	}
	m := make(map[string]models.Setting, len(list))
	for _, s := range list {
		m[s.Name] = s
	}
	return m
}

func settingsAnalysis(s *models.ServerSettings) *analyzer.AnalysisResult {
	return &analyzer.AnalysisResult{
		AnalyzedAt: time.Now(),
		Coverage:   analyzer.FullCoverage(),
		Settings:   &analyzer.SettingsAnalysis{ServerSettings: s},
	}
}

func TestConfigurationRule_TunedIsQuiet(t *testing.T) {
	cfg := suggester.DefaultConfig()
	cfg.ServerMemoryBytes = 16 << 30
	got := severities(t, rules.NewConfigurationRule(cfg), settingsAnalysis(&models.ServerSettings{Settings: tunedSettings()}))
	if len(got) != 0 {
		t.Errorf("tuned settings produced %v", got)
	}
}

func TestConfigurationRule_Findings(t *testing.T) {
	set := tunedSettings()
	override := func(name, value string) {
		s := set[name]
		s.Setting = value
		set[name] = s
	}
	override("autovacuum", "off")
	override("idle_in_transaction_session_timeout", "0")
	override("statement_timeout", "0")
	override("log_min_duration_statement", "-1")
	override("log_lock_waits", "off")
	override("random_page_cost", "4")
	override("work_mem", "65536") // 64MB x 200 connections + 4GB > 16GB
	pending := set["shared_buffers"]
	pending.PendingRestart = true
	set["shared_buffers"] = pending

	settings := &models.ServerSettings{
		Settings: set,
		AutovacuumDisabled: []models.TableSize{
			{SchemaName: "public", RelName: "events", TotalBytes: 5 << 30},
			{SchemaName: "public", RelName: "tags", TotalBytes: 1 << 20},
		},
	}
	analysis := settingsAnalysis(settings)
	analysis.ActivityStats = &analyzer.ActivityAnalysis{
		IdleInTransaction: []models.IdleInTransaction{{PID: 7}},
	}

	cfg := suggester.DefaultConfig()
	cfg.ServerMemoryBytes = 16 << 30
	rule := rules.NewConfigurationRule(cfg)
	got := severities(t, rule, analysis)
	want := map[string]string{
		"setting:autovacuum":                          models.SeverityCritical,
		"autovacuum_disabled:public.events":           models.SeverityCritical,
		"autovacuum_disabled:public.tags":             models.SeverityWarning,
		"setting:idle_in_transaction_session_timeout": models.SeverityWarning,
		"setting:statement_timeout":                   models.SeverityInfo,
		"setting:observability":                       models.SeverityInfo,
		"setting:random_page_cost":                    models.SeverityInfo,
		"setting:work_mem":                            models.SeverityWarning,
		"setting:pending_restart":                     models.SeverityInfo,
	}
	for target, sev := range want {
		if got[target] != sev {
			t.Errorf("%s = %q, want %q", target, got[target], sev)
		}
	}
	for target := range got {
		if _, ok := want[target]; !ok {
			t.Errorf("unexpected finding %s", target)
		}
	}

	// Descriptions carry runnable fixes that say whether a restart is needed.
	sugs, _ := rule.Evaluate(context.Background(), analysis)
	for _, s := range sugs {
		switch s.TargetObject {
		case "setting:autovacuum":
			mustContain(t, s.Description, "ALTER SYSTEM SET autovacuum = 'on';", "SELECT pg_reload_conf();")
		case "autovacuum_disabled:public.events":
			mustContain(t, s.Description, `ALTER TABLE "public"."events" RESET (autovacuum_enabled);`)
		case "setting:observability":
			mustContain(t, s.Description, "log_min_duration_statement = '1s'", "log_lock_waits = 'on'")
		case "setting:pending_restart":
			mustContain(t, s.Description, "`shared_buffers` (running with `4GB`)")
		}
	}

	// Without idle sessions the missing timeout is only informational.
	analysis.ActivityStats = nil
	if got := severities(t, rule, analysis); got["setting:idle_in_transaction_session_timeout"] != models.SeverityInfo {
		t.Errorf("idle timeout without idle sessions = %q, want info", got["setting:idle_in_transaction_session_timeout"])
	}
}

func TestConfigurationRule_SharedBuffers(t *testing.T) {
	withSB := func(value string) *models.ServerSettings {
		set := tunedSettings()
		sb := set["shared_buffers"]
		sb.Setting = value
		set["shared_buffers"] = sb
		return &models.ServerSettings{Settings: set}
	}

	// Memory unknown: only the untouched initdb value on a large database.
	cfg := suggester.DefaultConfig()
	defaultSB := settingsAnalysis(withSB("16384")) // 128MB
	defaultSB.Risk = &analyzer.RiskAnalysis{OutageRisk: &models.OutageRisk{ClusterSize: 50 << 30}}
	if got := severities(t, rules.NewConfigurationRule(cfg), defaultSB); got["setting:shared_buffers"] != models.SeverityWarning {
		t.Errorf("128MB on 50GB = %v, want warning", got)
	}
	defaultSB.Risk.ClusterSize = 500 << 20
	if got := severities(t, rules.NewConfigurationRule(cfg), defaultSB); len(got) != 0 {
		t.Errorf("128MB on a small database = %v, want nothing", got)
	}

	// Memory known: outside 15-40% of RAM.
	cfg.ServerMemoryBytes = 64 << 30
	for value, want := range map[string]string{
		"262144":  models.SeverityWarning, // 2GB = 3%
		"2097152": "",                     // 16GB = 25%
		"4194304": models.SeverityWarning, // 32GB = 50%
	} {
		got := severities(t, rules.NewConfigurationRule(cfg), settingsAnalysis(withSB(value)))
		if got["setting:shared_buffers"] != want {
			t.Errorf("shared_buffers %s on 64GB = %v, want %q", value, got, want)
		}
	}
}

func TestConfigurationRule_UnavailableIsNotResolution(t *testing.T) {
	settings := &models.ServerSettings{
		Settings:    tunedSettings(),
		Unavailable: map[string]string{"autovacuum_disabled": "permission denied"},
	}
	if _, err := rules.NewConfigurationRule(suggester.DefaultConfig()).Evaluate(context.Background(), settingsAnalysis(settings)); err == nil {
		t.Error("a failed check must be an error so existing findings are not resolved")
	}
}

func TestStatStatementsCapacityRule(t *testing.T) {
	cfg := suggester.DefaultConfig()
	rule := rules.NewStatStatementsCapacityRule(cfg)
	build := func(entries, max int64, evictions *int64) *analyzer.AnalysisResult {
		a := settingsAnalysis(&models.ServerSettings{
			Settings:       tunedSettings(),
			StatStatements: &models.StatStatementsUsage{Entries: entries, Max: max, Dealloc: 12},
		})
		a.Settings.Evictions = evictions
		a.Settings.EvictionWindow = 24 * time.Hour
		return a
	}

	if got := severities(t, rule, build(2000, 10000, int64p(0))); len(got) != 0 {
		t.Errorf("20%% full, no evictions = %v, want nothing", got)
	}
	if got := severities(t, rule, build(9600, 10000, nil)); got["setting:pg_stat_statements.max"] != models.SeverityWarning {
		t.Errorf("96%% full = %v, want warning", got)
	}
	evicting := build(3000, 10000, int64p(4))
	if got := severities(t, rule, evicting); got["setting:pg_stat_statements.max"] != models.SeverityWarning {
		t.Errorf("evictions after a reset = %v, want warning", got)
	}
	sugs, _ := rule.Evaluate(context.Background(), evicting)
	mustContain(t, sugs[0].Description, "4 time(s) in the last 24h", "pg_stat_statements.max = '20000'", "restart")

	// Not installed: nothing to say, and nothing to resolve wrongly either.
	notInstalled := settingsAnalysis(&models.ServerSettings{Settings: tunedSettings()})
	if got := severities(t, rule, notInstalled); len(got) != 0 {
		t.Errorf("not installed = %v", got)
	}

	// track = none makes all query analysis blind.
	set := tunedSettings()
	track := set["pg_stat_statements.track"]
	track.Setting = "none"
	set["pg_stat_statements.track"] = track
	if got := severities(t, rule, settingsAnalysis(&models.ServerSettings{Settings: set})); got["setting:pg_stat_statements.track"] != models.SeverityWarning {
		t.Errorf("track=none = %v, want warning", got)
	}

	failed := settingsAnalysis(&models.ServerSettings{
		Settings:    tunedSettings(),
		Unavailable: map[string]string{"stat_statements": "relation does not exist"},
	})
	if _, err := rule.Evaluate(context.Background(), failed); err == nil {
		t.Error("a failed stat_statements check must be an error")
	}
}

func mustContain(t *testing.T, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(s, p) {
			t.Errorf("missing %q in:\n%s", p, s)
		}
	}
}
