package analyzer

import (
	"context"
	"sort"

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

// Analyze identifies slow queries from the given snapshot.
// It uses both absolute values (from the snapshot) and delta values
// (compared to previous snapshot) for analysis.
func (a *SlowQueryAnalyzer) Analyze(ctx context.Context, snapshotID int64) ([]SlowQuery, error) {
	// Get current snapshot's query stats
	stats, err := a.storage.GetQueryStats(ctx, snapshotID)
	if err != nil {
		return nil, err
	}

	if len(stats) == 0 {
		return nil, nil
	}

	// Get snapshot info to find previous snapshot for delta calculation
	snapshot, err := a.storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}

	// Try to get delta with previous snapshot
	var deltas map[int64]models.QueryStatDelta
	snapshots, err := a.storage.ListSnapshots(ctx, snapshot.InstanceID, 2)
	if err == nil && len(snapshots) >= 2 {
		// snapshots[0] is the current (latest), snapshots[1] is the previous
		previousSnapshotID := snapshots[1].ID
		if previousSnapshotID != snapshotID {
			deltaList, err := a.storage.GetQueryStatsDelta(ctx, previousSnapshotID, snapshotID)
			if err == nil {
				deltas = make(map[int64]models.QueryStatDelta, len(deltaList))
				for _, d := range deltaList {
					deltas[d.QueryID] = d
				}
			}
		}
	}

	// Find slow queries
	var slowQueries []SlowQuery
	thresholdMs := a.config.SlowQueryMs

	for _, stat := range stats {
		// Check if mean execution time exceeds threshold
		if stat.MeanExecTime < thresholdMs {
			continue
		}

		sq := SlowQuery{
			QueryID:       stat.QueryID,
			Query:         stat.Query,
			MeanExecTime:  stat.MeanExecTime,
			MaxExecTime:   stat.MaxExecTime,
			TotalExecTime: stat.TotalExecTime,
			Calls:         stat.Calls,
		}

		// Calculate cache hit ratio for this query
		totalBlks := stat.SharedBlksHit + stat.SharedBlksRead
		if totalBlks > 0 {
			sq.CacheHitRatio = float64(stat.SharedBlksHit) / float64(totalBlks)
		}

		// Calculate average rows per call
		if stat.Calls > 0 {
			sq.AvgRows = float64(stat.Rows) / float64(stat.Calls)
		}

		// Add delta values if available
		if delta, ok := deltas[stat.QueryID]; ok {
			sq.DeltaCalls = delta.DeltaCalls
			sq.DeltaTotalTime = delta.DeltaTotalTime
			if delta.DeltaCalls > 0 {
				sq.DeltaMeanExecTime = delta.DeltaTotalTime / float64(delta.DeltaCalls)
			}
		}

		slowQueries = append(slowQueries, sq)
	}

	// Sort by total execution time (most impactful first)
	sort.Slice(slowQueries, func(i, j int) bool {
		return slowQueries[i].TotalExecTime > slowQueries[j].TotalExecTime
	})

	return slowQueries, nil
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

	var slowQueries []SlowQuery
	thresholdMs := a.config.SlowQueryMs

	for _, delta := range deltas {
		// Skip queries with no calls in this period
		if delta.DeltaCalls == 0 {
			continue
		}

		meanExecTime := delta.MeanExecTime
		if meanExecTime < thresholdMs {
			continue
		}

		sq := SlowQuery{
			QueryID:           delta.QueryID,
			Query:             delta.Query,
			MeanExecTime:      meanExecTime,
			TotalExecTime:     delta.DeltaTotalTime,
			Calls:             delta.DeltaCalls,
			CacheHitRatio:     delta.CacheHitRatio,
			AvgRows:           delta.AvgRowsPerCall,
			DeltaCalls:        delta.DeltaCalls,
			DeltaTotalTime:    delta.DeltaTotalTime,
			DeltaMeanExecTime: meanExecTime,
		}

		slowQueries = append(slowQueries, sq)
	}

	// Sort by total execution time in the period
	sort.Slice(slowQueries, func(i, j int) bool {
		return slowQueries[i].TotalExecTime > slowQueries[j].TotalExecTime
	})

	return slowQueries, nil
}
