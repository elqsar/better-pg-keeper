package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

type riskStore struct {
	risk  *models.OutageRisk
	sizes []models.SizeSample
}

func (r riskStore) GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error) {
	if r.risk == nil || collector != "outage_risk" {
		return nil, nil
	}
	return &models.Snapshot{ID: 9, CapturedAt: time.Now()}, nil
}

func (r riskStore) GetOutageRisk(ctx context.Context, snapshotID int64) (*models.OutageRisk, error) {
	return r.risk, nil
}

func (r riskStore) GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error) {
	return r.sizes, nil
}

func TestDashboardRiskPanel(t *testing.T) {
	e := setupTestEcho()
	now := time.Now()
	retained := int64(3 << 30)
	store := riskStore{
		risk: &models.OutageRisk{
			Databases: []models.DatabaseAge{{Name: "app", XIDAge: 1 << 30, MXIDAge: 10}, {Name: "other", XIDAge: 1000}},
			Slots:     []models.ReplicationSlot{{Name: "s", Active: false, RetainedBytes: &retained}},
			Sequences: []models.SequenceUsage{
				{SchemaName: "public", SequenceName: "small_seq", UsedFraction: 0.1},
				{SchemaName: "public", SequenceName: "orders_id_seq", UsedFraction: 0.93},
			},
			SequencesUnreadable: 2,
			ClusterSize:         100 << 30,
			Unavailable:         map[string]string{"replication": "permission denied"},
		},
		// 1GB/day over 3 days.
		sizes: []models.SizeSample{
			{CapturedAt: now.Add(-72 * time.Hour), ClusterBytes: 97 << 30},
			{CapturedAt: now, ClusterBytes: 100 << 30},
		},
	}
	storage := &mockPageStorage{
		snapshot:    &models.Snapshot{ID: 1, CapturedAt: now},
		suggestions: []models.Suggestion{{ID: 1, RuleID: "sequence_exhaustion", Severity: "critical", Title: "seq", TargetObject: "sequence:public.orders_id_seq", Status: models.StatusActive}},
	}
	h := NewPageHandler(storage, 1, "1.0.0").WithOutageRisk(OutageRiskConfig{Storage: store, DiskCapacityBytes: 130 << 30})

	rec := httptest.NewRecorder()
	if err := h.Dashboard(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"50% of the limit (oldest: app)",
		"1 slot(s), 1 inactive, 3.0 GB WAL retained",
		"public.orders_id_seq</span> at 93%",
		"2 not readable by the monitoring role",
		"100.0 GB of 130.0 GB · +1.0 GB/day · full in ~30 days",
		"1 active",
		"Checks that could not run: replication",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel missing %q", want)
		}
	}

	// No outage-risk data yet: no panel.
	h = NewPageHandler(storage, 1, "1.0.0").WithOutageRisk(OutageRiskConfig{Storage: riskStore{}})
	rec = httptest.NewRecorder()
	if err := h.Dashboard(e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "Outage risk") {
		t.Error("panel shown without outage-risk data")
	}
}
