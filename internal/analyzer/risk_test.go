package analyzer

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestFitSizeTrend(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	linear := func(hours int, perDay float64) []models.SizeSample {
		var s []models.SizeSample
		for h := 0; h <= hours; h++ {
			s = append(s, models.SizeSample{
				CapturedAt:   start.Add(time.Duration(h) * time.Hour),
				ClusterBytes: int64(1e9 + perDay*float64(h)/24),
			})
		}
		return s
	}

	if got := fitSizeTrend(linear(12, 1e8)); got != nil {
		t.Errorf("12h of history should not produce a trend, got %+v", got)
	}
	if got := fitSizeTrend(nil); got != nil {
		t.Errorf("no history should not produce a trend, got %+v", got)
	}

	got := fitSizeTrend(linear(72, 1e8))
	if got == nil {
		t.Fatal("72h of history should produce a trend")
	}
	if math.Abs(got.GrowthBytesPerDay-1e8) > 1e3 {
		t.Errorf("growth = %.0f/day, want 1e8", got.GrowthBytesPerDay)
	}
	if got.Samples != 73 || got.Span != 72*time.Hour || got.CurrentBytes != int64(1e9+3e8) {
		t.Errorf("trend = %+v", got)
	}

	if got := fitSizeTrend(linear(48, -5e7)); got == nil || got.GrowthBytesPerDay >= 0 {
		t.Errorf("shrinking database should give a negative slope, got %+v", got)
	}
}

func TestMainAnalyzer_Risk(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	now := time.Now()

	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-48 * time.Hour)}
	storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	storage.tableStats[1] = []models.TableStat{
		{SchemaName: "public", RelName: "events", TableSize: 1 << 30},
		{SchemaName: "public", RelName: "users", TableSize: 1 << 20},
	}
	storage.tableStats[2] = []models.TableStat{
		{SchemaName: "public", RelName: "events", TableSize: 3 << 30},
		{SchemaName: "public", RelName: "users", TableSize: 1 << 20},
	}
	storage.outageRisk[2] = &models.OutageRisk{
		Databases: []models.DatabaseAge{{Name: "app", XIDAge: 1000}},
	}
	for h := 72; h >= 0; h-- {
		storage.sizeHistory = append(storage.sizeHistory, models.SizeSample{
			CapturedAt:   now.Add(-time.Duration(h) * time.Hour),
			ClusterBytes: int64(10<<30) - int64(h)*(1<<30)/24,
		})
	}

	result, err := NewMainAnalyzer(storage, nil).Analyze(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DomainCoverage(DomainOutageRisk).Usable() {
		t.Fatalf("outage_risk coverage = %+v", result.DomainCoverage(DomainOutageRisk))
	}
	if result.Risk == nil || len(result.Risk.Databases) != 1 {
		t.Fatalf("risk = %+v", result.Risk)
	}
	trend := result.Risk.SizeTrend
	if trend == nil {
		t.Fatal("expected a size trend")
	}
	if math.Abs(trend.GrowthBytesPerDay-(1<<30)) > 1<<20 {
		t.Errorf("growth = %.0f/day, want ~1GB", trend.GrowthBytesPerDay)
	}
	if len(trend.GrowingTables) != 1 || trend.GrowingTables[0].RelName != "events" {
		t.Fatalf("growing tables = %+v, want only events", trend.GrowingTables)
	}
	if g := trend.GrowingTables[0].GrowthPerDay; math.Abs(g-(1<<30)) > 1<<20 {
		t.Errorf("events growth = %.0f/day, want ~1GB (2GB over 2 days)", g)
	}
}

func TestMainAnalyzer_RiskAbsentIsUncovered(t *testing.T) {
	storage := newMockStorage()
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: time.Now()}

	result, err := NewMainAnalyzer(storage, nil).Analyze(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Risk != nil || result.DomainCoverage(DomainOutageRisk).Usable() {
		t.Errorf("no outage-risk data must leave the domain uncovered, got %+v / %+v",
			result.Risk, result.DomainCoverage(DomainOutageRisk))
	}
}
