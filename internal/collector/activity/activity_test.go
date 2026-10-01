package activity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector/activity"
	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

// fakeClient implements only the activity queries and records the thresholds
// it was asked for; any other call panics.
type fakeClient struct {
	postgres.Client
	activity    *models.ConnectionActivity
	longRunning []models.LongRunningQuery
	idleInTx    []models.IdleInTransaction
	activityErr error
	longErr     error
	idleErr     error

	longThreshold, idleThreshold float64
}

func (f *fakeClient) GetConnectionActivity(ctx context.Context) (*models.ConnectionActivity, error) {
	return f.activity, f.activityErr
}

func (f *fakeClient) GetLongRunningQueries(ctx context.Context, threshold float64) ([]models.LongRunningQuery, error) {
	f.longThreshold = threshold
	return f.longRunning, f.longErr
}

func (f *fakeClient) GetIdleInTransaction(ctx context.Context, threshold float64) ([]models.IdleInTransaction, error) {
	f.idleThreshold = threshold
	return f.idleInTx, f.idleErr
}

func TestCollectSavesActivity(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	now := time.Now()
	client := &fakeClient{
		activity: &models.ConnectionActivity{ActiveCount: 3, IdleCount: 40, IdleInTxCount: 1, TotalConnections: 44, MaxConnections: 100},
		longRunning: []models.LongRunningQuery{
			{PID: 10, Username: "app", DatabaseName: "app", Query: "SELECT pg_sleep(600)", State: "active", QueryStart: now.Add(-10 * time.Minute), DurationSeconds: 600},
		},
		idleInTx: []models.IdleInTransaction{
			{PID: 11, Username: "app", DatabaseName: "app", State: "idle in transaction", XactStart: now.Add(-5 * time.Minute), DurationSeconds: 300, Query: "BEGIN"},
		},
	}
	c := activity.NewActivityCollector(activity.ActivityCollectorConfig{
		PGClient: client, Storage: storage, InstanceID: instance, Logger: logger,
		LongRunningThreshold: 120,
	})

	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if client.longThreshold != 120 || client.idleThreshold != activity.DefaultIdleInTxThreshold {
		t.Errorf("thresholds = %v, %v; want the configured 120 and the default %v",
			client.longThreshold, client.idleThreshold, activity.DefaultIdleInTxThreshold)
	}

	act, err := storage.GetConnectionActivity(ctx, snap)
	if err != nil || act == nil || act.IdleCount != 40 || act.MaxConnections != 100 {
		t.Errorf("GetConnectionActivity = %+v, %v", act, err)
	}
	current, err := storage.GetCurrentConnectionActivity(ctx, instance)
	if err != nil || current == nil || current.TotalConnections != 44 {
		t.Errorf("GetCurrentConnectionActivity = %+v, %v", current, err)
	}
	long, err := storage.GetLongRunningQueries(ctx, snap)
	if err != nil || len(long) != 1 || long[0].PID != 10 {
		t.Errorf("GetLongRunningQueries = %+v, %v", long, err)
	}
	idle, err := storage.GetIdleInTransaction(ctx, snap)
	if err != nil || len(idle) != 1 || idle[0].PID != 11 {
		t.Errorf("GetIdleInTransaction = %+v, %v", idle, err)
	}
	currentIdle, err := storage.GetCurrentIdleInTransaction(ctx, instance)
	if err != nil || len(currentIdle) != 1 {
		t.Errorf("GetCurrentIdleInTransaction = %+v, %v", currentIdle, err)
	}
}

func TestCollectErrors(t *testing.T) {
	act := &models.ConnectionActivity{}
	tests := map[string]*fakeClient{
		"activity error":     {activityErr: errors.New("connection refused")},
		"no activity":        {},
		"long-running error": {activity: act, longErr: errors.New("statement timeout")},
		"idle-in-tx error":   {activity: act, idleErr: errors.New("statement timeout")},
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			storage, instance := collectortest.NewStorage(t)
			logger, _ := collectortest.Logger()
			c := activity.NewActivityCollector(activity.ActivityCollectorConfig{PGClient: client, Storage: storage, InstanceID: instance, Logger: logger})
			if err := c.Collect(context.Background(), collectortest.Snapshot(t, storage, instance)); err == nil {
				t.Error("Collect succeeded, want an error")
			}
		})
	}
}
