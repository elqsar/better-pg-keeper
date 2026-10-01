package settings_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/collector/settings"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

// fakeClient implements only GetServerSettings; any other call panics.
type fakeClient struct {
	postgres.Client
	settings *models.ServerSettings
	err      error
}

func (f *fakeClient) GetServerSettings(ctx context.Context) (*models.ServerSettings, error) {
	return f.settings, f.err
}

func TestCollectSavesSettings(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, logs := collectortest.Logger()
	c := settings.NewCollector(settings.Config{
		PGClient: &fakeClient{settings: &models.ServerSettings{
			Settings: map[string]models.Setting{
				"work_mem": {Name: "work_mem", Setting: "4096", Unit: "kB", Source: "default", Context: "user"},
			},
			Unavailable: map[string]string{"autovacuum_disabled": "permission denied for table t"},
		}},
		Storage: storage, InstanceID: instance, Logger: logger,
	})

	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	got, err := storage.GetServerSettings(ctx, snap)
	if err != nil || got == nil {
		t.Fatalf("GetServerSettings = %v, %v", got, err)
	}
	if s := got.Settings["work_mem"]; s.Setting != "4096" || s.Unit != "kB" {
		t.Errorf("work_mem = %+v", s)
	}
	if got.Unavailable["autovacuum_disabled"] == "" {
		t.Errorf("unavailable checks not saved: %+v", got.Unavailable)
	}
	if !strings.Contains(logs.String(), "settings check autovacuum_disabled unavailable") {
		t.Errorf("failed check not logged; logs:\n%s", logs)
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
			c := settings.NewCollector(settings.Config{PGClient: client, Storage: storage, InstanceID: instance, Logger: logger})
			if err := c.Collect(context.Background(), collectortest.Snapshot(t, storage, instance)); err == nil {
				t.Error("Collect succeeded, want an error")
			}
		})
	}
}
