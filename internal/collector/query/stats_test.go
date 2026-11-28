package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/collector/query"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

type fakeClient struct {
	postgres.Client
	resetTime *time.Time
	resetErr  error
	stats     []models.QueryStat
	statsErr  error
}

func (f *fakeClient) GetStatsResetTime(ctx context.Context) (*time.Time, error) {
	return f.resetTime, f.resetErr
}

func (f *fakeClient) GetStatStatements(ctx context.Context) ([]models.QueryStat, error) {
	return f.stats, f.statsErr
}

func TestStatsCollector_CollectAndDetectReset(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, logs := collectortest.Logger()

	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	client := &fakeClient{
		resetTime: &t1,
		stats: []models.QueryStat{
			{QueryID: 101, Query: "SELECT $1", Calls: 50, TotalExecTime: 250, MeanExecTime: 5, Rows: 50},
		},
	}

	c := query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   client,
		Storage:    storage,
		InstanceID: instance,
		Logger:     logger,
	})
	if c.Name() != query.QueryStatsCollectorName {
		t.Fatalf("Name() = %q, want %q", c.Name(), query.QueryStatsCollectorName)
	}
	if c.Interval() != query.DefaultQueryStatsInterval {
		t.Fatalf("Interval() = %v, want %v", c.Interval(), query.DefaultQueryStatsInterval)
	}

	ctx := context.Background()
	snap1 := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap1); err != nil {
		t.Fatalf("Collect 1: %v", err)
	}

	gotHist, err := storage.GetQueryStats(ctx, snap1)
	if err != nil || len(gotHist) != 1 || gotHist[0].QueryID != 101 {
		t.Fatalf("GetQueryStats = %+v, %v", gotHist, err)
	}
	gotCur, err := storage.GetCurrentQueryStats(ctx, instance)
	if err != nil || len(gotCur) != 1 || gotCur[0].Calls != 50 {
		t.Fatalf("GetCurrentQueryStats = %+v, %v", gotCur, err)
	}

	// Simulate stats reset on second collection.
	client.resetTime = &t2
	client.stats[0].Calls = 5
	snap2 := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap2); err != nil {
		t.Fatalf("Collect 2: %v", err)
	}
	if !strings.Contains(logs.String(), "pg_stat_statements was reset") {
		t.Errorf("expected stats reset warning in logs, got:\n%s", logs.String())
	}
}

func TestStatsCollector_Errors(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	client := &fakeClient{
		resetErr: errors.New("reset check failed"),
		statsErr: errors.New("pg_stat_statements missing"),
	}
	c := query.NewStatsCollector(query.StatsCollectorConfig{
		PGClient:   client,
		Storage:    storage,
		InstanceID: instance,
		Logger:     logger,
	})
	if err := c.Collect(context.Background(), collectortest.Snapshot(t, storage, instance)); err == nil {
		t.Fatal("expected error when GetStatStatements fails")
	}
}
