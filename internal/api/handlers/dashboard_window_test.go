package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// windowStorage serves two query_stats snapshots a day apart and their delta.
type windowStorage struct {
	now    time.Time
	deltas []models.QueryStatDelta
}

func (w windowStorage) GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error) {
	if notAfter.Before(w.now.Add(-time.Minute)) {
		return &models.Snapshot{ID: 1, CapturedAt: w.now.Add(-25 * time.Hour)}, nil
	}
	return &models.Snapshot{ID: 2, CapturedAt: w.now}, nil
}

func (w windowStorage) GetQueryStatsDelta(ctx context.Context, from, to int64) ([]models.QueryStatDelta, error) {
	return w.deltas, nil
}

func TestDashboardUsesQueryWindow(t *testing.T) {
	e := setupTestEcho()
	now := time.Now()
	// Lifetime: query 1 looks slow and busy; recently it is fast. Query 2 is
	// the recent regression.
	lifetime := []models.QueryStat{
		{QueryID: 1, Query: "SELECT old", Calls: 1000, TotalExecTime: 9_000_000, MeanExecTime: 9000},
		{QueryID: 2, Query: "SELECT new", Calls: 10, TotalExecTime: 100, MeanExecTime: 10},
	}
	window := QueryWindowConfig{
		Storage: windowStorage{now: now, deltas: []models.QueryStatDelta{
			{QueryID: 1, Query: "SELECT old", DeltaCalls: 100, DeltaTotalTime: 500, MeanExecTime: 5},
			{QueryID: 2, Query: "SELECT new", DeltaCalls: 50, DeltaTotalTime: 150_000, MeanExecTime: 3000},
		}},
		Window: 24 * time.Hour, SlowQueryMs: 2000,
	}

	render := func(h *PageHandler) string {
		rec := httptest.NewRecorder()
		if err := h.Dashboard(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)); err != nil {
			t.Fatal(err)
		}
		return rec.Body.String()
	}
	storage := &mockPageStorage{snapshot: &models.Snapshot{ID: 2, CapturedAt: now}, queryStats: lifetime}

	body := render(NewPageHandler(storage, 1, "1.0.0").WithQueryWindow(window))
	for _, want := range []string{"Ran in the last 25h", "Mean &ge; 2.0s over the last 25h", "last 25h</span>"} {
		if !strings.Contains(body, want) {
			t.Errorf("windowed dashboard missing %q", want)
		}
	}
	// The recent regression is the top query, and the only slow one.
	if i, j := strings.Index(body, "SELECT new"), strings.Index(body, "SELECT old"); i < 0 || j < 0 || i > j {
		t.Errorf("SELECT new should be listed before SELECT old (%d, %d)", i, j)
	}

	// Without enough history the lifetime figures are used and labelled so.
	body = render(NewPageHandler(storage, 1, "1.0.0"))
	if !strings.Contains(body, "Tracked since statistics reset") || !strings.Contains(body, "Mean &ge; 1.0s since statistics reset") {
		t.Error("lifetime dashboard should say its figures are since statistics reset")
	}
}

func TestDashboardAPIUsesQueryWindow(t *testing.T) {
	e := setupTestEcho()
	now := time.Now()
	storage := &mockDashboardStorage{snapshot: &models.Snapshot{ID: 2, CapturedAt: now}}
	h := NewDashboardHandler(storage, 1).WithQueryWindow(QueryWindowConfig{
		Storage: windowStorage{now: now, deltas: []models.QueryStatDelta{
			{QueryID: 2, Query: "SELECT new", DeltaCalls: 50, DeltaTotalTime: 150_000, MeanExecTime: 3000},
		}},
		Window: 24 * time.Hour, SlowQueryMs: 2000,
	})
	rec := httptest.NewRecorder()
	if err := h.GetDashboard(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var resp DashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.SlowQueriesCount != 1 || resp.SlowQueryMs != 2000 || resp.QueryWindowSeconds != 25*3600 || len(resp.TopQueries) != 1 {
		t.Errorf("response = %+v", resp)
	}
}
