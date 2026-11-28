package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/setup"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
	"github.com/elqsar/pganalyzer/internal/verify"
)

type fakePGSetupChecker struct {
	calls  int
	report *models.SetupReport
}

func (f *fakePGSetupChecker) CheckSetup(ctx context.Context) (*models.SetupReport, error) {
	f.calls++
	cp := *f.report
	cp.CheckedAt = time.Now().UTC()
	return &cp, nil
}

func setupSQLiteTestHandlerEnv(t *testing.T) (*sqlite.SQLiteStorage, int64) {
	t.Helper()
	store, err := sqlite.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	instID, err := store.GetOrCreateInstance(context.Background(), &models.Instance{
		Name:     "localhost:5432/test",
		Host:     "localhost",
		Port:     5432,
		Database: "test",
	})
	if err != nil {
		t.Fatalf("GetOrCreateInstance: %v", err)
	}
	return store, instID
}

func TestActivityAPIHandler(t *testing.T) {
	store, instID := setupSQLiteTestHandlerEnv(t)
	ctx := context.Background()
	h := NewActivityHandler(store, instID)
	e := echo.New()

	// Empty state returns empty arrays, not null.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	rec := httptest.NewRecorder()
	if err := h.GetActivity(e.NewContext(req, rec)); err != nil {
		t.Fatalf("GetActivity empty: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `"blocked_queries":null`) ||
		strings.Contains(rec.Body.String(), `"long_running_queries":null`) ||
		strings.Contains(rec.Body.String(), `"idle_in_transaction":null`) {
		t.Fatalf("expected empty arrays instead of null: %s", rec.Body.String())
	}

	// Populated state.
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.SaveCurrentConnectionActivity(ctx, instID, &models.ConnectionActivity{
		ActiveCount: 2, IdleCount: 8, TotalConnections: 10, MaxConnections: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCurrentLongRunningQueries(ctx, instID, []models.LongRunningQuery{
		{PID: 101, Username: "app", DatabaseName: "test", Query: "SELECT pg_sleep(90)", State: "active", QueryStart: now, DurationSeconds: 90},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCurrentLockStats(ctx, instID, &models.LockStats{
		TotalLocks: 5, GrantedLocks: 4, WaitingLocks: 1,
	}); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	if err := h.GetActivity(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil), rec)); err != nil {
		t.Fatalf("GetActivity populated: %v", err)
	}
	var resp ActivityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Connections == nil || resp.Connections.TotalConnections != 10 {
		t.Errorf("unexpected connections: %+v", resp.Connections)
	}
	if len(resp.LongRunningQueries) != 1 || resp.LongRunningQueries[0].PID != 101 {
		t.Errorf("unexpected long running queries: %+v", resp.LongRunningQueries)
	}
	if resp.LockStats == nil || resp.LockStats.WaitingLocks != 1 {
		t.Errorf("unexpected lock stats: %+v", resp.LockStats)
	}
}

func TestRiskAPIHandler(t *testing.T) {
	store, instID := setupSQLiteTestHandlerEnv(t)
	ctx := context.Background()
	cfg := OutageRiskConfig{Storage: store, DiskCapacityBytes: 100 << 30}
	h := NewRiskHandler(store, cfg, instID)
	e := echo.New()

	// Before first snapshot -> 200 with null summary/outage_risk.
	rec := httptest.NewRecorder()
	if err := h.GetRisk(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/risk", nil), rec)); err != nil {
		t.Fatalf("GetRisk empty: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var emptyResp RiskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &emptyResp); err != nil {
		t.Fatal(err)
	}
	if emptyResp.Summary != nil || emptyResp.OutageRisk != nil {
		t.Fatalf("expected nil summary/outage_risk before first snapshot, got %+v", emptyResp)
	}

	// Save outage-risk snapshot + active risk suggestion.
	now := time.Now().UTC().Truncate(time.Second)
	snapID, err := store.CreateSnapshot(ctx, &models.Snapshot{
		InstanceID: instID, CapturedAt: now, PGVersion: "16.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCollectorRun(ctx, snapID, analyzer.DomainOutageRisk, models.CollectorStatusSuccess, now, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOutageRisk(ctx, snapID, &models.OutageRisk{
		Database: "test",
		Databases: []models.DatabaseAge{
			{Name: "test", XIDAge: 500_000_000, MXIDAge: 10_000},
		},
		ClusterSize: 20 << 30,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSuggestion(ctx, &models.Suggestion{
		InstanceID: instID, RuleID: "xid_wraparound", Severity: models.SeverityWarning,
		Title: "XID warning", Description: "desc", TargetObject: "db:test",
	}); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	if err := h.GetRisk(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/risk", nil), rec)); err != nil {
		t.Fatalf("GetRisk populated: %v", err)
	}
	var resp RiskResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Summary == nil || resp.OutageRisk == nil {
		t.Fatalf("expected summary and outage_risk, got %+v", resp)
	}
	if resp.Summary.XIDDatabase != "test" || resp.Summary.XIDIssues != 1 {
		t.Errorf("unexpected risk summary: %+v", resp.Summary)
	}
	if resp.OutageRisk.ClusterSize != 20<<30 {
		t.Errorf("ClusterSize = %d, want %d", resp.OutageRisk.ClusterSize, int64(20<<30))
	}
}

func TestSetupAPIHandler(t *testing.T) {
	e := echo.New()

	// Nil setup checker -> 503.
	nilHandler := NewSetupAPIHandler(nil)
	rec := httptest.NewRecorder()
	if err := nilHandler.GetSetup(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	pg := &fakePGSetupChecker{
		report: &models.SetupReport{
			Checks: []models.SetupCheck{
				{Name: "pg_stat_statements", Title: "pg_stat_statements", Status: models.SetupFail, Detail: "missing"},
			},
		},
	}
	checker := setup.New(setup.Options{PG: pg})
	h := NewSetupAPIHandler(checker)

	// Cached report
	rec = httptest.NewRecorder()
	if err := h.GetSetup(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var report models.SetupReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) == 0 || report.Checks[0].Status != models.SetupFail {
		t.Fatalf("unexpected setup report: %+v", report)
	}
	if pg.calls != 1 {
		t.Fatalf("expected 1 check call, got %d", pg.calls)
	}

	// Second call without refresh hits cache; ?refresh=1 forces re-check.
	rec = httptest.NewRecorder()
	_ = h.GetSetup(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil), rec))
	if pg.calls != 1 {
		t.Fatalf("expected cached check (1 call), got %d", pg.calls)
	}
	rec = httptest.NewRecorder()
	_ = h.GetSetup(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/setup?refresh=1", nil), rec))
	if pg.calls != 2 {
		t.Fatalf("expected refresh to trigger check (2 calls), got %d", pg.calls)
	}
}

func TestGetQueryAPIHandler(t *testing.T) {
	store, instID := setupSQLiteTestHandlerEnv(t)
	ctx := context.Background()
	h := NewQueriesHandler(store, nil, instID)
	e := echo.New()

	if err := store.SaveCurrentQueryStats(ctx, instID, []models.QueryStat{
		{QueryID: 4242, Query: "SELECT * FROM orders WHERE status = $1", Calls: 100, TotalExecTime: 500, MeanExecTime: 5, Rows: 200, SharedBlksHit: 95, SharedBlksRead: 5},
	}); err != nil {
		t.Fatal(err)
	}
	planJSON := `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders","Plan Rows":100,"Total Cost":50}}]`
	if _, err := store.SaveExplainPlan(ctx, &models.ExplainPlan{
		QueryID: 4242, PlanText: "Seq Scan on orders", PlanJSON: planJSON, CapturedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	// Valid query with cached plan and summary
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/queries/4242", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("4242")
	if err := h.GetQuery(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp QueryDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.QueryID != 4242 || resp.ExplainPlan == nil || len(resp.ExplainPlan.Summary) == 0 {
		t.Fatalf("unexpected query detail response: %+v", resp)
	}

	// Missing query -> 404
	rec = httptest.NewRecorder()
	c = e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/queries/9999", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("9999")
	_ = h.GetQuery(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSuggestionsDetailDismissAndUndismissAPI(t *testing.T) {
	store, instID := setupSQLiteTestHandlerEnv(t)
	otherInstID, err := store.GetOrCreateInstance(context.Background(), &models.Instance{
		Name: "other:5432/db", Host: "other", Port: 5432, Database: "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := store.UpsertSuggestion(ctx, &models.Suggestion{
		InstanceID: instID, RuleID: "slow_query", Severity: models.SeverityWarning,
		Title: "Slow query", Description: "Takes too long", TargetObject: "query:42",
		Metadata: `{"queryid":42}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSuggestion(ctx, &models.Suggestion{
		InstanceID: otherInstID, RuleID: "slow_query", Severity: models.SeverityWarning,
		Title: "Other instance", Description: "Other", TargetObject: "query:99",
	}); err != nil {
		t.Fatal(err)
	}

	active, _ := store.GetSuggestionsByStatus(ctx, instID, models.StatusActive)
	otherActive, _ := store.GetSuggestionsByStatus(ctx, otherInstID, models.StatusActive)
	sugID := active[0].ID
	otherSugID := otherActive[0].ID

	h := NewSuggestionsHandler(store, instID).WithVerifier(verify.New(store, time.Hour))
	e := echo.New()

	call := func(method, target string, id int64, fn func(echo.Context) error) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(method, target, nil), rec)
		c.SetParamNames("id")
		c.SetParamValues(strconvFormatInt(id))
		if err := fn(c); err != nil {
			t.Fatalf("%s %s error: %v", method, target, err)
		}
		return rec
	}

	// Cross-instance access returns 404.
	if rec := call(http.MethodGet, "/api/v1/suggestions/other", otherSugID, h.GetSuggestion); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-instance GET status = %d, want 404", rec.Code)
	}

	// GET active suggestion includes status.
	rec := call(http.MethodGet, "/api/v1/suggestions/id", sugID, h.GetSuggestion)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET suggestion status = %d, want 200", rec.Code)
	}
	var detail SuggestionDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Status != models.StatusActive {
		t.Errorf("detail.Status = %q, want active", detail.Status)
	}

	// Undismiss while active -> 409.
	if rec := call(http.MethodPost, "/api/v1/suggestions/id/undismiss", sugID, h.UndismissSuggestion); rec.Code != http.StatusConflict {
		t.Fatalf("undismiss active status = %d, want 409", rec.Code)
	}

	// Dismiss active -> 200.
	if rec := call(http.MethodPost, "/api/v1/suggestions/id/dismiss", sugID, h.DismissSuggestion); rec.Code != http.StatusOK {
		t.Fatalf("dismiss active status = %d, want 200", rec.Code)
	}

	// Undismiss dismissed -> 200 and status back to active.
	rec = call(http.MethodPost, "/api/v1/suggestions/id/undismiss", sugID, h.UndismissSuggestion)
	if rec.Code != http.StatusOK {
		t.Fatalf("undismiss dismissed status = %d, want 200", rec.Code)
	}
	got, _ := store.GetSuggestionByID(ctx, sugID)
	if got.Status != models.StatusActive || got.DismissedAt != nil {
		t.Fatalf("expected active with nil dismissed_at, got %+v", got)
	}

	// Resolve suggestion, then verify dismissing resolved returns 409 and GET includes verification.
	if err := store.ResolveSuggestion(ctx, sugID); err != nil {
		t.Fatal(err)
	}
	if rec := call(http.MethodPost, "/api/v1/suggestions/id/dismiss", sugID, h.DismissSuggestion); rec.Code != http.StatusConflict {
		t.Fatalf("dismiss resolved status = %d, want 409", rec.Code)
	}
	rec = call(http.MethodGet, "/api/v1/suggestions/id", sugID, h.GetSuggestion)
	var resolvedDetail SuggestionDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resolvedDetail); err != nil {
		t.Fatal(err)
	}
	if resolvedDetail.Status != models.StatusResolved || resolvedDetail.Verification == nil {
		t.Fatalf("expected resolved status and verification payload, got %+v", resolvedDetail)
	}
}

func TestP2WebUIPages(t *testing.T) {
	e := setupTestEcho()
	now := time.Now().UTC()
	resolvedAt := now.Add(-time.Hour)
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: now},
		queryStats: []models.QueryStat{
			{QueryID: 777, Query: "SELECT * FROM orders", Calls: 10, TotalExecTime: 200, MeanExecTime: 20},
		},
		suggestions: []models.Suggestion{
			{ID: 1, InstanceID: 1, RuleID: "slow_query", Severity: "warning", Status: models.StatusActive, Title: "Active slow query", TargetObject: "query:777", FirstSeenAt: now, LastSeenAt: now},
			{ID: 2, InstanceID: 1, RuleID: "unused_index", Severity: "info", Status: models.StatusDismissed, Title: "Dismissed idx", TargetObject: "idx_old", FirstSeenAt: now, LastSeenAt: now},
			{ID: 3, InstanceID: 1, RuleID: "slow_query", Severity: "warning", Status: models.StatusResolved, Title: "Resolved slow query", TargetObject: "query:888", FirstSeenAt: now, LastSeenAt: now, ResolvedAt: &resolvedAt},
		},
	}

	pg := &fakePGSetupChecker{
		report: &models.SetupReport{
			Checks: []models.SetupCheck{
				{Name: "pg_stat_statements", Title: "pg_stat_statements", Status: models.SetupFail, Detail: "missing"},
			},
		},
	}
	handler := NewPageHandler(storage, 1, "1.0.0").WithSetup(setup.New(setup.Options{PG: pg}))

	// Dashboard has clickable Top Query and Recent Suggestion links + Setup nav link.
	rec := httptest.NewRecorder()
	if err := handler.Dashboard(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)); err != nil {
		t.Fatal(err)
	}
	dashBody := rec.Body.String()
	for _, want := range []string{`href="/queries/777"`, `href="/suggestions/1"`, `href="/setup"`} {
		if !strings.Contains(dashBody, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}

	// Suggestions page supports status=resolved with badge-success and status=dismissed with Restore button.
	rec = httptest.NewRecorder()
	if err := handler.Suggestions(e.NewContext(httptest.NewRequest(http.MethodGet, "/suggestions?status=resolved", nil), rec)); err != nil {
		t.Fatal(err)
	}
	resBody := rec.Body.String()
	if !strings.Contains(resBody, `value="resolved" selected`) || !strings.Contains(resBody, `<span class="badge-success">Resolved</span>`) {
		t.Errorf("suggestions resolved view missing expected controls/badge")
	}

	rec = httptest.NewRecorder()
	if err := handler.Suggestions(e.NewContext(httptest.NewRequest(http.MethodGet, "/suggestions?status=dismissed", nil), rec)); err != nil {
		t.Fatal(err)
	}
	dismissedBody := rec.Body.String()
	if !strings.Contains(dismissedBody, "undismissSuggestion(") || !strings.Contains(dismissedBody, "Restore") {
		t.Errorf("suggestions dismissed view missing Restore button")
	}

	// Setup page renders failed check with badge-critical and active Setup nav link.
	rec = httptest.NewRecorder()
	if err := handler.Setup(e.NewContext(httptest.NewRequest(http.MethodGet, "/setup", nil), rec)); err != nil {
		t.Fatal(err)
	}
	setupBody := rec.Body.String()
	if !strings.Contains(setupBody, `<span class="badge-critical">Failed</span>`) {
		t.Errorf("setup page expected badge-critical for failed check")
	}
	if !strings.Contains(setupBody, `<a href="/setup" class="nav-link-active">Setup</a>`) {
		t.Errorf("setup page expected active Setup nav link")
	}
}

func strconvFormatInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
