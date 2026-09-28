package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

type fakePG struct {
	report *models.SetupReport
	err    error
	calls  int
}

func (f *fakePG) CheckSetup(ctx context.Context) (*models.SetupReport, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	// A fresh copy each time, as the real client returns.
	r := *f.report
	r.Checks = append([]models.SetupCheck(nil), f.report.Checks...)
	return &r, nil
}

type fakeStorage struct {
	firstQuery *models.Snapshot
	sizes      []models.SizeSample
}

func (f fakeStorage) GetEarliestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notBefore time.Time) (*models.Snapshot, error) {
	if collector != "query_stats" {
		return nil, nil
	}
	return f.firstQuery, nil
}

func (f fakeStorage) GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error) {
	return f.sizes, nil
}

func statuses(r *models.SetupReport) map[string]string {
	out := map[string]string{}
	for _, c := range r.Checks {
		out[c.Name] = c.Status
	}
	return out
}

func TestReadiness(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	since := now.Add(-10 * 24 * time.Hour)
	pg := &fakePG{report: &models.SetupReport{
		Checks:     []models.SetupCheck{{Name: "pg_stat_statements", Status: models.SetupOK}, {Name: "hypopg", Status: models.SetupInfo}},
		StatsSince: &since,
	}}

	fresh := New(Options{PG: pg, Storage: fakeStorage{
		firstQuery: &models.Snapshot{CapturedAt: now.Add(-5 * time.Hour)},
		sizes:      []models.SizeSample{{CapturedAt: now.Add(-2 * time.Hour)}, {CapturedAt: now}},
	}, SlowQueryWindow: 24 * time.Hour, UnusedIndexDays: 30, Now: func() time.Time { return now }}).Run(context.Background())

	got := statuses(fresh)
	want := map[string]string{
		"pg_stat_statements": models.SetupOK,
		"query_history":      models.SetupPending,
		"index_evidence":     models.SetupPending,
		"size_history":       models.SetupPending,
	}
	for name, status := range want {
		if got[name] != status {
			t.Errorf("%s = %q, want %q", name, got[name], status)
		}
	}
	if _, ok := got["hypopg"]; ok {
		t.Error("hypopg should be left out when the index advisor is disabled")
	}
	if fresh.NeedsAttention() {
		t.Error("pending history must not need attention")
	}
	for _, c := range fresh.Checks {
		switch c.Name {
		case "query_history":
			if !strings.Contains(c.Detail, "ready in ~19 hours") {
				t.Errorf("query history detail = %q", c.Detail)
			}
		case "index_evidence":
			if !strings.Contains(c.Detail, "ready in ~20 days") {
				t.Errorf("index evidence detail = %q", c.Detail)
			}
		}
	}

	// Enough history everywhere.
	old := New(Options{PG: pg, Storage: fakeStorage{
		firstQuery: &models.Snapshot{CapturedAt: now.Add(-48 * time.Hour)},
		sizes:      []models.SizeSample{{CapturedAt: now.Add(-30 * time.Hour)}, {CapturedAt: now}},
	}, SlowQueryWindow: 24 * time.Hour, UnusedIndexDays: 7, IndexAdvisor: true, Now: func() time.Time { return now }}).Run(context.Background())
	for name, status := range statuses(old) {
		if name != "hypopg" && status != models.SetupOK {
			t.Errorf("%s = %q with enough history, want ok", name, status)
		}
	}
}

func TestConnectionFailureAndCache(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	pg := &fakePG{err: errors.New("dial tcp: connection refused")}
	c := New(Options{PG: pg, Now: func() time.Time { return now }})

	r := c.Report(context.Background())
	if len(r.Checks) != 1 || r.Checks[0].Name != "connection" || r.Checks[0].Status != models.SetupFail || !r.NeedsAttention() {
		t.Fatalf("report = %+v", r.Checks)
	}
	c.Report(context.Background())
	if pg.calls != 1 {
		t.Errorf("checks ran %d times within the TTL, want 1", pg.calls)
	}
	now = now.Add(DefaultTTL + time.Second)
	c.Report(context.Background())
	if pg.calls != 2 {
		t.Errorf("checks ran %d times after the TTL, want 2", pg.calls)
	}
}

func TestFormatSpan(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Minute:    "1 hour",
		90 * time.Minute:    "2 hours",
		47 * time.Hour:      "47 hours",
		49 * time.Hour:      "3 days",
		20 * 24 * time.Hour: "20 days",
	} {
		if got := formatSpan(d); got != want {
			t.Errorf("formatSpan(%s) = %q, want %q", d, got, want)
		}
	}
}
