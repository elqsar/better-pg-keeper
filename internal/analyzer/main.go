package analyzer

import (
	"context"
	"fmt"
	"time"
)

// MainAnalyzer orchestrates all sub-analyzers to produce a complete analysis.
type MainAnalyzer struct {
	storage         Storage
	config          *Config
	slowQueryAnalyzer *SlowQueryAnalyzer
	cacheAnalyzer     *CacheAnalyzer
	tableAnalyzer     *TableAnalyzer
	indexAnalyzer     *IndexAnalyzer
}

// NewMainAnalyzer creates a new MainAnalyzer with all sub-analyzers.
func NewMainAnalyzer(storage Storage, cfg *Config) *MainAnalyzer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &MainAnalyzer{
		storage:           storage,
		config:            cfg,
		slowQueryAnalyzer: NewSlowQueryAnalyzer(storage, cfg),
		cacheAnalyzer:     NewCacheAnalyzer(storage, cfg),
		tableAnalyzer:     NewTableAnalyzer(storage, cfg),
		indexAnalyzer:     NewIndexAnalyzer(storage, cfg),
	}
}

// Analyze runs all sub-analyzers and aggregates results.
// It handles partial failures gracefully - if one analyzer fails,
// others will still run and their results will be included.
func (a *MainAnalyzer) Analyze(ctx context.Context, snapshotID int64) (*AnalysisResult, error) {
	// Get snapshot info
	snapshot, err := a.storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot %d not found", snapshotID)
	}

	result := &AnalysisResult{
		SnapshotID: snapshotID,
		InstanceID: snapshot.InstanceID,
		AnalyzedAt: time.Now(),
	}

	// Run slow query analysis
	slowQueries, err := a.slowQueryAnalyzer.Analyze(ctx, snapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("slow query analysis: %v", err))
		result.ErrorCount++
	} else {
		result.SlowQueries = slowQueries
	}

	// Run cache analysis
	cacheStats, err := a.cacheAnalyzer.Analyze(ctx, snapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("cache analysis: %v", err))
		result.ErrorCount++
	} else {
		result.CacheStats = cacheStats
	}

	// Run table analysis
	tableIssues, err := a.tableAnalyzer.Analyze(ctx, snapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("table analysis: %v", err))
		result.ErrorCount++
	} else {
		result.TableIssues = tableIssues
	}

	// Run index analysis
	indexIssues, err := a.indexAnalyzer.Analyze(ctx, snapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("index analysis: %v", err))
		result.ErrorCount++
	} else {
		result.IndexIssues = indexIssues
	}

	return result, nil
}

// AnalyzeWithTimeRange runs analysis using delta values between two snapshots.
// This is useful for analyzing recent performance over a specific time window.
func (a *MainAnalyzer) AnalyzeWithTimeRange(ctx context.Context, fromSnapshotID, toSnapshotID int64) (*AnalysisResult, error) {
	// Get target snapshot info
	snapshot, err := a.storage.GetSnapshotByID(ctx, toSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot %d not found", toSnapshotID)
	}

	result := &AnalysisResult{
		SnapshotID: toSnapshotID,
		InstanceID: snapshot.InstanceID,
		AnalyzedAt: time.Now(),
	}

	// Run slow query analysis with deltas
	slowQueries, err := a.slowQueryAnalyzer.AnalyzeWithDeltas(ctx, fromSnapshotID, toSnapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("slow query analysis: %v", err))
		result.ErrorCount++
	} else {
		result.SlowQueries = slowQueries
	}

	// Run cache analysis with deltas
	cacheStats, err := a.cacheAnalyzer.AnalyzeWithDeltas(ctx, fromSnapshotID, toSnapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("cache analysis: %v", err))
		result.ErrorCount++
	} else {
		result.CacheStats = cacheStats
	}

	// Table and index analysis use the latest snapshot (not deltas)
	tableIssues, err := a.tableAnalyzer.Analyze(ctx, toSnapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("table analysis: %v", err))
		result.ErrorCount++
	} else {
		result.TableIssues = tableIssues
	}

	indexIssues, err := a.indexAnalyzer.Analyze(ctx, toSnapshotID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("index analysis: %v", err))
		result.ErrorCount++
	} else {
		result.IndexIssues = indexIssues
	}

	return result, nil
}

// GetIssueCount returns the total count of all issues found.
func (r *AnalysisResult) GetIssueCount() int {
	count := len(r.SlowQueries) + len(r.TableIssues) + len(r.IndexIssues)
	if r.CacheStats != nil {
		count += len(r.CacheStats.PoorCacheQueries)
		if r.CacheStats.BelowThreshold {
			count++
		}
	}
	return count
}

// GetCriticalCount returns the count of critical severity issues.
func (r *AnalysisResult) GetCriticalCount() int {
	count := 0

	for _, issue := range r.TableIssues {
		if issue.Severity == "critical" {
			count++
		}
	}

	for _, issue := range r.IndexIssues {
		if issue.Severity == "critical" {
			count++
		}
	}

	return count
}

// GetWarningCount returns the count of warning severity issues.
func (r *AnalysisResult) GetWarningCount() int {
	count := 0

	// All slow queries are considered warnings
	count += len(r.SlowQueries)

	// Cache below threshold is a warning
	if r.CacheStats != nil && r.CacheStats.BelowThreshold {
		count++
	}

	for _, issue := range r.TableIssues {
		if issue.Severity == "warning" {
			count++
		}
	}

	for _, issue := range r.IndexIssues {
		if issue.Severity == "warning" {
			count++
		}
	}

	return count
}

// Ensure MainAnalyzer implements Analyzer interface.
var _ Analyzer = (*MainAnalyzer)(nil)
