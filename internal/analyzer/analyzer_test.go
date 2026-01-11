package analyzer

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
)

// mockStorage implements the Storage interface for testing.
type mockStorage struct {
	snapshots   map[int64]*models.Snapshot
	queryStats  map[int64][]models.QueryStat
	tableStats  map[int64][]models.TableStat
	indexStats  map[int64][]models.IndexStat
	bloatStats  map[int64][]models.BloatInfo
	queryDeltas []models.QueryStatDelta
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		snapshots:  make(map[int64]*models.Snapshot),
		queryStats: make(map[int64][]models.QueryStat),
		tableStats: make(map[int64][]models.TableStat),
		indexStats: make(map[int64][]models.IndexStat),
		bloatStats: make(map[int64][]models.BloatInfo),
	}
}

func (m *mockStorage) GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error) {
	snap, ok := m.snapshots[id]
	if !ok {
		return nil, nil
	}
	return snap, nil
}

func (m *mockStorage) GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error) {
	var latest *models.Snapshot
	for _, snap := range m.snapshots {
		if snap.InstanceID == instanceID {
			if latest == nil || snap.CapturedAt.After(latest.CapturedAt) {
				latest = snap
			}
		}
	}
	return latest, nil
}

func (m *mockStorage) ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error) {
	var result []models.Snapshot
	for _, snap := range m.snapshots {
		if snap.InstanceID == instanceID {
			result = append(result, *snap)
		}
	}
	// Sort by captured_at descending (simplified)
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStorage) GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error) {
	return m.queryStats[snapshotID], nil
}

func (m *mockStorage) GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error) {
	return m.queryDeltas, nil
}

func (m *mockStorage) GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error) {
	return m.tableStats[snapshotID], nil
}

func (m *mockStorage) GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error) {
	return m.indexStats[snapshotID], nil
}

func (m *mockStorage) GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error) {
	return m.bloatStats[snapshotID], nil
}

// Operational stats methods
func (m *mockStorage) GetConnectionActivity(ctx context.Context, snapshotID int64) (*models.ConnectionActivity, error) {
	return nil, nil
}

func (m *mockStorage) GetLongRunningQueries(ctx context.Context, snapshotID int64) ([]models.LongRunningQuery, error) {
	return nil, nil
}

func (m *mockStorage) GetIdleInTransaction(ctx context.Context, snapshotID int64) ([]models.IdleInTransaction, error) {
	return nil, nil
}

func (m *mockStorage) GetLockStats(ctx context.Context, snapshotID int64) (*models.LockStats, error) {
	return nil, nil
}

func (m *mockStorage) GetBlockedQueries(ctx context.Context, snapshotID int64) ([]models.BlockedQuery, error) {
	return nil, nil
}

func (m *mockStorage) GetExtendedDatabaseStats(ctx context.Context, snapshotID int64) (*models.ExtendedDatabaseStats, error) {
	return nil, nil
}

func TestSlowQueryAnalyzer_Analyze(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	cacheRatio := 98.5
	storage.snapshots[1] = &models.Snapshot{
		ID:            1,
		InstanceID:    1,
		CapturedAt:    time.Now(),
		CacheHitRatio: &cacheRatio,
	}

	storage.queryStats[1] = []models.QueryStat{
		{
			QueryID:        100,
			Query:          "SELECT * FROM users WHERE id = $1",
			MeanExecTime:   50.0, // Below threshold
			Calls:          1000,
			TotalExecTime:  50000,
			SharedBlksHit:  9000,
			SharedBlksRead: 1000,
		},
		{
			QueryID:        101,
			Query:          "SELECT * FROM orders WHERE user_id = $1",
			MeanExecTime:   1500.0, // Above threshold
			Calls:          500,
			TotalExecTime:  750000,
			SharedBlksHit:  4000,
			SharedBlksRead: 1000,
		},
		{
			QueryID:        102,
			Query:          "SELECT * FROM products",
			MeanExecTime:   2000.0, // Above threshold
			Calls:          100,
			TotalExecTime:  200000,
			SharedBlksHit:  100,
			SharedBlksRead: 900,
		},
	}

	config := DefaultConfig()
	config.SlowQueryMs = 1000

	analyzer := NewSlowQueryAnalyzer(storage, config)
	slowQueries, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should find 2 slow queries (101 and 102)
	if len(slowQueries) != 2 {
		t.Errorf("Expected 2 slow queries, got %d", len(slowQueries))
	}

	// First should be the one with highest total execution time (101)
	if len(slowQueries) > 0 && slowQueries[0].QueryID != 101 {
		t.Errorf("Expected query 101 first (highest total time), got %d", slowQueries[0].QueryID)
	}

	// Check cache hit ratio calculation
	if len(slowQueries) > 0 {
		// Query 101: 4000 hit / 5000 total = 0.8
		expectedRatio := 4000.0 / 5000.0
		if slowQueries[0].CacheHitRatio != expectedRatio {
			t.Errorf("Expected cache hit ratio %f, got %f", expectedRatio, slowQueries[0].CacheHitRatio)
		}
	}
}

func TestSlowQueryAnalyzer_NoSlowQueries(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	cacheRatio := 98.5
	storage.snapshots[1] = &models.Snapshot{
		ID:            1,
		InstanceID:    1,
		CapturedAt:    time.Now(),
		CacheHitRatio: &cacheRatio,
	}

	storage.queryStats[1] = []models.QueryStat{
		{
			QueryID:       100,
			Query:         "SELECT 1",
			MeanExecTime:  0.5,
			Calls:         1000,
			TotalExecTime: 500,
		},
	}

	analyzer := NewSlowQueryAnalyzer(storage, nil)
	slowQueries, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(slowQueries) != 0 {
		t.Errorf("Expected 0 slow queries, got %d", len(slowQueries))
	}
}

func TestCacheAnalyzer_Analyze(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	cacheRatio := 92.0 // Below default threshold of 95%
	storage.snapshots[1] = &models.Snapshot{
		ID:            1,
		InstanceID:    1,
		CapturedAt:    time.Now(),
		CacheHitRatio: &cacheRatio,
	}

	storage.queryStats[1] = []models.QueryStat{
		{
			QueryID:        100,
			Query:          "SELECT * FROM users",
			SharedBlksHit:  9500, // 95% - at threshold
			SharedBlksRead: 500,
			Calls:          100,
		},
		{
			QueryID:        101,
			Query:          "SELECT * FROM large_table",
			SharedBlksHit:  5000, // 50% - poor
			SharedBlksRead: 5000,
			Calls:          50,
		},
		{
			QueryID:        102,
			Query:          "SELECT 1",
			SharedBlksHit:  10, // Too few blocks to flag
			SharedBlksRead: 10,
			Calls:          1000,
		},
	}

	analyzer := NewCacheAnalyzer(storage, nil)
	result, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.OverallHitRatio != 92.0 {
		t.Errorf("Expected overall hit ratio 92.0, got %f", result.OverallHitRatio)
	}

	if !result.BelowThreshold {
		t.Error("Expected BelowThreshold to be true")
	}

	// Should find 1 poor cache query (101 with 50%)
	// Query 102 has too few blocks (20 < 100)
	if len(result.PoorCacheQueries) != 1 {
		t.Errorf("Expected 1 poor cache query, got %d", len(result.PoorCacheQueries))
	}

	if len(result.PoorCacheQueries) > 0 && result.PoorCacheQueries[0].QueryID != 101 {
		t.Errorf("Expected query 101 as poor cache query, got %d", result.PoorCacheQueries[0].QueryID)
	}
}

func TestTableAnalyzer_HighBloat(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: time.Now(),
	}

	storage.tableStats[1] = []models.TableStat{
		{
			SchemaName: "public",
			RelName:    "users",
			NLiveTup:   10000,
			NDeadTup:   5000,
			TableSize:  1024 * 1024,
		},
	}

	storage.bloatStats[1] = []models.BloatInfo{
		{
			SchemaName:   "public",
			RelName:      "users",
			NLiveTup:     10000,
			NDeadTup:     5000,
			BloatPercent: 50.0, // 50% bloat
		},
	}

	config := DefaultConfig()
	config.BloatPercentWarning = 20

	analyzer := NewTableAnalyzer(storage, config)
	issues, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should find high bloat issue
	var foundBloat bool
	for _, issue := range issues {
		if issue.IssueType == TableIssueHighBloat {
			foundBloat = true
			if issue.Severity != models.SeverityCritical {
				t.Errorf("Expected critical severity for 50%% bloat, got %s", issue.Severity)
			}
		}
	}

	if !foundBloat {
		t.Error("Expected to find high bloat issue")
	}
}

func TestTableAnalyzer_StaleVacuum(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: time.Now(),
	}

	oldVacuum := time.Now().AddDate(0, 0, -14) // 14 days ago
	storage.tableStats[1] = []models.TableStat{
		{
			SchemaName: "public",
			RelName:    "orders",
			NLiveTup:   50000,
			NDeadTup:   5000, // Significant dead tuples
			LastVacuum: &oldVacuum,
			TableSize:  10 * 1024 * 1024,
		},
	}

	config := DefaultConfig()
	config.VacuumStaleDays = 7

	analyzer := NewTableAnalyzer(storage, config)
	issues, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var foundStaleVacuum bool
	for _, issue := range issues {
		if issue.IssueType == TableIssueStaleVacuum {
			foundStaleVacuum = true
		}
	}

	if !foundStaleVacuum {
		t.Error("Expected to find stale vacuum issue")
	}
}

func TestTableAnalyzer_MissingIndex(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: time.Now(),
	}

	storage.tableStats[1] = []models.TableStat{
		{
			SchemaName: "public",
			RelName:    "large_table",
			NLiveTup:   100000,
			SeqScan:    10000, // High seq scans
			IdxScan:    100,   // Low index scans
			TableSize:  50 * 1024 * 1024,
		},
	}

	config := DefaultConfig()
	config.SeqScanRatioWarning = 0.5
	config.MinTableSizeForIndex = 10000

	analyzer := NewTableAnalyzer(storage, config)
	issues, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var foundMissingIndex bool
	for _, issue := range issues {
		if issue.IssueType == TableIssueMissingIndex {
			foundMissingIndex = true
		}
	}

	if !foundMissingIndex {
		t.Error("Expected to find missing index issue")
	}
}

func TestIndexAnalyzer_UnusedIndex(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: time.Now(),
	}

	storage.indexStats[1] = []models.IndexStat{
		{
			SchemaName:   "public",
			RelName:      "users",
			IndexRelName: "idx_users_legacy",
			IdxScan:      0, // Never used
			IndexSize:    1024 * 1024,
			IsUnique:     false,
			IsPrimary:    false,
		},
		{
			SchemaName:   "public",
			RelName:      "users",
			IndexRelName: "users_pkey",
			IdxScan:      0, // Primary key - should not be flagged
			IndexSize:    512 * 1024,
			IsUnique:     true,
			IsPrimary:    true,
		},
		{
			SchemaName:   "public",
			RelName:      "users",
			IndexRelName: "idx_users_email_unique",
			IdxScan:      0, // Unique - should not be flagged
			IndexSize:    256 * 1024,
			IsUnique:     true,
			IsPrimary:    false,
		},
	}

	analyzer := NewIndexAnalyzer(storage, nil)
	issues, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should only find 1 unused index (the non-unique, non-primary one)
	unusedCount := 0
	for _, issue := range issues {
		if issue.IssueType == IndexIssueUnused {
			unusedCount++
			if issue.IndexName != "idx_users_legacy" {
				t.Errorf("Expected idx_users_legacy, got %s", issue.IndexName)
			}
		}
	}

	if unusedCount != 1 {
		t.Errorf("Expected 1 unused index issue, got %d", unusedCount)
	}
}

func TestIndexAnalyzer_DuplicateIndex(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: time.Now(),
	}

	storage.indexStats[1] = []models.IndexStat{
		{
			SchemaName:   "public",
			RelName:      "orders",
			IndexRelName: "idx_orders_user_id",
			IdxScan:      1000,
			IndexSize:    1024 * 1024,
			IsUnique:     false,
			IsPrimary:    false,
		},
		{
			SchemaName:   "public",
			RelName:      "orders",
			IndexRelName: "idx_orders_user_id_old", // Similar name pattern
			IdxScan:      10,                       // Much less used
			IndexSize:    1024 * 1024,              // Same size
			IsUnique:     false,
			IsPrimary:    false,
		},
	}

	analyzer := NewIndexAnalyzer(storage, nil)
	issues, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should find potential duplicate
	var foundDuplicate bool
	for _, issue := range issues {
		if issue.IssueType == IndexIssueDuplicate {
			foundDuplicate = true
			if issue.IndexName != "idx_orders_user_id_old" {
				t.Errorf("Expected idx_orders_user_id_old as duplicate, got %s", issue.IndexName)
			}
		}
	}

	if !foundDuplicate {
		t.Error("Expected to find duplicate index issue")
	}
}

func TestMainAnalyzer_Analyze(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	cacheRatio := 98.0
	storage.snapshots[1] = &models.Snapshot{
		ID:            1,
		InstanceID:    1,
		CapturedAt:    time.Now(),
		CacheHitRatio: &cacheRatio,
	}

	storage.queryStats[1] = []models.QueryStat{
		{
			QueryID:       100,
			Query:         "SELECT * FROM slow_table",
			MeanExecTime:  2000,
			Calls:         100,
			TotalExecTime: 200000,
		},
	}

	storage.tableStats[1] = []models.TableStat{
		{
			SchemaName: "public",
			RelName:    "test_table",
			NLiveTup:   1000,
		},
	}

	storage.indexStats[1] = []models.IndexStat{
		{
			SchemaName:   "public",
			RelName:      "test_table",
			IndexRelName: "test_idx",
			IdxScan:      100,
			IndexSize:    8192,
		},
	}

	analyzer := NewMainAnalyzer(storage, nil)
	result, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.SnapshotID != 1 {
		t.Errorf("Expected snapshot ID 1, got %d", result.SnapshotID)
	}

	if result.InstanceID != 1 {
		t.Errorf("Expected instance ID 1, got %d", result.InstanceID)
	}

	// Should have found slow query
	if len(result.SlowQueries) != 1 {
		t.Errorf("Expected 1 slow query, got %d", len(result.SlowQueries))
	}

	// Should have cache stats
	if result.CacheStats == nil {
		t.Error("Expected cache stats")
	}

	// No errors expected
	if result.ErrorCount != 0 {
		t.Errorf("Expected 0 errors, got %d: %v", result.ErrorCount, result.Errors)
	}
}

func TestMainAnalyzer_PartialFailure(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	cacheRatio := 98.0
	storage.snapshots[1] = &models.Snapshot{
		ID:            1,
		InstanceID:    1,
		CapturedAt:    time.Now(),
		CacheHitRatio: &cacheRatio,
	}

	// Only set up query stats, no table or index stats
	storage.queryStats[1] = []models.QueryStat{
		{
			QueryID:       100,
			Query:         "SELECT 1",
			MeanExecTime:  1,
			Calls:         1000,
			TotalExecTime: 1000,
		},
	}

	analyzer := NewMainAnalyzer(storage, nil)
	result, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Should still return a result even with missing data
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Other analyses should complete even if some return empty results
	if result.CacheStats == nil {
		t.Error("Expected cache stats even with no issues")
	}
}

func TestAnalysisResult_GetIssueCount(t *testing.T) {
	result := &AnalysisResult{
		SlowQueries: []SlowQuery{{}, {}},
		TableIssues: []TableIssue{{}, {}, {}},
		IndexIssues: []IndexIssue{{}},
		CacheStats: &CacheAnalysis{
			BelowThreshold:   true,
			PoorCacheQueries: []PoorCacheQuery{{}, {}},
		},
	}

	// 2 slow + 3 table + 1 index + 2 poor cache + 1 below threshold = 9
	expected := 9
	if result.GetIssueCount() != expected {
		t.Errorf("Expected %d issues, got %d", expected, result.GetIssueCount())
	}
}

func TestAnalysisResult_GetCriticalCount(t *testing.T) {
	result := &AnalysisResult{
		TableIssues: []TableIssue{
			{Severity: "critical"},
			{Severity: "warning"},
			{Severity: "critical"},
		},
		IndexIssues: []IndexIssue{
			{Severity: "critical"},
			{Severity: "info"},
		},
	}

	if result.GetCriticalCount() != 3 {
		t.Errorf("Expected 3 critical issues, got %d", result.GetCriticalCount())
	}
}

func TestAnalysisResult_GetWarningCount(t *testing.T) {
	result := &AnalysisResult{
		SlowQueries: []SlowQuery{{}, {}}, // All slow queries are warnings
		TableIssues: []TableIssue{
			{Severity: "warning"},
			{Severity: "critical"},
		},
		IndexIssues: []IndexIssue{
			{Severity: "warning"},
		},
		CacheStats: &CacheAnalysis{
			BelowThreshold: true, // This is a warning
		},
	}

	// 2 slow + 1 table warning + 1 index warning + 1 cache warning = 5
	if result.GetWarningCount() != 5 {
		t.Errorf("Expected 5 warning issues, got %d", result.GetWarningCount())
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{500, "500 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %s, expected %s", tt.bytes, result, tt.expected)
		}
	}
}

func TestConfigFromThresholds(t *testing.T) {
	thresholds := DefaultConfig()
	cfg := ConfigFromThresholds(config.ThresholdsConfig{
		SlowQueryMs:          500,
		CacheHitRatioWarning: 0.9,
		BloatPercentWarning:  15,
		UnusedIndexDays:      14,
		SeqScanRatioWarning:  0.6,
		MinTableSizeForIndex: 5000,
	})

	if cfg.SlowQueryMs != 500 {
		t.Errorf("Expected SlowQueryMs 500, got %f", cfg.SlowQueryMs)
	}

	if cfg.CacheHitRatioWarning != 0.9 {
		t.Errorf("Expected CacheHitRatioWarning 0.9, got %f", cfg.CacheHitRatioWarning)
	}

	if cfg.BloatPercentWarning != 15 {
		t.Errorf("Expected BloatPercentWarning 15, got %f", cfg.BloatPercentWarning)
	}

	// Check defaults that aren't in threshold config
	if thresholds.VacuumStaleDays != 7 {
		t.Errorf("Expected VacuumStaleDays 7, got %d", thresholds.VacuumStaleDays)
	}
}

func TestArePotentialDuplicates(t *testing.T) {
	analyzer := &IndexAnalyzer{}

	tests := []struct {
		name1    string
		name2    string
		expected bool
	}{
		{"idx_users_email", "idx_users_email_1", true},   // Common suffix pattern
		{"idx_users_email", "idx_users_email_old", true}, // Backup pattern
		{"idx_orders_date", "idx_orders_status", false},  // Different columns
		{"idx_users_name", "idx_users_name_idx", true},   // Suffix variation
		{"short", "shorty", false},                       // Too short for contains check
		{"idx_products_category", "idx_products", true},  // One contains other
	}

	for _, tt := range tests {
		result := analyzer.arePotentialDuplicates(tt.name1, tt.name2)
		if result != tt.expected {
			t.Errorf("arePotentialDuplicates(%q, %q) = %v, expected %v",
				tt.name1, tt.name2, result, tt.expected)
		}
	}
}
