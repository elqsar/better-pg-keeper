package analyzer

import (
	"context"
	"fmt"
	"sort"
)

// CacheAnalyzer analyzes cache hit ratios at database and query levels.
type CacheAnalyzer struct {
	storage Storage
	config  *Config
}

// NewCacheAnalyzer creates a new CacheAnalyzer.
func NewCacheAnalyzer(storage Storage, cfg *Config) *CacheAnalyzer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &CacheAnalyzer{
		storage: storage,
		config:  cfg,
	}
}

// Analyze calculates cache statistics for the given snapshot.
// Returns overall database-level cache hit ratio and identifies queries
// with poor cache performance.
func (a *CacheAnalyzer) Analyze(ctx context.Context, snapshotID int64) (*CacheAnalysis, error) {
	result, err := a.AnalyzeOverall(ctx, snapshotID)
	if err != nil || result == nil {
		return result, err
	}
	if err := a.AddQueryStats(ctx, snapshotID, result); err != nil {
		return nil, err
	}
	return result, nil
}

// AnalyzeOverall reads the database cache ratio from its own collector snapshot.
func (a *CacheAnalyzer) AnalyzeOverall(ctx context.Context, snapshotID int64) (*CacheAnalysis, error) {
	// Get snapshot to retrieve database-level cache hit ratio
	snapshot, err := a.storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}

	result := &CacheAnalysis{
		Threshold: a.config.CacheHitRatioWarning * 100, // Convert to percentage for display
	}

	// Set overall hit ratio from snapshot (already stored as percentage 0-100)
	if snapshot.CacheHitRatio == nil {
		return nil, fmt.Errorf("snapshot %d has no database cache ratio", snapshotID)
	}
	result.OverallHitRatio = *snapshot.CacheHitRatio
	result.BelowThreshold = *snapshot.CacheHitRatio < (a.config.CacheHitRatioWarning * 100)
	return result, nil
}

// AddQueryStats enriches an overall cache result with query-level details.
func (a *CacheAnalyzer) AddQueryStats(ctx context.Context, snapshotID int64, result *CacheAnalysis) error {
	stats, err := a.storage.GetQueryStats(ctx, snapshotID)
	if err != nil {
		return err
	}

	result.TrackedQueries = len(stats)

	// Identify queries with poor cache hit ratio
	threshold := a.config.CacheHitRatioWarning // 0-1 scale for per-query calculation

	for _, stat := range stats {
		totalBlks := stat.SharedBlksHit + stat.SharedBlksRead
		if totalBlks == 0 {
			continue // No block reads, skip
		}

		hitRatio := float64(stat.SharedBlksHit) / float64(totalBlks)
		if hitRatio >= threshold {
			continue // Good cache performance, skip
		}

		// Only flag queries with significant block activity (to avoid noise from tiny queries)
		if totalBlks < 100 {
			continue
		}

		result.PoorCacheQueries = append(result.PoorCacheQueries, PoorCacheQuery{
			QueryID:       stat.QueryID,
			Query:         stat.Query,
			CacheHitRatio: hitRatio,
			BlksHit:       stat.SharedBlksHit,
			BlksRead:      stat.SharedBlksRead,
			Calls:         stat.Calls,
		})
	}

	// Sort by cache hit ratio (worst first)
	sort.Slice(result.PoorCacheQueries, func(i, j int) bool {
		return result.PoorCacheQueries[i].CacheHitRatio < result.PoorCacheQueries[j].CacheHitRatio
	})

	return nil
}

// AnalyzeWithDeltas analyzes cache performance using delta values between snapshots.
// This provides more accurate recent cache performance analysis.
func (a *CacheAnalyzer) AnalyzeWithDeltas(ctx context.Context, fromSnapshotID, toSnapshotID int64) (*CacheAnalysis, error) {
	result, err := a.AnalyzeOverall(ctx, toSnapshotID)
	if err != nil || result == nil {
		return result, err
	}
	if err := a.AddQueryDeltas(ctx, fromSnapshotID, toSnapshotID, result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddQueryDeltas enriches an overall result with per-query interval statistics.
func (a *CacheAnalyzer) AddQueryDeltas(ctx context.Context, fromSnapshotID, toSnapshotID int64, result *CacheAnalysis) error {
	// Get delta stats for per-query analysis
	deltas, err := a.storage.GetQueryStatsDelta(ctx, fromSnapshotID, toSnapshotID)
	if err != nil {
		return err
	}

	threshold := a.config.CacheHitRatioWarning

	for _, delta := range deltas {
		if delta.DeltaCalls == 0 {
			continue
		}

		totalBlks := delta.DeltaBlksHit + delta.DeltaBlksRead
		if totalBlks == 0 || totalBlks < 100 {
			continue
		}

		hitRatio := float64(delta.DeltaBlksHit) / float64(totalBlks)
		if hitRatio >= threshold {
			continue
		}

		result.PoorCacheQueries = append(result.PoorCacheQueries, PoorCacheQuery{
			QueryID:       delta.QueryID,
			Query:         delta.Query,
			CacheHitRatio: hitRatio,
			BlksHit:       delta.DeltaBlksHit,
			BlksRead:      delta.DeltaBlksRead,
			Calls:         delta.DeltaCalls,
		})
	}

	sort.Slice(result.PoorCacheQueries, func(i, j int) bool {
		return result.PoorCacheQueries[i].CacheHitRatio < result.PoorCacheQueries[j].CacheHitRatio
	})

	return nil
}
