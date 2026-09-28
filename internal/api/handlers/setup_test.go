package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/setup"
)

type stubSetup struct{ checks []models.SetupCheck }

func (s stubSetup) CheckSetup(ctx context.Context) (*models.SetupReport, error) {
	return &models.SetupReport{Checks: append([]models.SetupCheck(nil), s.checks...)}, nil
}

func renderDashboard(t *testing.T, checker *setup.Checker) string {
	t.Helper()
	e := setupTestEcho()
	handler := NewPageHandler(&mockPageStorage{snapshot: &models.Snapshot{ID: 1, CapturedAt: time.Now()}}, 1, "1.0.0").WithSetup(checker)
	rec := httptest.NewRecorder()
	if err := handler.Dashboard(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)); err != nil {
		t.Fatal(err)
	}
	return rec.Body.String()
}

func TestDashboardSetupBanner(t *testing.T) {
	failing := setup.New(setup.Options{PG: stubSetup{checks: []models.SetupCheck{
		{Name: "pg_stat_statements", Title: "pg_stat_statements", Status: models.SetupFail, Detail: "The extension is not created", Fix: "CREATE EXTENSION pg_stat_statements;"},
		{Name: "privileges", Title: "Monitoring privileges", Status: models.SetupWarn, Detail: "lacks pg_monitor"},
	}}})
	body := renderDashboard(t, failing)
	for _, want := range []string{"1 setup check(s) failed", "pg_stat_statements: The extension is not created", `href="/setup"`} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}

	healthy := setup.New(setup.Options{PG: stubSetup{checks: []models.SetupCheck{{Name: "version", Title: "PostgreSQL version", Status: models.SetupOK}}}})
	if body := renderDashboard(t, healthy); strings.Contains(body, "See setup") {
		t.Error("no banner expected when every check passes")
	}

	pending := setup.New(setup.Options{PG: stubSetup{checks: []models.SetupCheck{{Name: "x", Title: "Slow-query history", Status: models.SetupPending, Detail: "ready in ~5 hours"}}}})
	if body := renderDashboard(t, pending); !strings.Contains(body, "Still collecting history.") || strings.Contains(body, "setup check(s) failed") {
		t.Error("pending history should show the informational banner only")
	}
}

func TestSetupPage(t *testing.T) {
	e := setupTestEcho()
	checker := setup.New(setup.Options{PG: stubSetup{checks: []models.SetupCheck{
		{Name: "privileges", Title: "Monitoring privileges", Status: models.SetupWarn, Detail: "lacks pg_monitor", Fix: `GRANT pg_monitor TO "app";`},
		{Name: "version", Title: "PostgreSQL version", Status: models.SetupOK, Detail: "PostgreSQL 17.2", Fix: "not shown"},
	}}})
	handler := NewPageHandler(&mockPageStorage{}, 1, "1.0.0").WithSetup(checker)
	rec := httptest.NewRecorder()
	if err := handler.Setup(e.NewContext(httptest.NewRequest(http.MethodGet, "/setup?refresh=1", nil), rec)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{"Monitoring privileges", "GRANT pg_monitor TO &#34;app&#34;;", "PostgreSQL 17.2", "Check again"} {
		if !strings.Contains(body, want) {
			t.Errorf("setup page missing %q", want)
		}
	}
	if strings.Contains(body, "not shown") {
		t.Error("fixes for passing checks should be hidden")
	}
}
