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
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

func TestQueryHistoryEndpoint(t *testing.T) {
	storage, err := sqlite.NewStorage(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	ctx := context.Background()
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "api-history", Host: "localhost", Port: 5432, Database: "test"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now(), PGVersion: "16"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveQueryStats(ctx, snapshotID, []models.QueryStat{{QueryID: 42, Query: "SELECT 1", Calls: 7}}); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.GET("/api/v1/queries/:id/history", NewQueriesHandler(storage, nil, instanceID).GetQueryHistory)
	for _, test := range []struct {
		path string
		want int
	}{
		{"/api/v1/queries/42/history?limit=1", http.StatusOK},
		{"/api/v1/queries/invalid/history", http.StatusBadRequest},
		{"/api/v1/queries/42/history?limit=0", http.StatusBadRequest},
		{"/api/v1/queries/42/history?from=bad", http.StatusBadRequest},
		{"/api/v1/queries/42/history?offset=-1", http.StatusBadRequest},
	} {
		response := httptest.NewRecorder()
		e.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.want {
			t.Errorf("GET %s: got %d, want %d: %s", test.path, response.Code, test.want, response.Body.String())
		}
		if test.want == http.StatusOK {
			var body QueryHistoryResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Samples) != 1 || body.Samples[0].Calls != 7 {
				t.Errorf("GET %s: unexpected history %s: %v", test.path, response.Body.String(), err)
			}
		}
	}
}
