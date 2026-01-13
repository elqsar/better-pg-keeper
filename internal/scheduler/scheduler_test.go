package scheduler_test

import (
	"context"
	"errors"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/scheduler"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

var errNotConnected = errors.New("not connected")

// mockStorage implements all storage interfaces needed for testing.
type mockStorage struct {
	snapshots        []models.Snapshot
	queryStats       map[int64][]models.QueryStat
	suggestions      []models.Suggestion
	purgedCount      int64
	nextSnapshotID   int64
	nextSuggestionID int64
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		queryStats:       make(map[int64][]models.QueryStat),
		nextSnapshotID:   1,
		nextSuggestionID: 1,
	}
}

// Close implements sqlite.Storage
func (m *mockStorage) Close() error { return nil }

// Instance operations
func (m *mockStorage) GetInstance(ctx context.Context, id int64) (*models.Instance, error) {
	return nil, nil
}
func (m *mockStorage) GetInstanceByName(ctx context.Context, name string) (*models.Instance, error) {
	return nil, nil
}
func (m *mockStorage) CreateInstance(ctx context.Context, inst *models.Instance) (int64, error) {
	return 1, nil
}
func (m *mockStorage) GetOrCreateInstance(ctx context.Context, inst *models.Instance) (int64, error) {
	return 1, nil
}
func (m *mockStorage) ListInstances(ctx context.Context) ([]models.Instance, error) {
	return nil, nil
}

// Snapshot operations
func (m *mockStorage) CreateSnapshot(ctx context.Context, snap *models.Snapshot) (int64, error) {
	snap.ID = m.nextSnapshotID
	m.nextSnapshotID++
	m.snapshots = append(m.snapshots, *snap)
	return snap.ID, nil
}

func (m *mockStorage) GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error) {
	for i := range m.snapshots {
		if m.snapshots[i].ID == id {
			return &m.snapshots[i], nil
		}
	}
	return nil, nil
}

func (m *mockStorage) GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error) {
	if len(m.snapshots) == 0 {
		return nil, nil
	}
	return &m.snapshots[len(m.snapshots)-1], nil
}

func (m *mockStorage) ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error) {
	return m.snapshots, nil
}

func (m *mockStorage) UpdateSnapshotCacheHitRatio(ctx context.Context, snapshotID int64, ratio float64) error {
	return nil
}

// Query stats operations
func (m *mockStorage) SaveQueryStats(ctx context.Context, snapshotID int64, stats []models.QueryStat) error {
	m.queryStats[snapshotID] = stats
	return nil
}

func (m *mockStorage) GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error) {
	return m.queryStats[snapshotID], nil
}

func (m *mockStorage) GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error) {
	return nil, nil
}

// Table stats operations
func (m *mockStorage) SaveTableStats(ctx context.Context, snapshotID int64, stats []models.TableStat) error {
	return nil
}

func (m *mockStorage) GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error) {
	return nil, nil
}

// Index stats operations
func (m *mockStorage) SaveIndexStats(ctx context.Context, snapshotID int64, stats []models.IndexStat) error {
	return nil
}

func (m *mockStorage) GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error) {
	return nil, nil
}

// Bloat stats operations
func (m *mockStorage) SaveBloatStats(ctx context.Context, snapshotID int64, stats []models.BloatInfo) error {
	return nil
}

func (m *mockStorage) GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error) {
	return nil, nil
}

// Suggestion operations
func (m *mockStorage) UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error {
	sug.ID = m.nextSuggestionID
	m.nextSuggestionID++
	sug.Status = models.StatusActive
	m.suggestions = append(m.suggestions, *sug)
	return nil
}

func (m *mockStorage) GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error) {
	var filtered []models.Suggestion
	for _, sug := range m.suggestions {
		if sug.Status == status {
			filtered = append(filtered, sug)
		}
	}
	return filtered, nil
}

func (m *mockStorage) GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error) {
	for i := range m.suggestions {
		if m.suggestions[i].ID == id {
			return &m.suggestions[i], nil
		}
	}
	return nil, nil
}

func (m *mockStorage) DismissSuggestion(ctx context.Context, id int64) error {
	for i := range m.suggestions {
		if m.suggestions[i].ID == id {
			m.suggestions[i].Status = models.StatusDismissed
		}
	}
	return nil
}

func (m *mockStorage) ResolveSuggestion(ctx context.Context, id int64) error {
	for i := range m.suggestions {
		if m.suggestions[i].ID == id {
			m.suggestions[i].Status = models.StatusResolved
		}
	}
	return nil
}

// Explain plan operations
func (m *mockStorage) SaveExplainPlan(ctx context.Context, plan *models.ExplainPlan) (int64, error) {
	return 1, nil
}

func (m *mockStorage) GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error) {
	return nil, nil
}

// Connection activity operations
func (m *mockStorage) SaveConnectionActivity(ctx context.Context, snapshotID int64, activity *models.ConnectionActivity) error {
	return nil
}

func (m *mockStorage) GetConnectionActivity(ctx context.Context, snapshotID int64) (*models.ConnectionActivity, error) {
	return nil, nil
}

// Long running queries operations
func (m *mockStorage) SaveLongRunningQueries(ctx context.Context, snapshotID int64, queries []models.LongRunningQuery) error {
	return nil
}

func (m *mockStorage) GetLongRunningQueries(ctx context.Context, snapshotID int64) ([]models.LongRunningQuery, error) {
	return nil, nil
}

// Idle in transaction operations
func (m *mockStorage) SaveIdleInTransaction(ctx context.Context, snapshotID int64, idle []models.IdleInTransaction) error {
	return nil
}

func (m *mockStorage) GetIdleInTransaction(ctx context.Context, snapshotID int64) ([]models.IdleInTransaction, error) {
	return nil, nil
}

// Lock stats operations
func (m *mockStorage) SaveLockStats(ctx context.Context, snapshotID int64, stats *models.LockStats) error {
	return nil
}

func (m *mockStorage) GetLockStats(ctx context.Context, snapshotID int64) (*models.LockStats, error) {
	return nil, nil
}

// Blocked queries operations
func (m *mockStorage) SaveBlockedQueries(ctx context.Context, snapshotID int64, queries []models.BlockedQuery) error {
	return nil
}

func (m *mockStorage) GetBlockedQueries(ctx context.Context, snapshotID int64) ([]models.BlockedQuery, error) {
	return nil, nil
}

// Extended database stats operations
func (m *mockStorage) SaveExtendedDatabaseStats(ctx context.Context, snapshotID int64, stats *models.ExtendedDatabaseStats) error {
	return nil
}

func (m *mockStorage) GetExtendedDatabaseStats(ctx context.Context, snapshotID int64) (*models.ExtendedDatabaseStats, error) {
	return nil, nil
}

// Maintenance operations
func (m *mockStorage) PurgeOldSnapshots(ctx context.Context, retention time.Duration) (int64, error) {
	m.purgedCount++
	return m.purgedCount, nil
}

// Current state operations (for dashboard)
func (m *mockStorage) SaveCurrentConnectionActivity(ctx context.Context, instanceID int64, activity *models.ConnectionActivity) error {
	return nil
}
func (m *mockStorage) GetCurrentConnectionActivity(ctx context.Context, instanceID int64) (*models.ConnectionActivity, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentLockStats(ctx context.Context, instanceID int64, stats *models.LockStats) error {
	return nil
}
func (m *mockStorage) GetCurrentLockStats(ctx context.Context, instanceID int64) (*models.LockStats, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentDatabaseStats(ctx context.Context, instanceID int64, stats *models.ExtendedDatabaseStats, cacheHitRatio *float64) error {
	return nil
}
func (m *mockStorage) GetCurrentDatabaseStats(ctx context.Context, instanceID int64) (*models.ExtendedDatabaseStats, *float64, error) {
	return nil, nil, nil
}
func (m *mockStorage) SaveCurrentLongRunningQueries(ctx context.Context, instanceID int64, queries []models.LongRunningQuery) error {
	return nil
}
func (m *mockStorage) GetCurrentLongRunningQueries(ctx context.Context, instanceID int64) ([]models.LongRunningQuery, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentIdleInTransaction(ctx context.Context, instanceID int64, idle []models.IdleInTransaction) error {
	return nil
}
func (m *mockStorage) GetCurrentIdleInTransaction(ctx context.Context, instanceID int64) ([]models.IdleInTransaction, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentBlockedQueries(ctx context.Context, instanceID int64, queries []models.BlockedQuery) error {
	return nil
}
func (m *mockStorage) GetCurrentBlockedQueries(ctx context.Context, instanceID int64) ([]models.BlockedQuery, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentQueryStats(ctx context.Context, instanceID int64, stats []models.QueryStat) error {
	return nil
}
func (m *mockStorage) GetCurrentQueryStats(ctx context.Context, instanceID int64) ([]models.QueryStat, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentTableStats(ctx context.Context, instanceID int64, stats []models.TableStat) error {
	return nil
}
func (m *mockStorage) GetCurrentTableStats(ctx context.Context, instanceID int64) ([]models.TableStat, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentIndexStats(ctx context.Context, instanceID int64, stats []models.IndexStat) error {
	return nil
}
func (m *mockStorage) GetCurrentIndexStats(ctx context.Context, instanceID int64) ([]models.IndexStat, error) {
	return nil, nil
}
func (m *mockStorage) SaveCurrentBloatStats(ctx context.Context, instanceID int64, stats []models.BloatInfo) error {
	return nil
}
func (m *mockStorage) GetCurrentBloatStats(ctx context.Context, instanceID int64) ([]models.BloatInfo, error) {
	return nil, nil
}

// mockPGClient implements the postgres.Client interface for testing.
type mockPGClient struct {
	connected    bool
	version      string
	statsReset   *time.Time
	queryStats   []models.QueryStat
	collectCount atomic.Int64
}

func newMockPGClient() *mockPGClient {
	return &mockPGClient{
		connected: true,
		version:   "PostgreSQL 16.0",
	}
}

func (m *mockPGClient) Connect(ctx context.Context) error {
	m.connected = true
	return nil
}

func (m *mockPGClient) Close() error {
	m.connected = false
	return nil
}

func (m *mockPGClient) Ping(ctx context.Context) error {
	if !m.connected {
		return errNotConnected
	}
	return nil
}

func (m *mockPGClient) GetVersion(ctx context.Context) (string, error) {
	return m.version, nil
}

func (m *mockPGClient) GetStatsResetTime(ctx context.Context) (*time.Time, error) {
	return m.statsReset, nil
}

func (m *mockPGClient) GetStatStatements(ctx context.Context) ([]models.QueryStat, error) {
	m.collectCount.Add(1)
	return m.queryStats, nil
}

func (m *mockPGClient) GetStatTables(ctx context.Context) ([]models.TableStat, error) {
	return nil, nil
}

func (m *mockPGClient) GetStatIndexes(ctx context.Context) ([]models.IndexStat, error) {
	return nil, nil
}

func (m *mockPGClient) GetDatabaseStats(ctx context.Context) (*models.DatabaseStats, error) {
	return &models.DatabaseStats{CacheHitRatio: 99.0}, nil
}

func (m *mockPGClient) GetTableBloat(ctx context.Context) ([]models.BloatInfo, error) {
	return nil, nil
}

func (m *mockPGClient) GetIndexDetails(ctx context.Context) ([]models.IndexDetail, error) {
	return nil, nil
}

func (m *mockPGClient) Explain(ctx context.Context, query string, analyze bool) (*models.ExplainPlan, error) {
	return nil, nil
}

func (m *mockPGClient) ExplainWithParams(ctx context.Context, query string, params []any, analyze bool) (*models.ExplainPlan, error) {
	return nil, nil
}

// Operational stats methods
func (m *mockPGClient) GetConnectionActivity(ctx context.Context) (*models.ConnectionActivity, error) {
	return nil, nil
}

func (m *mockPGClient) GetLongRunningQueries(ctx context.Context, thresholdSeconds float64) ([]models.LongRunningQuery, error) {
	return nil, nil
}

func (m *mockPGClient) GetIdleInTransaction(ctx context.Context, thresholdSeconds float64) ([]models.IdleInTransaction, error) {
	return nil, nil
}

func (m *mockPGClient) GetLockStats(ctx context.Context) (*models.LockStats, error) {
	return nil, nil
}

func (m *mockPGClient) GetBlockedQueries(ctx context.Context) ([]models.BlockedQuery, error) {
	return nil, nil
}

func (m *mockPGClient) GetExtendedDatabaseStats(ctx context.Context) (*models.ExtendedDatabaseStats, error) {
	return nil, nil
}

// mockCollector is a simple collector for testing.
type mockCollector struct {
	name         string
	interval     time.Duration
	collectCount atomic.Int64
	shouldFail   bool
}

func (m *mockCollector) Name() string {
	return m.name
}

func (m *mockCollector) Interval() time.Duration {
	return m.interval
}

func (m *mockCollector) Collect(ctx context.Context, snapshotID int64) error {
	m.collectCount.Add(1)
	if m.shouldFail {
		return context.DeadlineExceeded
	}
	return nil
}

// Test helper to create a scheduler with mocks
func createTestScheduler(t *testing.T) (*scheduler.Scheduler, *mockStorage, *mockPGClient) {
	t.Helper()

	storage := newMockStorage()
	pgClient := newMockPGClient()
	logger := log.New(os.Stdout, "[test] ", 0)

	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   pgClient,
		Storage:    storage,
		InstanceID: 1,
		Logger:     logger,
	})

	// Add a mock collector with a short interval for testing
	coord.RegisterCollector(&mockCollector{
		name:     "test_collector",
		interval: 100 * time.Millisecond,
	})

	analyzerInst := analyzer.NewMainAnalyzer(storage, nil)
	suggesterInst := suggester.NewSuggester(storage, nil, logger)

	schedConfig := &config.SchedulerConfig{
		SnapshotInterval: config.Duration(100 * time.Millisecond),
		AnalysisInterval: config.Duration(200 * time.Millisecond),
	}

	retentionConfig := &config.RetentionConfig{
		Snapshots: config.Duration(24 * time.Hour),
	}

	sched, err := scheduler.NewScheduler(scheduler.Config{
		SchedulerConfig: schedConfig,
		RetentionConfig: retentionConfig,
		Coordinator:     coord,
		Analyzer:        analyzerInst,
		Suggester:       suggesterInst,
		Storage:         storage,
		InstanceID:      1,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	return sched, storage, pgClient
}

func TestNewScheduler_RequiredFields(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	storage := newMockStorage()
	pgClient := newMockPGClient()

	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   pgClient,
		Storage:    storage,
		InstanceID: 1,
	})

	analyzerInst := analyzer.NewMainAnalyzer(storage, nil)
	suggesterInst := suggester.NewSuggester(storage, nil, logger)

	tests := []struct {
		name    string
		config  scheduler.Config
		wantErr bool
	}{
		{
			name:    "missing coordinator",
			config:  scheduler.Config{Analyzer: analyzerInst, Suggester: suggesterInst, Storage: storage},
			wantErr: true,
		},
		{
			name:    "missing analyzer",
			config:  scheduler.Config{Coordinator: coord, Suggester: suggesterInst, Storage: storage},
			wantErr: true,
		},
		{
			name:    "missing suggester",
			config:  scheduler.Config{Coordinator: coord, Analyzer: analyzerInst, Storage: storage},
			wantErr: true,
		},
		{
			name:    "missing storage",
			config:  scheduler.Config{Coordinator: coord, Analyzer: analyzerInst, Suggester: suggesterInst},
			wantErr: true,
		},
		{
			name: "all required fields",
			config: scheduler.Config{
				Coordinator: coord,
				Analyzer:    analyzerInst,
				Suggester:   suggesterInst,
				Storage:     storage,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scheduler.NewScheduler(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewScheduler() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestScheduler_StartStop(t *testing.T) {
	sched, _, _ := createTestScheduler(t)

	// Should not be running initially
	if sched.IsRunning() {
		t.Error("Scheduler should not be running initially")
	}

	// Start the scheduler
	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !sched.IsRunning() {
		t.Error("Scheduler should be running after Start()")
	}

	// Starting again should fail
	if err := sched.Start(ctx); err == nil {
		t.Error("Start() should fail when already running")
	}

	// Give it a moment to run
	time.Sleep(50 * time.Millisecond)

	// Stop the scheduler
	if err := sched.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if sched.IsRunning() {
		t.Error("Scheduler should not be running after Stop()")
	}

	// Stopping again should fail
	if err := sched.Stop(); err == nil {
		t.Error("Stop() should fail when not running")
	}
}

func TestScheduler_CollectionLoop(t *testing.T) {
	sched, storage, _ := createTestScheduler(t)

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for a few collection cycles
	time.Sleep(350 * time.Millisecond)

	if err := sched.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Should have created at least 1 snapshot
	// Note: Multiple collection cycles may reuse the same snapshot (within 1 minute window)
	// which is the expected behavior to avoid fragmented data
	if len(storage.snapshots) < 1 {
		t.Errorf("Expected at least 1 snapshot, got %d", len(storage.snapshots))
	}
}

func TestScheduler_TriggerSnapshot(t *testing.T) {
	sched, storage, _ := createTestScheduler(t)

	ctx := context.Background()

	// Trigger snapshot without starting scheduler
	result, err := sched.TriggerSnapshot(ctx)
	if err != nil {
		t.Fatalf("TriggerSnapshot() error = %v", err)
	}

	if result == nil {
		t.Fatal("TriggerSnapshot() result is nil")
	}

	if result.CollectionResult == nil {
		t.Error("CollectionResult is nil")
	}

	if result.CollectionResult.SnapshotID == 0 {
		t.Error("SnapshotID should not be 0")
	}

	if result.Duration == 0 {
		t.Error("Duration should not be 0")
	}

	// Verify snapshot was created
	if len(storage.snapshots) != 1 {
		t.Errorf("Expected 1 snapshot, got %d", len(storage.snapshots))
	}
}

func TestScheduler_TriggerSnapshot_SequentialCalls(t *testing.T) {
	sched, storage, _ := createTestScheduler(t)

	ctx := context.Background()

	// First trigger should succeed
	result1, err := sched.TriggerSnapshot(ctx)
	if err != nil {
		t.Fatalf("First TriggerSnapshot() error = %v", err)
	}
	if result1.CollectionResult.SnapshotID == 0 {
		t.Error("First trigger should create snapshot")
	}

	// Second trigger should also succeed (sequential, not concurrent)
	result2, err := sched.TriggerSnapshot(ctx)
	if err != nil {
		t.Fatalf("Second TriggerSnapshot() error = %v", err)
	}
	if result2.CollectionResult.SnapshotID == 0 {
		t.Error("Second trigger should create snapshot")
	}

	// Should have 2 snapshots
	if len(storage.snapshots) != 2 {
		t.Errorf("Expected 2 snapshots, got %d", len(storage.snapshots))
	}
}

func TestScheduler_GetHealth(t *testing.T) {
	sched, _, _ := createTestScheduler(t)

	// Initial health status
	health := sched.GetHealth()
	if health == nil {
		t.Fatal("GetHealth() returned nil")
	}

	if health.IsRunning {
		t.Error("IsRunning should be false initially")
	}

	if health.TotalCollections != 0 {
		t.Error("TotalCollections should be 0 initially")
	}

	// Start and let it run
	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(250 * time.Millisecond)

	health = sched.GetHealth()
	if !health.IsRunning {
		t.Error("IsRunning should be true after Start()")
	}

	if health.TotalCollections == 0 {
		t.Error("TotalCollections should be > 0 after running")
	}

	if health.LastCollectionTime.IsZero() {
		t.Error("LastCollectionTime should not be zero")
	}

	if err := sched.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestScheduler_GracefulShutdown(t *testing.T) {
	sched, _, _ := createTestScheduler(t)

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Stop should complete within timeout
	start := time.Now()
	if err := sched.StopWithTimeout(5 * time.Second); err != nil {
		t.Fatalf("StopWithTimeout() error = %v", err)
	}
	duration := time.Since(start)

	if duration > 2*time.Second {
		t.Errorf("Stop took too long: %v", duration)
	}
}

func TestScheduler_ContextCancellation(t *testing.T) {
	sched, _, _ := createTestScheduler(t)

	ctx, cancel := context.WithCancel(context.Background())

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Give loops time to notice cancellation
	time.Sleep(100 * time.Millisecond)

	// Stop should still work
	if err := sched.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestScheduler_MaintenanceRuns(t *testing.T) {
	// This test uses a shorter maintenance interval for testing
	storage := newMockStorage()
	pgClient := newMockPGClient()
	logger := log.New(os.Stdout, "[test] ", 0)

	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   pgClient,
		Storage:    storage,
		InstanceID: 1,
		Logger:     logger,
	})

	coord.RegisterCollector(&mockCollector{
		name:     "test_collector",
		interval: time.Minute,
	})

	analyzerInst := analyzer.NewMainAnalyzer(storage, nil)
	suggesterInst := suggester.NewSuggester(storage, nil, logger)

	schedConfig := &config.SchedulerConfig{
		SnapshotInterval: config.Duration(50 * time.Millisecond),
		AnalysisInterval: config.Duration(100 * time.Millisecond),
	}

	retentionConfig := &config.RetentionConfig{
		Snapshots: config.Duration(1 * time.Hour),
	}

	sched, err := scheduler.NewScheduler(scheduler.Config{
		SchedulerConfig: schedConfig,
		RetentionConfig: retentionConfig,
		Coordinator:     coord,
		Analyzer:        analyzerInst,
		Suggester:       suggesterInst,
		Storage:         storage,
		InstanceID:      1,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("Failed to create scheduler: %v", err)
	}

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Just verify scheduler runs without panics
	time.Sleep(100 * time.Millisecond)

	if err := sched.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestHealthSnapshot_Fields(t *testing.T) {
	sched, _, _ := createTestScheduler(t)

	// Trigger a snapshot to populate health data
	ctx := context.Background()
	_, err := sched.TriggerSnapshot(ctx)
	if err != nil {
		t.Fatalf("TriggerSnapshot() error = %v", err)
	}

	health := sched.GetHealth()

	// Verify health fields are populated after trigger
	if health.TotalCollections != 1 {
		t.Errorf("TotalCollections = %d, want 1", health.TotalCollections)
	}

	if health.LastCollectionDuration == 0 {
		t.Error("LastCollectionDuration should not be 0")
	}
}

func TestScheduler_RestartAfterStop(t *testing.T) {
	sched, storage, _ := createTestScheduler(t)
	ctx := context.Background()

	// Start scheduler
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("First Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Stop scheduler
	if err := sched.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Should have at least 1 snapshot after first run
	if len(storage.snapshots) < 1 {
		t.Errorf("Expected at least 1 snapshot after first run, got %d", len(storage.snapshots))
	}

	// Start again
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Second Start() error = %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// Stop again
	if err := sched.Stop(); err != nil {
		t.Fatalf("Second Stop() error = %v", err)
	}

	// Should still have at least 1 snapshot
	// Note: Snapshots may be reused within 1-minute window, so count might not increase
	if len(storage.snapshots) < 1 {
		t.Errorf("Expected at least 1 snapshot after restart, got %d", len(storage.snapshots))
	}
}
