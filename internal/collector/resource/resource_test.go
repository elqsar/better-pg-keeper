package resource_test

import (
	"context"
	"errors"
	"testing"

	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/collector/resource"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

type fakeClient struct {
	postgres.Client
	tables      []models.TableStat
	tablesErr   error
	indexes     []models.IndexStat
	indexesErr  error
	dbStats     *models.DatabaseStats
	dbStatsErr  error
	extStats    *models.ExtendedDatabaseStats
	extStatsErr error
}

func (f *fakeClient) GetStatTables(ctx context.Context) ([]models.TableStat, error) {
	return f.tables, f.tablesErr
}

func (f *fakeClient) GetStatIndexes(ctx context.Context) ([]models.IndexStat, error) {
	return f.indexes, f.indexesErr
}

func (f *fakeClient) GetDatabaseStats(ctx context.Context) (*models.DatabaseStats, error) {
	return f.dbStats, f.dbStatsErr
}

func (f *fakeClient) GetExtendedDatabaseStats(ctx context.Context) (*models.ExtendedDatabaseStats, error) {
	return f.extStats, f.extStatsErr
}

func TestTableStatsCollector(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	client := &fakeClient{
		tables: []models.TableStat{
			{SchemaName: "public", RelName: "orders", SeqScan: 12, IdxScan: 340, NLiveTup: 5000, TableSize: 1024 * 1024},
		},
	}

	c := resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
		PGClient: client, Storage: storage, InstanceID: instance, Logger: logger,
	})
	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	hist, err := storage.GetTableStats(ctx, snap)
	if err != nil || len(hist) != 1 || hist[0].RelName != "orders" {
		t.Fatalf("GetTableStats = %+v, %v", hist, err)
	}
	cur, err := storage.GetCurrentTableStats(ctx, instance)
	if err != nil || len(cur) != 1 || cur[0].NLiveTup != 5000 {
		t.Fatalf("GetCurrentTableStats = %+v, %v", cur, err)
	}

	client.tablesErr = errors.New("permission denied")
	if err := c.Collect(ctx, snap); err == nil {
		t.Fatal("expected error when GetStatTables fails")
	}
}

func TestIndexStatsCollector(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	client := &fakeClient{
		indexes: []models.IndexStat{
			{
				SchemaName:      "public",
				RelName:         "orders",
				IndexRelName:    "orders_status_idx",
				IdxScan:         42,
				IndexSize:       65536,
				AccessMethod:    "btree",
				BacksForeignKey: true,
			},
		},
	}

	c := resource.NewIndexStatsCollector(resource.IndexStatsCollectorConfig{
		PGClient: client, Storage: storage, InstanceID: instance, Logger: logger,
	})
	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	hist, err := storage.GetIndexStats(ctx, snap)
	if err != nil || len(hist) != 1 || hist[0].IndexRelName != "orders_status_idx" {
		t.Fatalf("GetIndexStats = %+v, %v", hist, err)
	}
	cur, err := storage.GetCurrentIndexStats(ctx, instance)
	if err != nil || len(cur) != 1 || !cur[0].BacksForeignKey || cur[0].AccessMethod != "btree" {
		t.Fatalf("GetCurrentIndexStats = %+v, %v", cur, err)
	}

	client.indexesErr = errors.New("boom")
	if err := c.Collect(ctx, snap); err == nil {
		t.Fatal("expected error when GetStatIndexes fails")
	}
}

func TestDatabaseStatsCollector(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()
	client := &fakeClient{
		dbStats: &models.DatabaseStats{DatabaseName: "app", BlksHit: 990, BlksRead: 10, CacheHitRatio: 99.0},
		extStats: &models.ExtendedDatabaseStats{
			DatabaseName: "app",
			XactCommit:   1000,
			XactRollback: 5,
			TempFiles:    2,
			TempBytes:    4096,
			Deadlocks:    1,
		},
	}

	c := resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
		PGClient: client, Storage: storage, InstanceID: instance, Logger: logger,
	})
	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	gotSnap, err := storage.GetSnapshotByID(ctx, snap)
	if err != nil || gotSnap == nil || gotSnap.CacheHitRatio == nil || *gotSnap.CacheHitRatio != 99.0 {
		t.Fatalf("GetSnapshotByID cache hit ratio = %+v, %v", gotSnap, err)
	}
	ext, err := storage.GetExtendedDatabaseStats(ctx, snap)
	if err != nil || ext == nil || ext.Deadlocks != 1 {
		t.Fatalf("GetExtendedDatabaseStats = %+v, %v", ext, err)
	}
	curExt, curRatio, err := storage.GetCurrentDatabaseStats(ctx, instance)
	if err != nil || curExt == nil || curExt.XactCommit != 1000 || curRatio == nil || *curRatio != 99.0 {
		t.Fatalf("GetCurrentDatabaseStats = %+v, %v, %v", curExt, curRatio, err)
	}

	t.Run("errors on missing or failing stats", func(t *testing.T) {
		cases := []*fakeClient{
			{dbStatsErr: errors.New("db err")},
			{dbStats: nil},
			{dbStats: client.dbStats, extStatsErr: errors.New("ext err")},
			{dbStats: client.dbStats, extStats: nil},
		}
		for i, tc := range cases {
			col := resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
				PGClient: tc, Storage: storage, InstanceID: instance, Logger: logger,
			})
			if err := col.Collect(ctx, snap); err == nil {
				t.Errorf("case %d: expected error, got nil", i)
			}
		}
	})
}
