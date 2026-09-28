package analyzer

import (
	"context"
	"sort"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// SlowQueryAnalyzer identifies queries that exceed execution time thresholds.
type SlowQueryAnalyzer struct {
	storage Storage
	config  *Config
}

// NewSlowQueryAnalyzer creates a new SlowQueryAnalyzer.
func NewSlowQueryAnalyzer(storage Storage, cfg *Config) *SlowQueryAnalyzer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &SlowQueryAnalyzer{
		storage: storage,
		config:  cfg,
	}
}

// Analyze identifies slow queries as of the given snapshot.
//
// Queries are judged by their mean time over the configured window, measured
// against the newest query_stats snapshot at least that old. pg_stat_statements
// counters are cumulative since the last reset, so a lifetime mean would take
// weeks to reflect a regression and would keep flagging a query long after it
// was fixed. Until the window's worth of history exists, the lifetime figures
// are the only ones available and are used instead.
func (a *SlowQueryAnalyzer) Analyze(ctx context.Context, snapshotID int64) ([]SlowQuery, error) {
	stats, err := a.storage.GetQueryStats(ctx, snapshotID)
	if err != nil {
		return nil, err
	}

	if len(stats) == 0 {
		return nil, nil
	}

	snapshot, err := a.storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}

	window := a.config.SlowQueryWindow
	if window <= 0 {
		window = DefaultSlowQueryWindow
	}
	baseline, err := a.storage.GetLatestSnapshotWithCollector(ctx, snapshot.InstanceID, DomainQueryStats, snapshot.CapturedAt.Add(-window))
	if err != nil {
		return nil, err
	}
	if baseline == nil || baseline.ID == snapshotID {
		return a.analyzeCumulative(stats), nil
	}

	deltas, err := a.storage.GetQueryStatsDelta(ctx, baseline.ID, snapshotID)
	if err != nil {
		return nil, err
	}
	return a.analyzeDeltas(deltas, stats, snapshot.CapturedAt.Sub(baseline.CapturedAt)), nil
}

// analyzeCumulative judges queries by their figures since statistics were reset.
func (a *SlowQueryAnalyzer) analyzeCumulative(stats []models.QueryStat) []SlowQuery {
	var slowQueries []SlowQuery
	for _, stat := range stats {
		if stat.MeanExecTime < a.config.SlowQueryMs {
			continue
		}

		sq := SlowQuery{
			QueryID:              stat.QueryID,
			Query:                stat.Query,
			MeanExecTime:         stat.MeanExecTime,
			MaxExecTime:          stat.MaxExecTime,
			TotalExecTime:        stat.TotalExecTime,
			Calls:                stat.Calls,
			LifetimeMeanExecTime: stat.MeanExecTime,
		}

		totalBlks := stat.SharedBlksHit + stat.SharedBlksRead
		if totalBlks > 0 {
			sq.CacheHitRatio = float64(stat.SharedBlksHit) / float64(totalBlks)
		}
		if stat.Calls > 0 {
			sq.AvgRows = float64(stat.Rows) / float64(stat.Calls)
		}

		slowQueries = append(slowQueries, sq)
	}

	sortByTotalTime(slowQueries)
	return slowQueries
}

// analyzeDeltas judges queries by their activity within a window. Queries that
// did not run in the window are not reported: there is nothing recent to fix.
// current supplies the lifetime figures shown alongside, and may be nil.
func (a *SlowQueryAnalyzer) analyzeDeltas(deltas []models.QueryStatDelta, current []models.QueryStat, window time.Duration) []SlowQuery {
	lifetime := make(map[int64]models.QueryStat, len(current))
	for _, stat := range current {
		lifetime[stat.QueryID] = stat
	}

	var slowQueries []SlowQuery
	for _, delta := range deltas {
		if delta.DeltaCalls == 0 {
			continue
		}
		if delta.MeanExecTime < a.config.SlowQueryMs {
			continue
		}

		sq := SlowQuery{
			QueryID:           delta.QueryID,
			Query:             delta.Query,
			MeanExecTime:      delta.MeanExecTime,
			TotalExecTime:     delta.DeltaTotalTime,
			Calls:             delta.DeltaCalls,
			CacheHitRatio:     delta.CacheHitRatio,
			AvgRows:           delta.AvgRowsPerCall,
			DeltaCalls:        delta.DeltaCalls,
			DeltaTotalTime:    delta.DeltaTotalTime,
			DeltaMeanExecTime: delta.MeanExecTime,
			Window:            window,
		}
		// pg_stat_statements keeps no per-interval maximum, so max is lifetime.
		if stat, ok := lifetime[delta.QueryID]; ok {
			sq.MaxExecTime = stat.MaxExecTime
			sq.LifetimeMeanExecTime = stat.MeanExecTime
		}

		slowQueries = append(slowQueries, sq)
	}

	sortByTotalTime(slowQueries)
	return slowQueries
}

// AnalyzeWithDeltas identifies slow queries using delta values between two snapshots.
// This is useful for analyzing recent performance (e.g., last hour, last day).
func (a *SlowQueryAnalyzer) AnalyzeWithDeltas(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]SlowQuery, error) {
	deltas, err := a.storage.GetQueryStatsDelta(ctx, fromSnapshotID, toSnapshotID)
	if err != nil {
		return nil, err
	}

	if len(deltas) == 0 {
		return nil, nil
	}

	var window time.Duration
	from, err := a.storage.GetSnapshotByID(ctx, fromSnapshotID)
	if err != nil {
		return nil, err
	}
	to, err := a.storage.GetSnapshotByID(ctx, toSnapshotID)
	if err != nil {
		return nil, err
	}
	if from != nil && to != nil {
		window = to.CapturedAt.Sub(from.CapturedAt)
	}

	current, err := a.storage.GetQueryStats(ctx, toSnapshotID)
	if err != nil {
		return nil, err
	}

	return a.analyzeDeltas(deltas, current, window), nil
}

// sortByTotalTime orders queries by total execution time, most impactful first.
func sortByTotalTime(queries []SlowQuery) {
	sort.Slice(queries, func(i, j int) bool {
		return queries[i].TotalExecTime > queries[j].TotalExecTime
	})
}
