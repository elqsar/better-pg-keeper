// Package scheduler provides job scheduling for data collection and analysis.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// Storage defines the storage interface needed by the scheduler.
type Storage interface {
	PurgeOldSnapshots(ctx context.Context, retention time.Duration) (int64, error)
}

// Scheduler coordinates data collection, analysis, and maintenance jobs.
type Scheduler struct {
	config      *config.SchedulerConfig
	retention   *config.RetentionConfig
	coordinator *collector.Coordinator
	analyzer    analyzer.Analyzer
	suggester   *suggester.Suggester
	storage     Storage
	instanceID  int64
	logger      *log.Logger

	// State management
	mu       sync.RWMutex
	running  atomic.Bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	manualMu sync.Mutex // Prevents concurrent manual triggers

	// Health status
	health *HealthStatus
}

// Config holds configuration for creating a Scheduler.
type Config struct {
	SchedulerConfig *config.SchedulerConfig
	RetentionConfig *config.RetentionConfig
	Coordinator     *collector.Coordinator
	Analyzer        analyzer.Analyzer
	Suggester       *suggester.Suggester
	Storage         Storage
	InstanceID      int64
	Logger          *log.Logger
}

// HealthStatus tracks the health of scheduled jobs.
type HealthStatus struct {
	mu                     sync.RWMutex
	LastCollectionTime     time.Time
	LastCollectionSuccess  bool
	LastCollectionError    string
	LastCollectionDuration time.Duration
	LastAnalysisTime       time.Time
	LastAnalysisSuccess    bool
	LastAnalysisError      string
	LastAnalysisDuration   time.Duration
	LastMaintenanceTime    time.Time
	LastMaintenanceSuccess bool
	TotalCollections       int64
	TotalAnalyses          int64
	FailedCollections      int64
	FailedAnalyses         int64
}

// HealthSnapshot returns a snapshot of the current health status.
type HealthSnapshot struct {
	LastCollectionTime     time.Time     `json:"last_collection_time"`
	LastCollectionSuccess  bool          `json:"last_collection_success"`
	LastCollectionError    string        `json:"last_collection_error,omitempty"`
	LastCollectionDuration time.Duration `json:"last_collection_duration"`
	LastAnalysisTime       time.Time     `json:"last_analysis_time"`
	LastAnalysisSuccess    bool          `json:"last_analysis_success"`
	LastAnalysisError      string        `json:"last_analysis_error,omitempty"`
	LastAnalysisDuration   time.Duration `json:"last_analysis_duration"`
	LastMaintenanceTime    time.Time     `json:"last_maintenance_time"`
	LastMaintenanceSuccess bool          `json:"last_maintenance_success"`
	TotalCollections       int64         `json:"total_collections"`
	TotalAnalyses          int64         `json:"total_analyses"`
	FailedCollections      int64         `json:"failed_collections"`
	FailedAnalyses         int64         `json:"failed_analyses"`
	IsRunning              bool          `json:"is_running"`
}

// NewScheduler creates a new Scheduler.
func NewScheduler(cfg Config) (*Scheduler, error) {
	if cfg.Coordinator == nil {
		return nil, fmt.Errorf("coordinator is required")
	}
	if cfg.Analyzer == nil {
		return nil, fmt.Errorf("analyzer is required")
	}
	if cfg.Suggester == nil {
		return nil, fmt.Errorf("suggester is required")
	}
	if cfg.Storage == nil {
		return nil, fmt.Errorf("storage is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	schedConfig := cfg.SchedulerConfig
	if schedConfig == nil {
		schedConfig = &config.SchedulerConfig{
			SnapshotInterval: config.Duration(5 * time.Minute),
			AnalysisInterval: config.Duration(15 * time.Minute),
		}
	}

	retentionConfig := cfg.RetentionConfig
	if retentionConfig == nil {
		retentionConfig = &config.RetentionConfig{
			Snapshots: config.Duration(168 * time.Hour), // 7 days
		}
	}

	return &Scheduler{
		config:      schedConfig,
		retention:   retentionConfig,
		coordinator: cfg.Coordinator,
		analyzer:    cfg.Analyzer,
		suggester:   cfg.Suggester,
		storage:     cfg.Storage,
		instanceID:  cfg.InstanceID,
		logger:      logger,
		stopCh:      make(chan struct{}),
		health:      &HealthStatus{},
	}, nil
}

// Start begins the scheduler's job execution.
// It returns immediately and runs jobs in the background.
func (s *Scheduler) Start(ctx context.Context) error {
	if s.running.Swap(true) {
		return fmt.Errorf("scheduler is already running")
	}

	s.mu.Lock()
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.logger.Printf("[scheduler] starting with snapshot_interval=%v, analysis_interval=%v",
		s.config.SnapshotInterval.Duration(), s.config.AnalysisInterval.Duration())

	// Start collection ticker
	s.wg.Add(1)
	go s.runCollectionLoop(ctx)

	// Start analysis ticker
	s.wg.Add(1)
	go s.runAnalysisLoop(ctx)

	// Start maintenance ticker (daily)
	s.wg.Add(1)
	go s.runMaintenanceLoop(ctx)

	s.logger.Printf("[scheduler] started")
	return nil
}

// Stop gracefully shuts down the scheduler.
// It waits for in-progress jobs to complete up to the timeout.
func (s *Scheduler) Stop() error {
	return s.StopWithTimeout(30 * time.Second)
}

// StopWithTimeout gracefully shuts down the scheduler with a custom timeout.
func (s *Scheduler) StopWithTimeout(timeout time.Duration) error {
	if !s.running.Swap(false) {
		return fmt.Errorf("scheduler is not running")
	}

	s.logger.Printf("[scheduler] stopping...")

	s.mu.Lock()
	close(s.stopCh)
	s.mu.Unlock()

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Printf("[scheduler] stopped gracefully")
		return nil
	case <-time.After(timeout):
		s.logger.Printf("[scheduler] stop timeout exceeded, forcing shutdown")
		return fmt.Errorf("stop timeout exceeded")
	}
}

// IsRunning returns true if the scheduler is currently running.
func (s *Scheduler) IsRunning() bool {
	return s.running.Load()
}

// TriggerSnapshot manually triggers a collection and analysis cycle.
// Unlike scheduled collection, this runs ALL collectors regardless of their intervals.
// Returns an error if a manual trigger is already in progress.
func (s *Scheduler) TriggerSnapshot(ctx context.Context) (*TriggerResult, error) {
	if !s.manualMu.TryLock() {
		return nil, fmt.Errorf("manual trigger already in progress")
	}
	defer s.manualMu.Unlock()

	s.logger.Printf("[scheduler] manual snapshot triggered")

	result := &TriggerResult{
		StartedAt: time.Now(),
	}

	// Run collection with all collectors (forced, ignoring intervals)
	collStart := time.Now()
	collResult, err := s.coordinator.CollectAll(ctx)
	collDuration := time.Since(collStart)
	result.CollectionResult = collResult

	// Update collection health
	collSuccess := err == nil && (collResult == nil || !collResult.HasErrors())
	var collErrMsg string
	if err != nil {
		collErrMsg = err.Error()
		result.CollectionError = collErrMsg
	} else if collResult != nil && collResult.HasErrors() {
		collErrMsg = collResult.Error().Error()
	}
	s.updateCollectionHealth(collSuccess, collDuration, collErrMsg)

	// Run analysis if collection succeeded
	if collResult != nil && collResult.SnapshotID > 0 {
		analysisStart := time.Now()
		analysisResult, suggestResult, err := s.runAnalysis(ctx, collResult.SnapshotID)
		analysisDuration := time.Since(analysisStart)
		result.AnalysisResult = analysisResult
		result.SuggestResult = suggestResult

		// Update analysis health
		analysisSuccess := err == nil
		var analysisErrMsg string
		if err != nil {
			analysisErrMsg = err.Error()
			result.AnalysisError = analysisErrMsg
		}
		s.updateAnalysisHealth(analysisSuccess, analysisDuration, analysisErrMsg)
	}

	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)

	s.logger.Printf("[scheduler] manual snapshot completed in %v", result.Duration)
	return result, nil
}

// TriggerResult contains the results of a manual trigger.
type TriggerResult struct {
	StartedAt        time.Time
	FinishedAt       time.Time
	Duration         time.Duration
	CollectionResult *collector.CollectionResult
	CollectionError  string
	AnalysisResult   *analyzer.AnalysisResult
	SuggestResult    *suggester.SuggestResult
	AnalysisError    string
}

// GetHealth returns a snapshot of the scheduler's health status.
func (s *Scheduler) GetHealth() *HealthSnapshot {
	s.health.mu.RLock()
	defer s.health.mu.RUnlock()

	return &HealthSnapshot{
		LastCollectionTime:     s.health.LastCollectionTime,
		LastCollectionSuccess:  s.health.LastCollectionSuccess,
		LastCollectionError:    s.health.LastCollectionError,
		LastCollectionDuration: s.health.LastCollectionDuration,
		LastAnalysisTime:       s.health.LastAnalysisTime,
		LastAnalysisSuccess:    s.health.LastAnalysisSuccess,
		LastAnalysisError:      s.health.LastAnalysisError,
		LastAnalysisDuration:   s.health.LastAnalysisDuration,
		LastMaintenanceTime:    s.health.LastMaintenanceTime,
		LastMaintenanceSuccess: s.health.LastMaintenanceSuccess,
		TotalCollections:       s.health.TotalCollections,
		TotalAnalyses:          s.health.TotalAnalyses,
		FailedCollections:      s.health.FailedCollections,
		FailedAnalyses:         s.health.FailedAnalyses,
		IsRunning:              s.running.Load(),
	}
}

// updateCollectionHealth updates health status after a collection.
func (s *Scheduler) updateCollectionHealth(success bool, duration time.Duration, errMsg string) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()

	s.health.LastCollectionTime = time.Now()
	s.health.LastCollectionSuccess = success
	s.health.LastCollectionError = errMsg
	s.health.LastCollectionDuration = duration
	s.health.TotalCollections++
	if !success {
		s.health.FailedCollections++
	}
}

// updateAnalysisHealth updates health status after an analysis.
func (s *Scheduler) updateAnalysisHealth(success bool, duration time.Duration, errMsg string) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()

	s.health.LastAnalysisTime = time.Now()
	s.health.LastAnalysisSuccess = success
	s.health.LastAnalysisError = errMsg
	s.health.LastAnalysisDuration = duration
	s.health.TotalAnalyses++
	if !success {
		s.health.FailedAnalyses++
	}
}

// updateMaintenanceHealth updates health status after maintenance.
func (s *Scheduler) updateMaintenanceHealth(success bool) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()

	s.health.LastMaintenanceTime = time.Now()
	s.health.LastMaintenanceSuccess = success
}
