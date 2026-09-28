package analyzer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
)

// mockStorage implements the Storage interface for testing.
type mockStorage struct {
	snapshots     map[int64]*models.Snapshot
	queryStats    map[int64][]models.QueryStat
	tableStats    map[int64][]models.TableStat
	indexStats    map[int64][]models.IndexStat
	bloatStats    map[int64][]models.BloatInfo
	extendedStats map[int64]*models.ExtendedDatabaseStats
	queryDeltas   []models.QueryStatDelta
	onDelta       func(from, to int64)
	queryStatsErr error
	outageRisk    map[int64]*models.OutageRisk
	queryPlans    map[int64]*models.QueryPlanReport
	sizeHistory   []models.SizeSample
	connPeak      *models.ConnectionPeak
	// coverage optionally overrides which snapshots a collector contributed to,
	// keyed by domain. Nil means "derive it from the data maps above".
	coverage map[string][]int64
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		snapshots:     make(map[int64]*models.Snapshot),
		queryStats:    make(map[int64][]models.QueryStat),
		tableStats:    make(map[int64][]models.TableStat),
		indexStats:    make(map[int64][]models.IndexStat),
		bloatStats:    make(map[int64][]models.BloatInfo),
		extendedStats: make(map[int64]*models.ExtendedDatabaseStats),
		outageRisk:    make(map[int64]*models.OutageRisk),
		queryPlans:    make(map[int64]*models.QueryPlanReport),
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

// GetLatestSnapshotWithCollector resolves the newest snapshot carrying a domain.
func (m *mockStorage) GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error) {
	var latest *models.Snapshot
	for id, snap := range m.snapshots {
		if snap.InstanceID != instanceID {
			continue
		}
		if !notAfter.IsZero() && snap.CapturedAt.After(notAfter) {
			continue
		}
		if !m.covers(id, collector) {
			continue
		}
		if latest == nil || snap.CapturedAt.After(latest.CapturedAt) {
			latest = snap
		}
	}
	return latest, nil
}

func (m *mockStorage) GetSnapshotCollectors(ctx context.Context, snapshotID int64) ([]models.SnapshotCollector, error) {
	var runs []models.SnapshotCollector
	for _, domain := range AllDomains {
		if m.covers(snapshotID, domain) {
			runs = append(runs, models.SnapshotCollector{
				SnapshotID: snapshotID, Collector: domain,
				Status:      models.CollectorStatusSuccess,
				CollectedAt: m.snapshots[snapshotID].CapturedAt,
			})
		}
	}
	return runs, nil
}

// covers reports whether a snapshot carries data for a domain.
func (m *mockStorage) covers(snapshotID int64, collector string) bool {
	if m.coverage != nil {
		for _, id := range m.coverage[collector] {
			if id == snapshotID {
				return true
			}
		}
		return false
	}

	switch collector {
	case DomainQueryStats:
		return len(m.queryStats[snapshotID]) > 0
	case DomainTableStats:
		return len(m.tableStats[snapshotID]) > 0
	case DomainIndexStats:
		return len(m.indexStats[snapshotID]) > 0
	case DomainBloat:
		return len(m.bloatStats[snapshotID]) > 0
	case DomainOutageRisk:
		return m.outageRisk[snapshotID] != nil
	case DomainQueryPlans:
		return m.queryPlans[snapshotID] != nil
	case DomainActivity, DomainLocks, DomainDatabaseStats:
		// The mock returns nil for these and the analyzers treat nil as
		// "not collected", so reporting them as covered costs nothing.
		return true
	}
	return false
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
	if m.queryStatsErr != nil {
		return nil, m.queryStatsErr
	}
	return m.queryStats[snapshotID], nil
}

func (m *mockStorage) GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error) {
	if m.onDelta != nil {
		m.onDelta(fromSnapshotID, toSnapshotID)
	}
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

func (m *mockStorage) GetQueryPlans(ctx context.Context, snapshotID int64) (*models.QueryPlanReport, error) {
	return m.queryPlans[snapshotID], nil
}

func (m *mockStorage) GetOutageRisk(ctx context.Context, snapshotID int64) (*models.OutageRisk, error) {
	return m.outageRisk[snapshotID], nil
}

func (m *mockStorage) GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error) {
	var out []models.SizeSample
	for _, s := range m.sizeHistory {
		if !s.CapturedAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockStorage) GetConnectionPeak(ctx context.Context, instanceID int64, since, until time.Time) (*models.ConnectionPeak, error) {
	return m.connPeak, nil
}

func (m *mockStorage) GetExtendedDatabaseStats(ctx context.Context, snapshotID int64) (*models.ExtendedDatabaseStats, error) {
	return m.extendedStats[snapshotID], nil
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

// TestSlowQueryAnalyzer_UsesRecentWindow checks that queries are judged by what
// they did within the window, not by their mean since statistics were reset.
func TestSlowQueryAnalyzer_UsesRecentWindow(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()
	now := time.Now()

	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-25 * time.Hour)}
	storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	storage.snapshots[3] = &models.Snapshot{ID: 3, InstanceID: 1, CapturedAt: now}
	for _, id := range []int64{1, 2, 3} {
		storage.queryStats[id] = []models.QueryStat{
			{QueryID: 101, Query: "regressed", MeanExecTime: 300, MaxExecTime: 9000},
			{QueryID: 102, Query: "fixed", MeanExecTime: 2000},
			{QueryID: 103, Query: "idle", MeanExecTime: 2000},
		}
	}
	storage.queryDeltas = []models.QueryStatDelta{
		{QueryID: 101, Query: "regressed", DeltaCalls: 10, DeltaTotalTime: 15000, MeanExecTime: 1500},
		{QueryID: 102, Query: "fixed", DeltaCalls: 1000, DeltaTotalTime: 50000, MeanExecTime: 50},
		{QueryID: 103, Query: "idle", DeltaCalls: 0},
	}

	var from, to int64
	storage.onDelta = func(f, t int64) { from, to = f, t }

	slowQueries, err := NewSlowQueryAnalyzer(storage, DefaultConfig()).Analyze(ctx, 3)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// The baseline is the newest snapshot at least a window old, not the previous one.
	if from != 1 || to != 3 {
		t.Errorf("delta taken over snapshots %d..%d, want 1..3", from, to)
	}
	if len(slowQueries) != 1 {
		t.Fatalf("expected only the regressed query, got %+v", slowQueries)
	}
	sq := slowQueries[0]
	if sq.QueryID != 101 || sq.MeanExecTime != 1500 || sq.Calls != 10 {
		t.Errorf("got %+v, want query 101 with window mean 1500 over 10 calls", sq)
	}
	if sq.Window != 25*time.Hour {
		t.Errorf("Window = %s, want 25h", sq.Window)
	}
	if sq.LifetimeMeanExecTime != 300 || sq.MaxExecTime != 9000 {
		t.Errorf("lifetime figures = mean %v max %v, want 300 and 9000", sq.LifetimeMeanExecTime, sq.MaxExecTime)
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
			StatsSince:   daysAgo(45),
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

func daysAgo(days int) *time.Time {
	t := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	return &t
}

// btreeIndex builds an index on public.orders with the given key tokens.
func btreeIndex(name string, keys string, scans int64) models.IndexStat {
	return models.IndexStat{
		SchemaName: "public", RelName: "orders", IndexRelName: name,
		IdxScan: scans, IndexSize: 1 << 20, AccessMethod: "btree",
		KeyColumns: keys, IndexDef: "CREATE INDEX " + name,
	}
}

func TestIndexAnalyzer_UnusedIndexGuards(t *testing.T) {
	base := models.IndexStat{
		SchemaName: "public", RelName: "orders", IdxScan: 0, IndexSize: 1 << 20,
	}
	tests := []struct {
		name         string
		mutate       func(*models.IndexStat)
		wantFlagged  bool
		wantSeverity string
	}{
		{"old enough window", func(s *models.IndexStat) { s.StatsSince = daysAgo(31) }, true, models.SeverityInfo},
		{"window shorter than unused_index_days", func(s *models.IndexStat) { s.StatsSince = daysAgo(3) }, false, ""},
		{"unknown stats window", func(s *models.IndexStat) { s.StatsSince = nil }, false, ""},
		{"backs a foreign key", func(s *models.IndexStat) {
			s.StatsSince = daysAgo(90)
			s.BacksForeignKey = true
		}, false, ""},
		{"large index is a warning, never critical", func(s *models.IndexStat) {
			s.StatsSince = daysAgo(90)
			s.IndexSize = 5 << 30
		}, true, models.SeverityWarning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stat := base
			stat.IndexRelName = "idx_candidate"
			tt.mutate(&stat)

			a := NewIndexAnalyzer(newMockStorage(), nil)
			issues := a.detectUnusedIndexes([]models.IndexStat{stat}, time.Now())
			if got := len(issues) == 1; got != tt.wantFlagged {
				t.Fatalf("flagged = %v, want %v (issues: %+v)", got, tt.wantFlagged, issues)
			}
			if tt.wantFlagged && issues[0].Severity != tt.wantSeverity {
				t.Errorf("severity = %s, want %s", issues[0].Severity, tt.wantSeverity)
			}
		})
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

	// user_id is attnum 2 with int4 opclass 1978; created_at is attnum 4.
	storage.indexStats[1] = []models.IndexStat{
		btreeIndex("idx_orders_user_created", "2:1978:0:0 4:3127:0:0", 1000),
		btreeIndex("idx_orders_user", "2:1978:0:0", 10),
		// Name looks like a duplicate but the column differs.
		btreeIndex("idx_orders_user_old", "3:3126:100:0", 5),
	}

	analyzer := NewIndexAnalyzer(storage, nil)
	issues, err := analyzer.Analyze(ctx, 1)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var dups []IndexIssue
	for _, issue := range issues {
		if issue.IssueType == IndexIssueDuplicate {
			dups = append(dups, issue)
		}
	}
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate issue, got %d: %+v", len(dups), dups)
	}
	issue := dups[0]
	if issue.IndexName != "idx_orders_user" || issue.DuplicateOf != "idx_orders_user_created" {
		t.Errorf("got %s covered by %s, want idx_orders_user covered by idx_orders_user_created",
			issue.IndexName, issue.DuplicateOf)
	}
	if issue.ExactDuplicate {
		t.Error("prefix redundancy must not be reported as an exact duplicate")
	}
	if issue.DuplicateOfIdxScan != 1000 {
		t.Errorf("Expected duplicate_of_idx_scan 1000, got %d", issue.DuplicateOfIdxScan)
	}
	if issue.DuplicateOfDef != "CREATE INDEX idx_orders_user_created" {
		t.Errorf("DuplicateOfDef = %q", issue.DuplicateOfDef)
	}
}

// TestIndexAnalyzer_DuplicatesPointAtKeptIndex covers a chain: two identical
// indexes that are both prefixes of a wider one. Both must be reported against
// the wider index, never against each other, since that one is dropped too.
func TestIndexAnalyzer_DuplicatesPointAtKeptIndex(t *testing.T) {
	a := NewIndexAnalyzer(newMockStorage(), nil)
	issues := a.detectDuplicateIndexes([]models.IndexStat{
		btreeIndex("o_user", "2:1978:0:0", 0),
		btreeIndex("o_user_dup", "2:1978:0:0", 0),
		btreeIndex("o_user_created", "2:1978:0:0 4:3127:0:0", 0),
	})

	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %+v", issues)
	}
	for _, issue := range issues {
		if issue.DuplicateOf != "o_user_created" {
			t.Errorf("%s reported against %s, want o_user_created", issue.IndexName, issue.DuplicateOf)
		}
	}
}

func TestRedundantWith(t *testing.T) {
	withInclude := func(s models.IndexStat, inc string) models.IndexStat { s.IncludeColumns = inc; return s }
	withPred := func(s models.IndexStat, p string) models.IndexStat { s.Predicate = p; return s }
	withAM := func(s models.IndexStat, am string) models.IndexStat { s.AccessMethod = am; return s }
	unique := func(s models.IndexStat) models.IndexStat { s.IsUnique = true; return s }

	a := btreeIndex("a", "2:1978:0:0", 0)
	ab := btreeIndex("ab", "2:1978:0:0 4:3127:0:0", 0)

	tests := []struct {
		name      string
		candidate models.IndexStat
		retained  models.IndexStat
		wantOK    bool
		wantExact bool
	}{
		{"prefix is redundant", a, ab, true, false},
		{"superset is not redundant", ab, a, false, false},
		{"exact duplicate, fewer scans dropped", btreeIndex("x", "2:1978:0:0", 1), btreeIndex("y", "2:1978:0:0", 9), true, true},
		{"exact duplicate, more scans kept", btreeIndex("y", "2:1978:0:0", 9), btreeIndex("x", "2:1978:0:0", 1), false, false},
		{"exact duplicate tie reports only one", btreeIndex("b", "2:1978:0:0", 0), btreeIndex("a", "2:1978:0:0", 0), true, true},
		{"exact duplicate tie other direction", btreeIndex("a", "2:1978:0:0", 0), btreeIndex("b", "2:1978:0:0", 0), false, false},
		{"unique candidate is kept", unique(a), ab, false, false},
		{"plain duplicate of unique index", a, unique(btreeIndex("u", "2:1978:0:0", 0)), true, true},
		{"different sort order", a, btreeIndex("desc", "2:1978:0:3", 0), false, false},
		{"different opclass", a, btreeIndex("ops", "2:3128:0:0", 0), false, false},
		{"partial vs full", withPred(a, "(status = 'open')"), ab, false, false},
		{"same predicate prefix", withPred(a, "(x)"), withPred(ab, "(x)"), true, false},
		{"include column not in retained", withInclude(a, "5"), ab, false, false},
		{"include column is a retained key", withInclude(a, "4"), ab, true, false},
		{"include sets compared unordered", withInclude(btreeIndex("b", "2:1978:0:0", 0), "5 6"), withInclude(btreeIndex("a", "2:1978:0:0", 0), "6 5"), true, true},
		{"prefix only counts for btree", withAM(a, "hash"), withAM(ab, "hash"), false, false},
		{"different access method", a, withAM(btreeIndex("g", "2:1978:0:0", 0), "gin"), false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exact, ok := redundantWith(tt.candidate, tt.retained)
			if ok != tt.wantOK || exact != tt.wantExact {
				t.Errorf("redundantWith = (exact=%v, ok=%v), want (exact=%v, ok=%v)", exact, ok, tt.wantExact, tt.wantOK)
			}
		})
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

// TestMainAnalyzer_ResolvesDomainsAcrossSnapshots covers the core scheduling problem:
// the newest snapshot is usually a partial one cut for the 30s collectors, so reading
// every domain from it reported tables and indexes as empty on nearly every run.
// Each domain must instead be read from the most recent snapshot that actually holds
// it, and the result must say how fresh that data is.
func TestMainAnalyzer_ResolvesDomainsAcrossSnapshots(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	now := time.Now()
	// Snapshot 1: a full cycle, five minutes ago.
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-5 * time.Minute)}
	// Snapshot 2: the newest one, holding only the fast-moving collectors.
	storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}

	storage.tableStats[1] = []models.TableStat{
		{SchemaName: "public", RelName: "orders", NLiveTup: 100000, SeqScan: 900, IdxScan: 100},
	}
	storage.indexStats[1] = []models.IndexStat{
		{SchemaName: "public", RelName: "orders", IndexRelName: "orders_unused_idx", IdxScan: 0, IndexSize: 1 << 20, StatsSince: daysAgo(60)},
	}
	storage.queryStats[2] = []models.QueryStat{
		{QueryID: 100, Query: "SELECT * FROM orders", MeanExecTime: 2000, Calls: 100, TotalExecTime: 200000},
	}

	cfg := DefaultConfig()
	cfg.CollectorIntervals = map[string]time.Duration{
		DomainQueryStats: time.Minute,
		DomainTableStats: 5 * time.Minute,
		DomainIndexStats: 5 * time.Minute,
	}

	result, err := NewMainAnalyzer(storage, cfg).Analyze(ctx, 2)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Table and index data live in the older snapshot and must still be analysed.
	if len(result.TableIssues) == 0 {
		t.Error("expected table issues from the older snapshot, got none")
	}
	if len(result.IndexIssues) == 0 {
		t.Error("expected index issues from the older snapshot, got none")
	}
	if len(result.SlowQueries) != 1 {
		t.Errorf("SlowQueries = %d, want 1", len(result.SlowQueries))
	}

	if got := result.DomainCoverage(DomainTableStats); !got.Present || got.SnapshotID != 1 {
		t.Errorf("table_stats coverage = %+v, want present from snapshot 1", got)
	}
	if got := result.DomainCoverage(DomainQueryStats); !got.Present || got.SnapshotID != 2 {
		t.Errorf("query_stats coverage = %+v, want present from snapshot 2", got)
	}
	// 5 minutes old against a 5 minute interval is within the 3-interval budget.
	if !result.DomainsUsable(DomainTableStats, DomainIndexStats, DomainQueryStats) {
		t.Error("expected all three domains to be usable")
	}

	// A domain that was never collected must read as absent, not as clean.
	if got := result.DomainCoverage(DomainBloat); got.Present {
		t.Errorf("bloat coverage = %+v, want absent", got)
	}
	if result.DomainsUsable(DomainBloat) {
		t.Error("bloat must not be usable when it was never collected")
	}
}

// TestMainAnalyzer_MarksStaleDomains verifies that data older than a domain's
// staleness budget is reported as stale, so callers stop concluding "no issues" from
// it even though the rows are present.
func TestMainAnalyzer_MarksStaleDomains(t *testing.T) {
	ctx := context.Background()
	storage := newMockStorage()

	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	storage.tableStats[1] = []models.TableStat{{SchemaName: "public", RelName: "orders", NLiveTup: 10}}

	cfg := DefaultConfig()
	cfg.CollectorIntervals = map[string]time.Duration{DomainTableStats: 5 * time.Minute}

	result, err := NewMainAnalyzer(storage, cfg).Analyze(ctx, 2)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	got := result.DomainCoverage(DomainTableStats)
	if !got.Present {
		t.Fatal("expected table_stats to be present")
	}
	if !got.Stale {
		t.Errorf("table_stats coverage = %+v, want stale (2h old against a 15m budget)", got)
	}
	if result.DomainsUsable(DomainTableStats) {
		t.Error("stale data must not be usable")
	}
}

func TestMainAnalyzer_UsesDatabaseCollectorCacheRatio(t *testing.T) {
	storage := newMockStorage()
	now := time.Now()
	ratio := 82.0
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-time.Minute), CacheHitRatio: &ratio}
	storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	storage.queryStats[2] = []models.QueryStat{{QueryID: 7, Query: "SELECT 1", Calls: 5}}
	storage.extendedStats[1] = &models.ExtendedDatabaseStats{}
	storage.coverage = map[string][]int64{DomainDatabaseStats: {1}, DomainQueryStats: {2}}

	result, err := NewMainAnalyzer(storage, nil).Analyze(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheStats == nil || result.CacheStats.OverallHitRatio != ratio || !result.CacheStats.BelowThreshold {
		t.Fatalf("expected overall cache ratio from database snapshot, got %+v", result.CacheStats)
	}
	if result.CacheStats.TrackedQueries != 1 || !result.DomainsUsable(DomainDatabaseStats, DomainQueryStats) {
		t.Fatalf("expected query details and usable domains, got cache=%+v coverage=%+v", result.CacheStats, result.Coverage)
	}
}

func TestMainAnalyzer_DoesNotResolveOnQueryReadFailure(t *testing.T) {
	storage := newMockStorage()
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: time.Now()}
	storage.queryStats[1] = []models.QueryStat{{QueryID: 7, Query: "SELECT 1", Calls: 5}}
	storage.queryStatsErr = errors.New("disk read failed")
	result, err := NewMainAnalyzer(storage, nil).Analyze(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount == 0 || result.DomainsUsable(DomainQueryStats) || !result.DomainCoverage(DomainQueryStats).Stale {
		t.Fatalf("query read failure must invalidate coverage, got %+v", result)
	}
}
