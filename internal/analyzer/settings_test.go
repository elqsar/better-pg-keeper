package analyzer

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestMainAnalyzer_SettingsEvictions(t *testing.T) {
	now := time.Now()
	reset := now.Add(-30 * 24 * time.Hour)
	otherReset := now.Add(-time.Hour)

	cases := []struct {
		name      string
		baseline  *models.StatStatementsUsage
		current   models.StatStatementsUsage
		wantEvict *int64
	}{
		{"no baseline", nil, models.StatStatementsUsage{Dealloc: 40, StatsReset: &reset}, nil},
		{"counted since baseline", &models.StatStatementsUsage{Dealloc: 10, StatsReset: &reset}, models.StatStatementsUsage{Dealloc: 40, StatsReset: &reset}, int64ptr(30)},
		{"reset in between", &models.StatStatementsUsage{Dealloc: 10, StatsReset: &reset}, models.StatStatementsUsage{Dealloc: 2, StatsReset: &otherReset}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storage := newMockStorage()
			storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
			current := tc.current
			storage.settings[2] = &models.ServerSettings{StatStatements: &current}
			if tc.baseline != nil {
				storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-25 * time.Hour)}
				storage.settings[1] = &models.ServerSettings{StatStatements: tc.baseline}
			}

			result, err := NewMainAnalyzer(storage, nil).Analyze(context.Background(), 2)
			if err != nil {
				t.Fatal(err)
			}
			if !result.DomainCoverage(DomainSettings).Usable() || result.Settings == nil {
				t.Fatalf("settings = %+v, coverage %+v", result.Settings, result.DomainCoverage(DomainSettings))
			}
			got := result.Settings.Evictions
			if (got == nil) != (tc.wantEvict == nil) || (got != nil && *got != *tc.wantEvict) {
				t.Errorf("evictions = %v, want %v", deref(got), deref(tc.wantEvict))
			}
			if got != nil && result.Settings.EvictionWindow < 24*time.Hour {
				t.Errorf("eviction window = %s, want >= 24h", result.Settings.EvictionWindow)
			}
		})
	}
}

func TestMainAnalyzer_SettingsAbsentIsUncovered(t *testing.T) {
	storage := newMockStorage()
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: time.Now()}

	result, err := NewMainAnalyzer(storage, nil).Analyze(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Settings != nil || result.DomainCoverage(DomainSettings).Usable() {
		t.Errorf("no settings must leave the domain uncovered, got %+v", result.Settings)
	}
	if _, ok := result.Settings.GetSetting("work_mem"); ok {
		t.Error("GetSetting on nil analysis should report missing")
	}
}

func int64ptr(v int64) *int64 { return &v }

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
