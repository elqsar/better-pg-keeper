package locks_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/collector/locks"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

// fakeClient implements only the lock queries; any other call panics.
type fakeClient struct {
	postgres.Client
	stats      *models.LockStats
	blocked    []models.BlockedQuery
	statsErr   error
	blockedErr error
}

func (f *fakeClient) GetLockStats(ctx context.Context) (*models.LockStats, error) {
	return f.stats, f.statsErr
}

func (f *fakeClient) GetBlockedQueries(ctx context.Context) ([]models.BlockedQuery, error) {
	return f.blocked, f.blockedErr
}

func TestCollectSavesLocks(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	relation := "public.orders"
	c := locks.NewLocksCollector(locks.LocksCollectorConfig{
		PGClient: &fakeClient{
			stats: &models.LockStats{TotalLocks: 12, GrantedLocks: 11, WaitingLocks: 1},
			blocked: []models.BlockedQuery{{
				BlockedPID: 101, BlockedQuery: "UPDATE orders SET note = 'x'", BlockedStart: time.Now().Add(-time.Minute),
				WaitDuration: 60, BlockingPID: 100, BlockingQuery: "ALTER TABLE orders ADD c int",
				LockType: "relation", LockMode: "RowExclusiveLock", Relation: &relation,
			}},
		},
		Storage: storage, InstanceID: instance, Logger: logger,
	})

	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	stats, err := storage.GetLockStats(ctx, snap)
	if err != nil || stats == nil || stats.WaitingLocks != 1 || stats.TotalLocks != 12 {
		t.Errorf("GetLockStats = %+v, %v", stats, err)
	}
	current, err := storage.GetCurrentLockStats(ctx, instance)
	if err != nil || current == nil || current.WaitingLocks != 1 {
		t.Errorf("GetCurrentLockStats = %+v, %v", current, err)
	}
	blocked, err := storage.GetBlockedQueries(ctx, snap)
	if err != nil || len(blocked) != 1 || blocked[0].BlockingPID != 100 {
		t.Errorf("GetBlockedQueries = %+v, %v", blocked, err)
	}
	currentBlocked, err := storage.GetCurrentBlockedQueries(ctx, instance)
	if err != nil || len(currentBlocked) != 1 {
		t.Errorf("GetCurrentBlockedQueries = %+v, %v", currentBlocked, err)
	}
}

func TestCollectErrors(t *testing.T) {
	stats := &models.LockStats{}
	tests := map[string]*fakeClient{
		"lock stats error":      {statsErr: errors.New("connection refused")},
		"no lock stats":         {},
		"blocked queries error": {stats: stats, blockedErr: errors.New("canceling statement due to statement timeout")},
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			storage, instance := collectortest.NewStorage(t)
			logger, _ := collectortest.Logger()
			c := locks.NewLocksCollector(locks.LocksCollectorConfig{PGClient: client, Storage: storage, InstanceID: instance, Logger: logger})
			if err := c.Collect(context.Background(), collectortest.Snapshot(t, storage, instance)); err == nil {
				t.Error("Collect succeeded, want an error")
			}
		})
	}
}
