package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
)

type mockDashboardStorage struct {
	snapshot    *models.Snapshot
	queryStats  []models.QueryStat
	suggestions []models.Suggestion
	err         error
}

func (m *mockDashboardStorage) GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error) {
	return m.snapshot, m.err
}

func (m *mockDashboardStorage) GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error) {
	return m.queryStats, m.err
}

func (m *mockDashboardStorage) GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error) {
	var filtered []models.Suggestion
	for _, suggestion := range m.suggestions {
		if suggestion.Status == status {
			filtered = append(filtered, suggestion)
		}
	}
	return filtered, m.err
}

func TestDashboardHandlerSortsTopQueriesByTotalExecTime(t *testing.T) {
	e := echo.New()
	storage := &mockDashboardStorage{
		snapshot: &models.Snapshot{
			ID:         42,
			CapturedAt: time.Now(),
		},
		queryStats: []models.QueryStat{
			{QueryID: 1, Query: "SELECT 1", TotalExecTime: 500, MeanExecTime: 50, Calls: 10},
			{QueryID: 2, Query: "SELECT 2", TotalExecTime: 3000, MeanExecTime: 1500, Calls: 2},
			{QueryID: 3, Query: "SELECT 3", TotalExecTime: 200, MeanExecTime: 20, Calls: 10},
			{QueryID: 4, Query: "SELECT 4", TotalExecTime: 1800, MeanExecTime: 180, Calls: 10},
			{QueryID: 5, Query: "SELECT 5", TotalExecTime: 900, MeanExecTime: 90, Calls: 10},
			{QueryID: 6, Query: "SELECT 6", TotalExecTime: 1200, MeanExecTime: 120, Calls: 10},
		},
	}

	handler := NewDashboardHandler(storage, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := handler.GetDashboard(c); err != nil {
		t.Fatalf("GetDashboard() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("GetDashboard() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var response DashboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got, want := len(response.TopQueries), 5; got != want {
		t.Fatalf("len(TopQueries) = %d, want %d", got, want)
	}

	wantIDs := []int64{2, 4, 6, 5, 1}
	for i, wantID := range wantIDs {
		if got := response.TopQueries[i].QueryID; got != wantID {
			t.Fatalf("TopQueries[%d].QueryID = %d, want %d", i, got, wantID)
		}
	}

	if got, want := response.SlowQueriesCount, 1; got != want {
		t.Fatalf("SlowQueriesCount = %d, want %d", got, want)
	}
}
