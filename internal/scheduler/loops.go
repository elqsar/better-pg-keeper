package scheduler

import (
	"context"
	"time"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/suggester"
)

// runCollectionLoop runs the collection job at the configured interval.
func (s *Scheduler) runCollectionLoop(ctx context.Context) {
	defer s.wg.Done()

	interval := s.config.SnapshotInterval.Duration()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Printf("[scheduler] collection loop started with interval %v", interval)

	// Run initial collection
	s.executeCollection(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("[scheduler] collection loop stopping due to context cancellation")
			return
		case <-s.stopCh:
			s.logger.Printf("[scheduler] collection loop stopping")
			return
		case <-ticker.C:
			s.executeCollection(ctx)
		}
	}
}

// runAnalysisLoop runs the analysis job at the configured interval.
func (s *Scheduler) runAnalysisLoop(ctx context.Context) {
	defer s.wg.Done()

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
	case <-s.stopCh:
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
		case <-s.stopCh:
			s.logger.Printf("[scheduler] analysis loop stopping")
			return
		case <-ticker.C:
			s.executeAnalysis(ctx)
		}
	}
}

// runMaintenanceLoop runs the maintenance job daily.
func (s *Scheduler) runMaintenanceLoop(ctx context.Context) {
	defer s.wg.Done()

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
	case <-s.stopCh:
		return
	case <-initialDelay.C:
		s.executeMaintenance(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("[scheduler] maintenance loop stopping due to context cancellation")
			return
		case <-s.stopCh:
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

	if success {
		issueCount := 0
		if result != nil {
			issueCount = result.GetIssueCount()
		}
		suggestCount := 0
		if suggestResult != nil {
			suggestCount = suggestResult.TotalSuggestions
		}
		s.logger.Printf("[scheduler] analysis completed in %v (issues=%d, suggestions=%d)",
			duration, issueCount, suggestCount)
	} else {
		s.logger.Printf("[scheduler] analysis failed in %v: %s", duration, errMsg)
	}
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

	s.updateMaintenanceHealth(success)
	s.logger.Printf("[scheduler] maintenance completed in %v", time.Since(start))
}

// runCollection executes a collection via the coordinator.
func (s *Scheduler) runCollection(ctx context.Context) (*collector.CollectionResult, error) {
	// Use a timeout for collection to prevent hanging
	timeout := s.config.SnapshotInterval.Duration() / 2
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}

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
