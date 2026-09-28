package scheduler

import (
	"context"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/metrics"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// runCollectionLoop runs the collection job at the minimum collector interval.
// This ensures collectors with shorter intervals are triggered appropriately.
func (s *Scheduler) runCollectionLoop(ctx context.Context, run *schedulerRun) {
	defer run.wg.Done()

	// Use minimum collector interval, falling back to configured snapshot interval
	interval := s.coordinator.MinInterval()
	if interval <= 0 {
		interval = s.config.SnapshotInterval.Duration()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Printf("[scheduler] collection loop started with interval %v (based on minimum collector interval)", interval)

	// Run initial collection
	s.executeCollection(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("[scheduler] collection loop stopping due to context cancellation")
			return
		case <-run.stopCh:
			s.logger.Printf("[scheduler] collection loop stopping")
			return
		case <-ticker.C:
			s.executeCollection(ctx)
		}
	}
}

// runAnalysisLoop runs the analysis job at the configured interval.
func (s *Scheduler) runAnalysisLoop(ctx context.Context, run *schedulerRun) {
	defer run.wg.Done()

	interval := s.config.AnalysisInterval.Duration()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Printf("[scheduler] analysis loop started with interval %v", interval)

	// Run initial analysis after a short delay to allow initial collection
	// Use a timer that respects the stop channel
	initialDelay := time.NewTimer(interval / 2)
	defer initialDelay.Stop()

	select {
	case <-ctx.Done():
		s.logger.Printf("[scheduler] analysis loop stopping due to context cancellation")
		return
	case <-run.stopCh:
		s.logger.Printf("[scheduler] analysis loop stopping")
		return
	case <-initialDelay.C:
		s.executeAnalysis(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("[scheduler] analysis loop stopping due to context cancellation")
			return
		case <-run.stopCh:
			s.logger.Printf("[scheduler] analysis loop stopping")
			return
		case <-ticker.C:
			s.executeAnalysis(ctx)
		}
	}
}

// runMaintenanceLoop runs the maintenance job daily.
func (s *Scheduler) runMaintenanceLoop(ctx context.Context, run *schedulerRun) {
	defer run.wg.Done()

	// Run maintenance daily
	interval := 24 * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Printf("[scheduler] maintenance loop started with interval %v", interval)

	// Run initial maintenance after 1 hour to avoid startup load
	initialDelay := time.NewTimer(1 * time.Hour)
	defer initialDelay.Stop()

	select {
	case <-ctx.Done():
		return
	case <-run.stopCh:
		return
	case <-initialDelay.C:
		s.executeMaintenance(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("[scheduler] maintenance loop stopping due to context cancellation")
			return
		case <-run.stopCh:
			s.logger.Printf("[scheduler] maintenance loop stopping")
			return
		case <-ticker.C:
			s.executeMaintenance(ctx)
		}
	}
}

// executeCollection runs a collection cycle and updates health status.
func (s *Scheduler) executeCollection(ctx context.Context) {
	start := time.Now()

	result, err := s.runCollection(ctx)

	duration := time.Since(start)
	success := err == nil && (result == nil || !result.HasErrors())

	var errMsg string
	if err != nil {
		errMsg = err.Error()
	} else if result != nil && result.HasErrors() {
		errMsg = result.Error().Error()
	}

	s.updateCollectionHealth(success, duration, errMsg)

	if success {
		s.logger.Printf("[scheduler] collection completed in %v (snapshot_id=%d)",
			duration, result.SnapshotID)
	} else {
		s.logger.Printf("[scheduler] collection failed in %v: %s", duration, errMsg)
	}
}

// executeAnalysis runs an analysis cycle and updates health status.
func (s *Scheduler) executeAnalysis(ctx context.Context) {
	start := time.Now()

	// Get the latest snapshot for analysis
	snapshot, err := s.coordinator.GetLatestSnapshot(ctx)
	if err != nil {
		s.updateAnalysisHealth(false, time.Since(start), err.Error())
		s.logger.Printf("[scheduler] failed to get latest snapshot: %v", err)
		return
	}
	if snapshot == nil {
		s.updateAnalysisHealth(false, time.Since(start), "no snapshots available")
		s.logger.Printf("[scheduler] no snapshots available for analysis")
		return
	}

	result, suggestResult, err := s.runAnalysis(ctx, snapshot.ID)

	duration := time.Since(start)
	success := err == nil

	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}

	s.updateAnalysisHealth(success, duration, errMsg)
	s.recordAnalysisMetrics(ctx, duration, success, result)

	if success {
		issueCount := 0
		if result != nil {
			issueCount = result.GetIssueCount()
		}
		suggestCount := 0
		if suggestResult != nil {
			suggestCount = suggestResult.TotalSuggestions
		}
		skipped := 0
		if suggestResult != nil {
			skipped = len(suggestResult.SkippedRules)
		}
		s.logger.Printf("[scheduler] analysis completed in %v (issues=%d, suggestions=%d, skipped_rules=%d)",
			duration, issueCount, suggestCount, skipped)
	} else {
		s.logger.Printf("[scheduler] analysis failed in %v: %s", duration, errMsg)
	}
}

// recordAnalysisMetrics publishes the Prometheus gauges and counters describing an
// analysis run. Failures here are non-fatal - metrics must never break analysis.
func (s *Scheduler) recordAnalysisMetrics(ctx context.Context, duration time.Duration, success bool, result *analyzer.AnalysisResult) {
	issues := map[string]int{}
	if result != nil {
		issues[models.SeverityCritical] = result.GetCriticalCount()
		issues[models.SeverityWarning] = result.GetWarningCount()
	}
	metrics.RecordAnalysis(duration.Seconds(), success, issues)

	if result != nil {
		cacheRatio, queryCount := 0.0, 0
		if result.CacheStats != nil {
			// CacheStats reports 0-100; the gauge is documented as 0-1.
			cacheRatio = result.CacheStats.OverallHitRatio / 100
			queryCount = result.CacheStats.TrackedQueries
		}
		metrics.UpdateDatabaseMetrics(cacheRatio, queryCount, len(result.SlowQueries))
	}

	stats, err := s.suggester.GetSuggestionStats(ctx, s.instanceID)
	if err != nil {
		s.logger.Printf("[scheduler] failed to read suggestion stats for metrics: %v", err)
		return
	}
	metrics.UpdateSuggestionMetrics(map[string]map[string]int{
		models.SeverityCritical: {models.StatusActive: stats.Critical},
		models.SeverityWarning:  {models.StatusActive: stats.Warning},
		models.SeverityInfo:     {models.StatusActive: stats.Info},
	})
}

// executeMaintenance runs a maintenance cycle.
func (s *Scheduler) executeMaintenance(ctx context.Context) {
	s.logger.Printf("[scheduler] running maintenance...")

	start := time.Now()
	success := true

	// Purge old snapshots
	retention := s.retention.Snapshots.Duration()
	purged, err := s.storage.PurgeOldSnapshots(ctx, retention)
	if err != nil {
		s.logger.Printf("[scheduler] failed to purge old snapshots: %v", err)
		success = false
	} else if purged > 0 {
		s.logger.Printf("[scheduler] purged %d old snapshots (retention=%v)", purged, retention)
	}

	queryRetention := s.retention.QueryStats.Duration()
	queryPurged, err := s.storage.PurgeOldQueryHistory(ctx, queryRetention)
	if err != nil {
		s.logger.Printf("[scheduler] failed to purge query history: %v", err)
		success = false
	} else if queryPurged > 0 {
		s.logger.Printf("[scheduler] purged %d query samples (retention=%v)", queryPurged, queryRetention)
	}

	// Size samples outlive snapshots so disk growth can be forecast.
	if sizeRetention := s.retention.SizeHistory.Duration(); sizeRetention > 0 {
		sizePurged, err := s.storage.PurgeOldSizeHistory(ctx, sizeRetention)
		if err != nil {
			s.logger.Printf("[scheduler] failed to purge size history: %v", err)
			success = false
		} else if sizePurged > 0 {
			s.logger.Printf("[scheduler] purged %d size samples (retention=%v)", sizePurged, sizeRetention)
		}
	}

	s.updateMaintenanceHealth(success)
	s.logger.Printf("[scheduler] maintenance completed in %v", time.Since(start))
}

// runCollection executes a collection via the coordinator.
func (s *Scheduler) runCollection(ctx context.Context) (*collector.CollectionResult, error) {
	// Use a timeout for collection to prevent hanging
	// Use minimum collector interval or snapshot interval, whichever is smaller
	interval := s.coordinator.MinInterval()
	if interval <= 0 {
		interval = s.config.SnapshotInterval.Duration()
	}
	timeout := max(interval/2, 30*time.Second)

	return s.coordinator.CollectWithTimeout(ctx, timeout)
}

// runAnalysis executes analysis and suggestion generation.
func (s *Scheduler) runAnalysis(ctx context.Context, snapshotID int64) (*analyzer.AnalysisResult, *suggester.SuggestResult, error) {
	// Run analyzer
	analysisResult, err := s.analyzer.Analyze(ctx, snapshotID)
	if err != nil {
		return nil, nil, err
	}

	// Run suggester
	suggestResult, err := s.suggester.Suggest(ctx, analysisResult)
	if err != nil {
		// Return analysis result even if suggester fails
		return analysisResult, nil, err
	}

	return analysisResult, suggestResult, nil
}
