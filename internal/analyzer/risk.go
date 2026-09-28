package analyzer

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

const (
	// SizeTrendWindow is how far back disk growth is fitted. A week smooths out
	// daily load patterns while still reacting to a change in growth rate.
	SizeTrendWindow = 7 * 24 * time.Hour
	// MinSizeTrendSpan is the least history needed before a growth rate is
	// reported; a few hours of samples extrapolate badly.
	MinSizeTrendSpan = 24 * time.Hour
	// ConnectionPeakWindow is the period the connection peak is taken over.
	ConnectionPeakWindow = 24 * time.Hour
	// TableGrowthBaselineAge is how old the table-size baseline must be before
	// per-table growth is compared against it.
	TableGrowthBaselineAge = 24 * time.Hour
	maxGrowingTables       = 5
)

// RiskAnalysis contains outage-risk signals from the outage_risk domain.
type RiskAnalysis struct {
	*models.OutageRisk
	// SizeTrend is nil until MinSizeTrendSpan of size history exists.
	SizeTrend *SizeTrend `json:"size_trend,omitempty"`
}

// SizeTrend is a linear fit of cluster size over SizeTrendWindow.
type SizeTrend struct {
	CurrentBytes      int64         `json:"current_bytes"`
	GrowthBytesPerDay float64       `json:"growth_bytes_per_day"`
	Span              time.Duration `json:"span_ns"`
	Samples           int           `json:"samples"`
	// GrowingTables are the tables that grew most since a snapshot at least
	// TableGrowthBaselineAge old, when table statistics allow the comparison.
	GrowingTables []TableGrowth `json:"growing_tables,omitempty"`
}

// TableGrowth is how fast one table (with its indexes) is growing.
type TableGrowth struct {
	SchemaName   string  `json:"schemaname"`
	RelName      string  `json:"relname"`
	CurrentBytes int64   `json:"current_bytes"`
	GrowthPerDay float64 `json:"growth_bytes_per_day"`
}

// ConnectionPeakAnalysis is the highest connection utilization over ConnectionPeakWindow.
type ConnectionPeakAnalysis struct {
	models.ConnectionPeak
	Window      time.Duration `json:"window_ns"`
	Utilization float64       `json:"utilization"` // 0-1
}

// analyzeRisk reads the outage-risk document and fits the size trend.
func (a *MainAnalyzer) analyzeRisk(ctx context.Context, instanceID, snapshotID int64, coverage map[string]DomainCoverage) (*RiskAnalysis, error) {
	risk, err := a.storage.GetOutageRisk(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting outage risk: %w", err)
	}
	if risk == nil {
		return nil, nil
	}
	analysis := &RiskAnalysis{OutageRisk: risk}

	now := coverage[DomainOutageRisk].CapturedAt
	if now.IsZero() {
		now = time.Now()
	}
	history, err := a.storage.GetSizeHistory(ctx, instanceID, now.Add(-SizeTrendWindow))
	if err != nil {
		return nil, fmt.Errorf("getting size history: %w", err)
	}
	analysis.SizeTrend = fitSizeTrend(history)
	if analysis.SizeTrend != nil && coverage[DomainTableStats].Usable() {
		growing, err := a.growingTables(ctx, instanceID, coverage[DomainTableStats].SnapshotID, now)
		if err != nil {
			return nil, fmt.Errorf("comparing table sizes: %w", err)
		}
		analysis.SizeTrend.GrowingTables = growing
	}
	return analysis, nil
}

// fitSizeTrend fits a least-squares line through the samples. It returns nil
// when they span less than MinSizeTrendSpan.
func fitSizeTrend(samples []models.SizeSample) *SizeTrend {
	if len(samples) < 2 {
		return nil
	}
	first, last := samples[0].CapturedAt, samples[len(samples)-1].CapturedAt
	span := last.Sub(first)
	if span < MinSizeTrendSpan {
		return nil
	}

	// x in days since the first sample keeps the numbers well conditioned.
	n := float64(len(samples))
	var sumX, sumY, sumXY, sumXX float64
	for _, s := range samples {
		x := s.CapturedAt.Sub(first).Hours() / 24
		y := float64(s.ClusterBytes)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return nil
	}
	return &SizeTrend{
		CurrentBytes:      samples[len(samples)-1].ClusterBytes,
		GrowthBytesPerDay: (n*sumXY - sumX*sumY) / denom,
		Span:              span,
		Samples:           len(samples),
	}
}

// growingTables compares the latest table sizes with a snapshot at least
// TableGrowthBaselineAge older and returns the fastest growers.
func (a *MainAnalyzer) growingTables(ctx context.Context, instanceID, latestID int64, now time.Time) ([]TableGrowth, error) {
	baseline, err := a.storage.GetLatestSnapshotWithCollector(ctx, instanceID, DomainTableStats, now.Add(-TableGrowthBaselineAge))
	if err != nil || baseline == nil || baseline.ID == latestID {
		return nil, err
	}
	latestSnap, err := a.storage.GetSnapshotByID(ctx, latestID)
	if err != nil || latestSnap == nil {
		return nil, err
	}
	days := latestSnap.CapturedAt.Sub(baseline.CapturedAt).Hours() / 24
	if days <= 0 {
		return nil, nil
	}

	before, err := a.storage.GetTableStats(ctx, baseline.ID)
	if err != nil {
		return nil, err
	}
	after, err := a.storage.GetTableStats(ctx, latestID)
	if err != nil {
		return nil, err
	}
	old := make(map[string]int64, len(before))
	for _, t := range before {
		old[t.SchemaName+"."+t.RelName] = t.TableSize + t.IndexSize
	}

	var growing []TableGrowth
	for _, t := range after {
		prev, ok := old[t.SchemaName+"."+t.RelName]
		size := t.TableSize + t.IndexSize
		if !ok || size <= prev {
			continue
		}
		growing = append(growing, TableGrowth{
			SchemaName:   t.SchemaName,
			RelName:      t.RelName,
			CurrentBytes: size,
			GrowthPerDay: float64(size-prev) / days,
		})
	}
	sort.Slice(growing, func(i, j int) bool { return growing[i].GrowthPerDay > growing[j].GrowthPerDay })
	if len(growing) > maxGrowingTables {
		growing = growing[:maxGrowingTables]
	}
	return growing, nil
}

// analyzeConnectionPeak finds the busiest activity sample in the last
// ConnectionPeakWindow before the activity snapshot.
func (a *MainAnalyzer) analyzeConnectionPeak(ctx context.Context, instanceID int64, until time.Time) (*ConnectionPeakAnalysis, error) {
	peak, err := a.storage.GetConnectionPeak(ctx, instanceID, until.Add(-ConnectionPeakWindow), until)
	if err != nil || peak == nil {
		return nil, err
	}
	return &ConnectionPeakAnalysis{
		ConnectionPeak: *peak,
		Window:         ConnectionPeakWindow,
		Utilization:    float64(peak.TotalConnections) / float64(peak.MaxConnections),
	}, nil
}
