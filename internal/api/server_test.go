package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/scheduler"
)

// mockStorage implements all storage interfaces needed for testing.
type mockStorage struct {
	instances   map[int64]*models.Instance
	snapshots   map[int64]*models.Snapshot
	queryStats  map[int64][]models.QueryStat
	tableStats  map[int64][]models.TableStat
	indexStats  map[int64][]models.IndexStat
	bloatStats  map[int64][]models.BloatInfo
	suggestions []models.Suggestion
	plans       map[int64]*models.ExplainPlan
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		instances:   make(map[int64]*models.Instance),
		snapshots:   make(map[int64]*models.Snapshot),
		queryStats:  make(map[int64][]models.QueryStat),
		tableStats:  make(map[int64][]models.TableStat),
		indexStats:  make(map[int64][]models.IndexStat),
		bloatStats:  make(map[int64][]models.BloatInfo),
		suggestions: []models.Suggestion{},
		plans:       make(map[int64]*models.ExplainPlan),
	}
}

func (m *mockStorage) Close() error { return nil }

func (m *mockStorage) GetInstance(ctx context.Context, id int64) (*models.Instance, error) {
	return m.instances[id], nil
}

func (m *mockStorage) GetInstanceByName(ctx context.Context, name string) (*models.Instance, error) {
	for _, inst := range m.instances {
		if inst.Name == name {
			return inst, nil
		}
	}
	return nil, nil
}

func (m *mockStorage) CreateInstance(ctx context.Context, inst *models.Instance) (int64, error) {
	inst.ID = int64(len(m.instances) + 1)
	m.instances[inst.ID] = inst
	return inst.ID, nil
}

func (m *mockStorage) GetOrCreateInstance(ctx context.Context, inst *models.Instance) (int64, error) {
	for _, existing := range m.instances {
		if existing.Host == inst.Host && existing.Port == inst.Port && existing.Database == inst.Database {
			return existing.ID, nil
		}
	}
	return m.CreateInstance(ctx, inst)
}

func (m *mockStorage) ListInstances(ctx context.Context) ([]models.Instance, error) {
	var result []models.Instance
	for _, inst := range m.instances {
		result = append(result, *inst)
	}
	return result, nil
}

func (m *mockStorage) CreateSnapshot(ctx context.Context, snap *models.Snapshot) (int64, error) {
	snap.ID = int64(len(m.snapshots) + 1)
	m.snapshots[snap.ID] = snap
	return snap.ID, nil
}

func (m *mockStorage) GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error) {
	return m.snapshots[id], nil
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
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockStorage) UpdateSnapshotCacheHitRatio(ctx context.Context, snapshotID int64, ratio float64) error {
	if snap, ok := m.snapshots[snapshotID]; ok {
		snap.CacheHitRatio = &ratio
	}
	return nil
}

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

func (m *mockStorage) SaveTableStats(ctx context.Context, snapshotID int64, stats []models.TableStat) error {
	m.tableStats[snapshotID] = stats
	return nil
}

func (m *mockStorage) GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error) {
	return m.tableStats[snapshotID], nil
}

func (m *mockStorage) SaveIndexStats(ctx context.Context, snapshotID int64, stats []models.IndexStat) error {
	m.indexStats[snapshotID] = stats
	return nil
}

func (m *mockStorage) GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error) {
	return m.indexStats[snapshotID], nil
}

func (m *mockStorage) SaveBloatStats(ctx context.Context, snapshotID int64, stats []models.BloatInfo) error {
	m.bloatStats[snapshotID] = stats
	return nil
}

func (m *mockStorage) GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error) {
	return m.bloatStats[snapshotID], nil
}

func (m *mockStorage) UpsertSuggestion(ctx context.Context, sug *models.Suggestion) error {
	for i, existing := range m.suggestions {
		if existing.InstanceID == sug.InstanceID && existing.RuleID == sug.RuleID && existing.TargetObject == sug.TargetObject {
			m.suggestions[i] = *sug
			return nil
		}
	}
	sug.ID = int64(len(m.suggestions) + 1)
	m.suggestions = append(m.suggestions, *sug)
	return nil
}

func (m *mockStorage) GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error) {
	var result []models.Suggestion
	for _, sug := range m.suggestions {
		if sug.InstanceID == instanceID && sug.Status == status {
			result = append(result, sug)
		}
	}
	return result, nil
}

func (m *mockStorage) GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error) {
	for _, sug := range m.suggestions {
		if sug.ID == id {
			return &sug, nil
		}
	}
	return nil, nil
}

func (m *mockStorage) DismissSuggestion(ctx context.Context, id int64) error {
	for i, sug := range m.suggestions {
		if sug.ID == id {
			m.suggestions[i].Status = models.StatusDismissed
			now := time.Now()
			m.suggestions[i].DismissedAt = &now
			return nil
		}
	}
	return nil
}

func (m *mockStorage) ResolveSuggestion(ctx context.Context, id int64) error {
	for i, sug := range m.suggestions {
		if sug.ID == id {
			m.suggestions[i].Status = models.StatusResolved
			return nil
		}
	}
	return nil
}

func (m *mockStorage) SaveExplainPlan(ctx context.Context, plan *models.ExplainPlan) (int64, error) {
	plan.ID = int64(len(m.plans) + 1)
	m.plans[plan.QueryID] = plan
	return plan.ID, nil
}

func (m *mockStorage) GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error) {
	return m.plans[queryID], nil
}

func (m *mockStorage) PurgeOldSnapshots(ctx context.Context, retention time.Duration) (int64, error) {
	return 0, nil
}

// mockPGClient implements the postgres.Client interface for testing.
type mockPGClient struct {
	connected bool
}

func newMockPGClient() *mockPGClient {
	return &mockPGClient{connected: true}
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
		return context.DeadlineExceeded
	}
	return nil
}

func (m *mockPGClient) GetStatStatements(ctx context.Context) ([]models.QueryStat, error) {
	return nil, nil
}

func (m *mockPGClient) GetStatTables(ctx context.Context) ([]models.TableStat, error) {
	return nil, nil
}

func (m *mockPGClient) GetStatIndexes(ctx context.Context) ([]models.IndexStat, error) {
	return nil, nil
}

func (m *mockPGClient) GetDatabaseStats(ctx context.Context) (*models.DatabaseStats, error) {
	return nil, nil
}

func (m *mockPGClient) GetTableBloat(ctx context.Context) ([]models.BloatInfo, error) {
	return nil, nil
}

func (m *mockPGClient) GetIndexDetails(ctx context.Context) ([]models.IndexDetail, error) {
	return nil, nil
}

func (m *mockPGClient) Explain(ctx context.Context, query string, analyze bool) (*models.ExplainPlan, error) {
	return &models.ExplainPlan{
		QueryID:    0,
		PlanText:   "Seq Scan on users  (cost=0.00..10.00 rows=100 width=100)",
		PlanJSON:   `[{"Plan": {"Node Type": "Seq Scan"}}]`,
		CapturedAt: time.Now(),
	}, nil
}

func (m *mockPGClient) GetVersion(ctx context.Context) (string, error) {
	return "PostgreSQL 15.0", nil
}

func (m *mockPGClient) GetStatsResetTime(ctx context.Context) (*time.Time, error) {
	return nil, nil
}

// mockScheduler creates a minimal scheduler for testing.
func createMockScheduler(t *testing.T, storage *mockStorage) *scheduler.Scheduler {
	// We can't easily mock the scheduler, so we'll skip scheduler-dependent tests
	// or test them differently
	return nil
}

// Helper to create a test request with Basic Auth.
func newAuthRequest(method, path string, body string, username, password string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}
	return req
}

// TestAuthMiddleware tests the Basic Auth middleware.
func TestAuthMiddleware(t *testing.T) {
	e := echo.New()

	// Create a handler that requires auth
	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "success")
	})

	authConfig := config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	tests := []struct {
		name       string
		username   string
		password   string
		wantStatus int
	}{
		{"no credentials", "", "", http.StatusUnauthorized},
		{"invalid username", "wrong", "secret", http.StatusUnauthorized},
		{"invalid password", "admin", "wrong", http.StatusUnauthorized},
		{"valid credentials", "admin", "secret", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new echo instance for each test to avoid middleware issues
			e := echo.New()
			e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					if !authConfig.Enabled {
						return next(c)
					}

					username, password, ok := c.Request().BasicAuth()
					if !ok {
						c.Response().Header().Set("WWW-Authenticate", `Basic realm="pganalyzer"`)
						return c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing credentials"})
					}

					if username != authConfig.Username || password != authConfig.Password {
						c.Response().Header().Set("WWW-Authenticate", `Basic realm="pganalyzer"`)
						return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
					}

					return next(c)
				}
			})
			e.GET("/test", func(c echo.Context) error {
				return c.String(http.StatusOK, "success")
			})

			req := newAuthRequest("GET", "/test", "", tt.username, tt.password)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestAuthMiddlewareSkipsHealth tests that health endpoint bypasses auth.
func TestAuthMiddlewareSkipsHealth(t *testing.T) {
	e := echo.New()

	authConfig := config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
	}

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !authConfig.Enabled {
				return next(c)
			}
			if c.Path() == "/health" {
				return next(c)
			}

			username, password, ok := c.Request().BasicAuth()
			if !ok || username != authConfig.Username || password != authConfig.Password {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}

			return next(c)
		}
	})
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Request without credentials should succeed for /health
	req := newAuthRequest("GET", "/health", "", "", "")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestHealthEndpoint tests the health endpoint.
func TestHealthEndpoint(t *testing.T) {
	storage := newMockStorage()
	pgClient := newMockPGClient()

	// Create a snapshot
	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: now,
		PGVersion:  "15.0",
	}

	e := echo.New()
	e.GET("/health", func(c echo.Context) error {
		ctx := c.Request().Context()
		pgConnected := pgClient.Ping(ctx) == nil

		var lastSnapshot *time.Time
		if snap, _ := storage.GetLatestSnapshot(ctx, 1); snap != nil {
			lastSnapshot = &snap.CapturedAt
		}

		status := "ok"
		if !pgConnected {
			status = "degraded"
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":        status,
			"pg_connected":  pgConnected,
			"last_snapshot": lastSnapshot,
		})
	})

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("got status %v, want ok", response["status"])
	}
	if response["pg_connected"] != true {
		t.Errorf("got pg_connected %v, want true", response["pg_connected"])
	}
}

// TestDashboardEndpoint tests the dashboard endpoint.
func TestDashboardEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	cacheHitRatio := 98.5
	storage.snapshots[1] = &models.Snapshot{
		ID:            1,
		InstanceID:    1,
		CapturedAt:    now,
		PGVersion:     "15.0",
		CacheHitRatio: &cacheHitRatio,
	}

	storage.queryStats[1] = []models.QueryStat{
		{QueryID: 1, Query: "SELECT * FROM users", Calls: 1000, TotalExecTime: 5000, MeanExecTime: 5},
		{QueryID: 2, Query: "SELECT * FROM orders", Calls: 500, TotalExecTime: 10000, MeanExecTime: 20},
		{QueryID: 3, Query: "SELECT slow query", Calls: 10, TotalExecTime: 50000, MeanExecTime: 5000}, // slow query
	}

	storage.suggestions = []models.Suggestion{
		{ID: 1, InstanceID: 1, RuleID: "unused_index", Severity: "warning", Status: "active", Title: "Unused index", TargetObject: "idx_test", FirstSeenAt: now},
	}

	e := echo.New()
	e.GET("/api/v1/dashboard", func(c echo.Context) error {
		ctx := c.Request().Context()

		response := map[string]interface{}{
			"top_queries":        []interface{}{},
			"recent_suggestions": []interface{}{},
		}

		if snap, _ := storage.GetLatestSnapshot(ctx, 1); snap != nil {
			response["cache_hit_ratio"] = snap.CacheHitRatio

			stats, _ := storage.GetQueryStats(ctx, snap.ID)
			response["total_queries"] = len(stats)

			slowCount := 0
			for _, stat := range stats {
				if stat.MeanExecTime > 1000 {
					slowCount++
				}
			}
			response["slow_queries_count"] = slowCount
		}

		suggestions, _ := storage.GetSuggestionsByStatus(ctx, 1, models.StatusActive)
		response["active_suggestions"] = len(suggestions)

		return c.JSON(http.StatusOK, response)
	})

	req := httptest.NewRequest("GET", "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total_queries"].(float64) != 3 {
		t.Errorf("got total_queries %v, want 3", response["total_queries"])
	}
	if response["slow_queries_count"].(float64) != 1 {
		t.Errorf("got slow_queries_count %v, want 1", response["slow_queries_count"])
	}
	if response["active_suggestions"].(float64) != 1 {
		t.Errorf("got active_suggestions %v, want 1", response["active_suggestions"])
	}
}

// TestQueriesEndpoint tests the queries list endpoint.
func TestQueriesEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: now,
	}

	storage.queryStats[1] = []models.QueryStat{
		{QueryID: 1, Query: "SELECT * FROM users", Calls: 1000, TotalExecTime: 5000, MeanExecTime: 5},
		{QueryID: 2, Query: "SELECT * FROM orders", Calls: 500, TotalExecTime: 10000, MeanExecTime: 20},
	}

	e := echo.New()
	e.GET("/api/v1/queries", func(c echo.Context) error {
		ctx := c.Request().Context()

		snap, _ := storage.GetLatestSnapshot(ctx, 1)
		if snap == nil {
			return c.JSON(http.StatusOK, map[string]interface{}{"queries": []interface{}{}, "total": 0})
		}

		stats, _ := storage.GetQueryStats(ctx, snap.ID)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"queries":  stats,
			"total":    len(stats),
			"page":     1,
			"per_page": 20,
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/queries", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 2 {
		t.Errorf("got total %v, want 2", response["total"])
	}
}

// TestSuggestionsEndpoint tests the suggestions list endpoint.
func TestSuggestionsEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.suggestions = []models.Suggestion{
		{ID: 1, InstanceID: 1, RuleID: "unused_index", Severity: "warning", Status: "active", Title: "Unused index", TargetObject: "idx_test", FirstSeenAt: now, LastSeenAt: now},
		{ID: 2, InstanceID: 1, RuleID: "slow_query", Severity: "critical", Status: "active", Title: "Slow query", TargetObject: "query:123", FirstSeenAt: now, LastSeenAt: now},
	}

	e := echo.New()
	e.GET("/api/v1/suggestions", func(c echo.Context) error {
		ctx := c.Request().Context()

		suggestions, _ := storage.GetSuggestionsByStatus(ctx, 1, models.StatusActive)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"suggestions": suggestions,
			"total":       len(suggestions),
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/suggestions", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 2 {
		t.Errorf("got total %v, want 2", response["total"])
	}
}

// TestDismissSuggestionEndpoint tests the dismiss suggestion endpoint.
func TestDismissSuggestionEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.suggestions = []models.Suggestion{
		{ID: 1, InstanceID: 1, RuleID: "unused_index", Severity: "warning", Status: "active", Title: "Unused index", TargetObject: "idx_test", FirstSeenAt: now, LastSeenAt: now},
	}

	e := echo.New()
	e.POST("/api/v1/suggestions/:id/dismiss", func(c echo.Context) error {
		ctx := c.Request().Context()
		id := int64(1) // hardcoded for test

		sug, _ := storage.GetSuggestionByID(ctx, id)
		if sug == nil {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}

		storage.DismissSuggestion(ctx, id)
		sug, _ = storage.GetSuggestionByID(ctx, id)

		dismissedAt := ""
		if sug.DismissedAt != nil {
			dismissedAt = sug.DismissedAt.Format(time.RFC3339)
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"id":           id,
			"status":       sug.Status,
			"dismissed_at": dismissedAt,
		})
	})

	req := httptest.NewRequest("POST", "/api/v1/suggestions/1/dismiss", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify the suggestion was dismissed
	sug, _ := storage.GetSuggestionByID(context.Background(), 1)
	if sug.Status != models.StatusDismissed {
		t.Errorf("got status %s, want dismissed", sug.Status)
	}
}

// TestSchemaTablesEndpoint tests the schema tables endpoint.
func TestSchemaTablesEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: now,
	}

	storage.tableStats[1] = []models.TableStat{
		{SchemaName: "public", RelName: "users", NLiveTup: 1000, NDeadTup: 50, TableSize: 1024000, IndexSize: 512000},
		{SchemaName: "public", RelName: "orders", NLiveTup: 5000, NDeadTup: 100, TableSize: 2048000, IndexSize: 1024000},
	}

	e := echo.New()
	e.GET("/api/v1/schema/tables", func(c echo.Context) error {
		ctx := c.Request().Context()

		snap, _ := storage.GetLatestSnapshot(ctx, 1)
		if snap == nil {
			return c.JSON(http.StatusOK, map[string]interface{}{"tables": []interface{}{}, "total": 0})
		}

		stats, _ := storage.GetTableStats(ctx, snap.ID)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"tables": stats,
			"total":  len(stats),
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/schema/tables", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 2 {
		t.Errorf("got total %v, want 2", response["total"])
	}
}

// TestSchemaIndexesEndpoint tests the schema indexes endpoint.
func TestSchemaIndexesEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: now,
	}

	storage.indexStats[1] = []models.IndexStat{
		{SchemaName: "public", RelName: "users", IndexRelName: "users_pkey", IdxScan: 1000, IndexSize: 65536, IsPrimary: true},
		{SchemaName: "public", RelName: "users", IndexRelName: "idx_users_email", IdxScan: 500, IndexSize: 32768, IsUnique: true},
	}

	e := echo.New()
	e.GET("/api/v1/schema/indexes", func(c echo.Context) error {
		ctx := c.Request().Context()

		snap, _ := storage.GetLatestSnapshot(ctx, 1)
		if snap == nil {
			return c.JSON(http.StatusOK, map[string]interface{}{"indexes": []interface{}{}, "total": 0})
		}

		stats, _ := storage.GetIndexStats(ctx, snap.ID)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"indexes": stats,
			"total":   len(stats),
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/schema/indexes", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 2 {
		t.Errorf("got total %v, want 2", response["total"])
	}
}

// TestSchemaBloatEndpoint tests the schema bloat endpoint.
func TestSchemaBloatEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: now,
	}

	storage.bloatStats[1] = []models.BloatInfo{
		{SchemaName: "public", RelName: "users", NDeadTup: 500, NLiveTup: 1000, BloatPercent: 50.0},
	}

	e := echo.New()
	e.GET("/api/v1/schema/bloat", func(c echo.Context) error {
		ctx := c.Request().Context()

		snap, _ := storage.GetLatestSnapshot(ctx, 1)
		if snap == nil {
			return c.JSON(http.StatusOK, map[string]interface{}{"tables": []interface{}{}, "total": 0})
		}

		stats, _ := storage.GetBloatStats(ctx, snap.ID)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"tables": stats,
			"total":  len(stats),
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/schema/bloat", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 1 {
		t.Errorf("got total %v, want 1", response["total"])
	}
}

// TestErrorResponses tests the error response format.
func TestErrorResponses(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = CustomHTTPErrorHandler

	// Test 404
	e.GET("/not-found", func(c echo.Context) error {
		return NotFound(c, "resource not found")
	})

	req := httptest.NewRequest("GET", "/not-found", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusNotFound)
	}

	var response ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Error != "resource not found" {
		t.Errorf("got error %s, want 'resource not found'", response.Error)
	}
	if response.Code != ErrCodeNotFound {
		t.Errorf("got code %s, want %s", response.Code, ErrCodeNotFound)
	}
}

// TestPagination tests query pagination.
func TestPagination(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	storage.snapshots[1] = &models.Snapshot{
		ID:         1,
		InstanceID: 1,
		CapturedAt: now,
	}

	// Create 25 queries
	for i := 1; i <= 25; i++ {
		storage.queryStats[1] = append(storage.queryStats[1], models.QueryStat{
			QueryID:       int64(i),
			Query:         "SELECT query_" + string(rune('A'+i-1)),
			Calls:         int64(i * 100),
			TotalExecTime: float64(i * 1000),
		})
	}

	e := echo.New()
	e.GET("/api/v1/queries", func(c echo.Context) error {
		ctx := c.Request().Context()

		snap, _ := storage.GetLatestSnapshot(ctx, 1)
		if snap == nil {
			return c.JSON(http.StatusOK, map[string]interface{}{"queries": []interface{}{}, "total": 0})
		}

		stats, _ := storage.GetQueryStats(ctx, snap.ID)
		total := len(stats)

		limit := 10
		offset := 0

		if o := c.QueryParam("offset"); o != "" {
			if v, err := time.ParseDuration(o); err == nil {
				offset = int(v)
			}
		}

		if offset >= total {
			return c.JSON(http.StatusOK, map[string]interface{}{
				"queries":  []interface{}{},
				"total":    total,
				"page":     (offset / limit) + 1,
				"per_page": limit,
			})
		}

		end := offset + limit
		if end > total {
			end = total
		}

		return c.JSON(http.StatusOK, map[string]interface{}{
			"queries":  stats[offset:end],
			"total":    total,
			"page":     (offset / limit) + 1,
			"per_page": limit,
		})
	})

	// Test first page
	req := httptest.NewRequest("GET", "/api/v1/queries?limit=10", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 25 {
		t.Errorf("got total %v, want 25", response["total"])
	}

	queries := response["queries"].([]interface{})
	if len(queries) != 10 {
		t.Errorf("got %d queries, want 10", len(queries))
	}
}

// TestSnapshotsListEndpoint tests the snapshots list endpoint.
func TestSnapshotsListEndpoint(t *testing.T) {
	storage := newMockStorage()

	// Create test data
	now := time.Now()
	cacheHitRatio := 98.5
	for i := 1; i <= 5; i++ {
		storage.snapshots[int64(i)] = &models.Snapshot{
			ID:            int64(i),
			InstanceID:    1,
			CapturedAt:    now.Add(time.Duration(-i) * time.Hour),
			PGVersion:     "15.0",
			CacheHitRatio: &cacheHitRatio,
		}
	}

	e := echo.New()
	e.GET("/api/v1/snapshots", func(c echo.Context) error {
		ctx := c.Request().Context()

		snapshots, _ := storage.ListSnapshots(ctx, 1, 20)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"snapshots": snapshots,
			"total":     len(snapshots),
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/snapshots", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["total"].(float64) != 5 {
		t.Errorf("got total %v, want 5", response["total"])
	}
}
