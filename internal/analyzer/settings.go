package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// StatStatementsEvictionWindow is how far back pg_stat_statements evictions
// are counted. Lifetime counts would keep flagging an eviction long fixed.
const StatStatementsEvictionWindow = 24 * time.Hour

// SettingsAnalysis contains the server configuration from the server_settings domain.
type SettingsAnalysis struct {
	*models.ServerSettings
	// Evictions is how many pg_stat_statements entries were evicted over
	// EvictionWindow. Nil until a baseline that old exists, or when the
	// statistics were reset in between.
	Evictions      *int64        `json:"evictions,omitempty"`
	EvictionWindow time.Duration `json:"eviction_window_ns,omitempty"`
}

// analyzeSettings reads the server settings and counts recent
// pg_stat_statements evictions against a baseline snapshot.
func (a *MainAnalyzer) analyzeSettings(ctx context.Context, instanceID, snapshotID int64, coverage map[string]DomainCoverage) (*SettingsAnalysis, error) {
	settings, err := a.storage.GetServerSettings(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting server settings: %w", err)
	}
	if settings == nil {
		return nil, nil
	}
	analysis := &SettingsAnalysis{ServerSettings: settings}
	if settings.StatStatements == nil {
		return analysis, nil
	}

	now := coverage[DomainSettings].CapturedAt
	if now.IsZero() {
		now = time.Now()
	}
	baseSnap, err := a.storage.GetLatestSnapshotWithCollector(ctx, instanceID, DomainSettings, now.Add(-StatStatementsEvictionWindow))
	if err != nil {
		return nil, fmt.Errorf("finding settings baseline: %w", err)
	}
	if baseSnap == nil || baseSnap.ID == snapshotID {
		return analysis, nil
	}
	base, err := a.storage.GetServerSettings(ctx, baseSnap.ID)
	if err != nil {
		return nil, fmt.Errorf("getting settings baseline: %w", err)
	}
	if base == nil || base.StatStatements == nil || !sameReset(base.StatStatements.StatsReset, settings.StatStatements.StatsReset) {
		return analysis, nil
	}
	evicted := settings.StatStatements.Dealloc - base.StatStatements.Dealloc
	if evicted < 0 {
		return analysis, nil
	}
	analysis.Evictions = &evicted
	analysis.EvictionWindow = now.Sub(baseSnap.CapturedAt)
	return analysis, nil
}

// GetSetting returns a setting by name. It is safe on a nil analysis, which
// rules that only optionally read settings rely on.
func (s *SettingsAnalysis) GetSetting(name string) (models.Setting, bool) {
	if s == nil {
		return models.Setting{}, false
	}
	return s.ServerSettings.Get(name)
}

func sameReset(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
