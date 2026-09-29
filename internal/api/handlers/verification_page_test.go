package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/verify"
)

// flatHistory returns 5-minute cumulative samples for one query whose mean is
// 2000ms before cut and 20ms after it.
type flatHistory struct{ cut time.Time }

func (f flatHistory) GetQueryHistory(ctx context.Context, instanceID, queryID int64, from, to time.Time, limit, offset int) ([]models.QueryHistorySample, error) {
	var out []models.QueryHistorySample
	var calls int64
	var total float64
	for ts := from; !ts.After(to); ts = ts.Add(5 * time.Minute) {
		mean := 2000.0
		if ts.After(f.cut) {
			mean = 20
		}
		calls += 10
		total += 10 * mean
		s := models.QueryHistorySample{SampledAt: ts}
		s.QueryID, s.Query, s.Calls, s.TotalExecTime = queryID, "SELECT * FROM orders WHERE status = $1", calls, total
		out = append([]models.QueryHistorySample{s}, out...)
	}
	return out, nil
}

func TestSuggestionDetailPageVerification(t *testing.T) {
	e := setupTestEcho()
	now := time.Now().Truncate(time.Minute)
	resolved := now.Add(-26 * time.Hour)
	storage := &mockPageStorage{
		snapshot: &models.Snapshot{ID: 1, CapturedAt: now},
		suggestions: []models.Suggestion{{
			ID: 11, RuleID: "index_recommendation", Severity: "warning", Title: "Index on orders(status)",
			Description: "d", TargetObject: "index:public.orders(status)", Status: models.StatusResolved,
			FirstSeenAt: resolved.Add(-72 * time.Hour), LastSeenAt: resolved, ResolvedAt: &resolved,
			Metadata: `{"query_ids":[4242]}`,
		}},
	}
	handler := NewPageHandler(storage, 1, "1.0.0").WithVerifier(verify.New(flatHistory{cut: resolved}, 24*time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/suggestions/11", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("11")
	if err := handler.SuggestionDetail(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Fix verification", "Faster", "2.0s → 20ms (99% faster)", `href="/queries/4242"`, "Resolved"} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}

	// Without a verifier, or for an active suggestion, there is no card.
	storage.suggestions[0].Status = models.StatusActive
	rec = httptest.NewRecorder()
	c = e.NewContext(httptest.NewRequest(http.MethodGet, "/suggestions/11", nil), rec)
	c.SetParamNames("id")
	c.SetParamValues("11")
	if err := handler.SuggestionDetail(c); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "Fix verification") {
		t.Error("active suggestion should not show verification")
	}
}
