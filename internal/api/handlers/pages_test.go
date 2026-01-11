package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/models"
	"github.com/user/pganalyzer/internal/web"
)

// mockPageStorage implements PageStorage for testing.
type mockPageStorage struct {
	snapshot    *models.Snapshot
	queryStats  []models.QueryStat
	suggestions []models.Suggestion
	tableStats  []models.TableStat
	indexStats  []models.IndexStat
	bloatStats  []models.BloatInfo
	explainPlan *models.ExplainPlan
	err         error
}

func (m *mockPageStorage) GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error) {
	return m.snapshot, m.err
}

func (m *mockPageStorage) GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error) {
	return m.queryStats, m.err
}

func (m *mockPageStorage) GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error) {
	// Filter suggestions by status for testing
	var filtered []models.Suggestion
	for _, s := range m.suggestions {
		if s.Status == status {
			filtered = append(filtered, s)
		}
	}
	return filtered, m.err
}

func (m *mockPageStorage) GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error) {
	return m.tableStats, m.err
}

func (m *mockPageStorage) GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error) {
	return m.indexStats, m.err
}

func (m *mockPageStorage) GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error) {
	return m.bloatStats, m.err
}

func (m *mockPageStorage) GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error) {
	return m.explainPlan, m.err
}

// mockRenderer implements echo.Renderer for testing.
type mockRenderer struct {
	lastTemplate string
	lastData     interface{}
}

func (r *mockRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	r.lastTemplate = name
	r.lastData = data
	_, err := w.Write([]byte("rendered"))
	return err
}

func setupTestEcho() *echo.Echo {
	e := echo.New()
	renderer, _ := web.NewTemplateRenderer()
	e.Renderer = renderer
	return e
}

func TestDashboardPage(t *testing.T) {
	e := setupTestEcho()
	cacheRatio := 99.5
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{
			ID:            1,
			CapturedAt:    time.Now(),
			CacheHitRatio: &cacheRatio,
		},
		queryStats: []models.QueryStat{
			{QueryID: 1, Query: "SELECT 1", Calls: 100, TotalExecTime: 1000, MeanExecTime: 10},
			{QueryID: 2, Query: "SELECT 2", Calls: 50, TotalExecTime: 5000, MeanExecTime: 100},
		},
		suggestions: []models.Suggestion{
			{ID: 1, Severity: "warning", Title: "Test suggestion", TargetObject: "test_table"},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Dashboard(c)
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Dashboard() status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("Dashboard() response should contain 'Dashboard'")
	}
}

func TestDashboardPageNoSnapshot(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: nil,
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Dashboard(c)
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Dashboard() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestQueriesPage(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		queryStats: []models.QueryStat{
			{QueryID: 1, Query: "SELECT 1", Calls: 100, TotalExecTime: 1000, MeanExecTime: 10, SharedBlksHit: 90, SharedBlksRead: 10},
			{QueryID: 2, Query: "SELECT 2", Calls: 50, TotalExecTime: 5000, MeanExecTime: 100, SharedBlksHit: 80, SharedBlksRead: 20},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/queries?sort=total_time&order=desc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Queries(c)
	if err != nil {
		t.Fatalf("Queries() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Queries() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestQueriesPagePagination(t *testing.T) {
	e := setupTestEcho()

	// Create many queries for pagination testing
	var stats []models.QueryStat
	for i := 0; i < 50; i++ {
		stats = append(stats, models.QueryStat{
			QueryID:       int64(i + 1),
			Query:         "SELECT " + string(rune('0'+i)),
			Calls:         100,
			TotalExecTime: float64(i * 100),
		})
	}

	storage := &mockPageStorage{
		snapshot:   &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		queryStats: stats,
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/queries?page=2", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Queries(c)
	if err != nil {
		t.Fatalf("Queries() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Queries() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestQueryDetailPage(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		queryStats: []models.QueryStat{
			{QueryID: 123, Query: "SELECT * FROM users", Calls: 100, TotalExecTime: 1000, MeanExecTime: 10, SharedBlksHit: 90, SharedBlksRead: 10},
		},
		explainPlan: &models.ExplainPlan{
			QueryID:    123,
			PlanText:   "Seq Scan on users",
			CapturedAt: time.Now(),
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/queries/123", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("123")

	err := handler.QueryDetail(c)
	if err != nil {
		t.Fatalf("QueryDetail() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("QueryDetail() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestQueryDetailPageNotFound(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot:   &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		queryStats: []models.QueryStat{},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/queries/999", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	err := handler.QueryDetail(c)
	if err != nil {
		t.Fatalf("QueryDetail() error = %v", err)
	}

	// Should redirect when query not found
	if rec.Code != http.StatusFound {
		t.Errorf("QueryDetail() status = %d, want %d", rec.Code, http.StatusFound)
	}
}

func TestSchemaPageTables(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		tableStats: []models.TableStat{
			{SchemaName: "public", RelName: "users", TableSize: 1024, NLiveTup: 100, NDeadTup: 5},
			{SchemaName: "public", RelName: "orders", TableSize: 2048, NLiveTup: 500, NDeadTup: 10},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/schema?tab=tables", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Schema(c)
	if err != nil {
		t.Fatalf("Schema() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Schema() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSchemaPageIndexes(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		indexStats: []models.IndexStat{
			{SchemaName: "public", RelName: "users", IndexRelName: "users_pkey", IdxScan: 1000, IndexSize: 4096, IsPrimary: true},
			{SchemaName: "public", RelName: "users", IndexRelName: "users_email_idx", IdxScan: 500, IndexSize: 2048, IsUnique: true},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/schema?tab=indexes", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Schema(c)
	if err != nil {
		t.Fatalf("Schema() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Schema() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSchemaPageBloat(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		bloatStats: []models.BloatInfo{
			{SchemaName: "public", RelName: "old_table", NLiveTup: 1000, NDeadTup: 500, BloatPercent: 50.0},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/schema?tab=bloat", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Schema(c)
	if err != nil {
		t.Fatalf("Schema() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Schema() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSuggestionsPage(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		suggestions: []models.Suggestion{
			{ID: 1, RuleID: "slow_query", Severity: "critical", Title: "Slow query detected", TargetObject: "query_123", Status: "active", FirstSeenAt: time.Now(), LastSeenAt: time.Now()},
			{ID: 2, RuleID: "unused_index", Severity: "warning", Title: "Unused index", TargetObject: "idx_old", Status: "active", FirstSeenAt: time.Now(), LastSeenAt: time.Now()},
			{ID: 3, RuleID: "low_cache", Severity: "info", Title: "Low cache hit", TargetObject: "database", Status: "active", FirstSeenAt: time.Now(), LastSeenAt: time.Now()},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/suggestions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Suggestions(c)
	if err != nil {
		t.Fatalf("Suggestions() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Suggestions() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSuggestionsPageWithFilter(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		suggestions: []models.Suggestion{
			{ID: 1, RuleID: "slow_query", Severity: "critical", Title: "Slow query detected", Status: "active", FirstSeenAt: time.Now(), LastSeenAt: time.Now()},
			{ID: 2, RuleID: "unused_index", Severity: "warning", Title: "Unused index", Status: "active", FirstSeenAt: time.Now(), LastSeenAt: time.Now()},
		},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/suggestions?severity=critical", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Suggestions(c)
	if err != nil {
		t.Fatalf("Suggestions() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Suggestions() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSuggestionsPageEmpty(t *testing.T) {
	e := setupTestEcho()
	storage := &mockPageStorage{
		snapshot:    &models.Snapshot{ID: 1, CapturedAt: time.Now()},
		suggestions: []models.Suggestion{},
	}

	handler := NewPageHandler(storage, 1, "1.0.0")

	req := httptest.NewRequest(http.MethodGet, "/suggestions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.Suggestions(c)
	if err != nil {
		t.Fatalf("Suggestions() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Suggestions() status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("truncateString", func(t *testing.T) {
		if result := truncateString("hello world", 8); result != "hello..." {
			t.Errorf("truncateString() = %v, want hello...", result)
		}
		if result := truncateString("hello", 10); result != "hello" {
			t.Errorf("truncateString() = %v, want hello", result)
		}
	})

	t.Run("calculateRowsPerCall", func(t *testing.T) {
		if result := calculateRowsPerCall(100, 10); result != 10.0 {
			t.Errorf("calculateRowsPerCall(100, 10) = %v, want 10.0", result)
		}
		if result := calculateRowsPerCall(100, 0); result != 0 {
			t.Errorf("calculateRowsPerCall(100, 0) = %v, want 0", result)
		}
	})

	t.Run("calculateCacheHitRatio", func(t *testing.T) {
		if result := calculateCacheHitRatio(90, 10); result != 90.0 {
			t.Errorf("calculateCacheHitRatio(90, 10) = %v, want 90.0", result)
		}
		if result := calculateCacheHitRatio(0, 0); result != 100.0 {
			t.Errorf("calculateCacheHitRatio(0, 0) = %v, want 100.0", result)
		}
	})
}

func TestSortQueryStats(t *testing.T) {
	stats := []models.QueryStat{
		{QueryID: 1, Calls: 100, MeanExecTime: 50, TotalExecTime: 5000, Rows: 1000},
		{QueryID: 2, Calls: 50, MeanExecTime: 100, TotalExecTime: 5000, Rows: 500},
		{QueryID: 3, Calls: 200, MeanExecTime: 25, TotalExecTime: 5000, Rows: 2000},
	}

	t.Run("sort by calls desc", func(t *testing.T) {
		statsCopy := make([]models.QueryStat, len(stats))
		copy(statsCopy, stats)
		sortQueryStats(statsCopy, "calls", true)
		if statsCopy[0].QueryID != 3 {
			t.Errorf("First query should be ID 3, got %d", statsCopy[0].QueryID)
		}
	})

	t.Run("sort by mean_time desc", func(t *testing.T) {
		statsCopy := make([]models.QueryStat, len(stats))
		copy(statsCopy, stats)
		sortQueryStats(statsCopy, "mean_time", true)
		if statsCopy[0].QueryID != 2 {
			t.Errorf("First query should be ID 2, got %d", statsCopy[0].QueryID)
		}
	})

	t.Run("sort by rows asc", func(t *testing.T) {
		statsCopy := make([]models.QueryStat, len(stats))
		copy(statsCopy, stats)
		sortQueryStats(statsCopy, "rows", false)
		if statsCopy[0].QueryID != 2 {
			t.Errorf("First query should be ID 2, got %d", statsCopy[0].QueryID)
		}
	})
}
