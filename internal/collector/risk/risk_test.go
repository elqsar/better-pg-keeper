package risk_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/collector/risk"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

// fakeClient implements only GetOutageRisk; any other call panics.
type fakeClient struct {
	postgres.Client
	risk *models.OutageRisk
	err  error
}

func (f *fakeClient) GetOutageRisk(ctx context.Context) (*models.OutageRisk, error) {
	return f.risk, f.err
}

func TestCollectSavesRiskAndSize(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, logs := collectortest.Logger()
	c := risk.NewCollector(risk.Config{
		PGClient: &fakeClient{risk: &models.OutageRisk{
			Database:    "app",
			ClusterSize: 5 << 30,
			Unavailable: map[string]string{"sequences": "permission denied for sequence s"},
		}},
		Storage: storage, InstanceID: instance, Logger: logger,
	})

	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	got, err := storage.GetOutageRisk(ctx, snap)
	if err != nil || got == nil {
		t.Fatalf("GetOutageRisk = %v, %v", got, err)
	}
	if got.Database != "app" || got.Unavailable["sequences"] == "" {
		t.Errorf("saved risk = %+v", got)
	}
	if !strings.Contains(logs.String(), "outage-risk check sequences unavailable") {
		t.Errorf("failed check not logged; logs:\n%s", logs)
	}

	sizes, err := storage.GetSizeHistory(ctx, instance, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 1 || sizes[0].ClusterBytes != 5<<30 {
		t.Errorf("size history = %+v, want one sample of 5GiB", sizes)
	}
}

func TestCollectSkipsSizeWhenSizeCheckFailed(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	c := risk.NewCollector(risk.Config{
		PGClient: &fakeClient{risk: &models.OutageRisk{
			Unavailable: map[string]string{"size": "permission denied"},
		}},
		Storage: storage, InstanceID: instance, Logger: logger,
	})

	ctx := context.Background()
	if err := c.Collect(ctx, collectortest.Snapshot(t, storage, instance)); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	sizes, err := storage.GetSizeHistory(ctx, instance, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 0 {
		t.Errorf("size history = %+v, want none after a failed size check", sizes)
	}
}

func TestCollectErrors(t *testing.T) {
	tests := map[string]*fakeClient{
		"client error": {err: errors.New("connection refused")},
		"no data":      {},
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			storage, instance := collectortest.NewStorage(t)
			logger, _ := collectortest.Logger()
			c := risk.NewCollector(risk.Config{PGClient: client, Storage: storage, InstanceID: instance, Logger: logger})
			if err := c.Collect(context.Background(), collectortest.Snapshot(t, storage, instance)); err == nil {
				t.Error("Collect succeeded, want an error")
			}
		})
	}
}
