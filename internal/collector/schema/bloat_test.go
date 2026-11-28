package schema_test

import (
	"context"
	"errors"
	"testing"

	"github.com/elqsar/pganalyzer/internal/collector/collectortest"
	"github.com/elqsar/pganalyzer/internal/collector/schema"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

type fakeClient struct {
	postgres.Client
	bloat []models.BloatInfo
	err   error
}

func (f *fakeClient) GetTableBloat(ctx context.Context) ([]models.BloatInfo, error) {
	return f.bloat, f.err
}

func TestBloatCollector_FiltersAndStores(t *testing.T) {
	storage, instance := collectortest.NewStorage(t)
	logger, _ := collectortest.Logger()

	client := &fakeClient{
		bloat: []models.BloatInfo{
			// Kept: exceeds dead-tuple threshold
			{SchemaName: "public", RelName: "dead_heavy", NDeadTup: 2500, NLiveTup: 10000, BloatPercent: 25.0, HeapBytes: 8 << 20},
			// Kept: large heap >= 64 MiB even with low dead tuples
			{SchemaName: "public", RelName: "large_heap", NDeadTup: 50, NLiveTup: 100000, BloatPercent: 0.05, HeapBytes: 80 << 20},
			// Filtered out: small heap and below dead-tuple threshold
			{SchemaName: "public", RelName: "small_clean", NDeadTup: 20, NLiveTup: 500, BloatPercent: 4.0, HeapBytes: 1 << 20},
		},
	}

	c := schema.NewBloatCollector(schema.BloatCollectorConfig{
		PGClient: client, Storage: storage, InstanceID: instance, Logger: logger,
	})
	if c.Name() != schema.BloatCollectorName {
		t.Fatalf("Name() = %q, want %q", c.Name(), schema.BloatCollectorName)
	}
	if c.Interval() != schema.DefaultBloatInterval {
		t.Fatalf("Interval() = %v, want %v", c.Interval(), schema.DefaultBloatInterval)
	}

	ctx := context.Background()
	snap := collectortest.Snapshot(t, storage, instance)
	if err := c.Collect(ctx, snap); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	hist, err := storage.GetBloatStats(ctx, snap)
	if err != nil {
		t.Fatalf("GetBloatStats: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 significant bloat rows, got %d: %+v", len(hist), hist)
	}

	cur, err := storage.GetCurrentBloatStats(ctx, instance)
	if err != nil || len(cur) != 2 {
		t.Fatalf("GetCurrentBloatStats = %+v, %v", cur, err)
	}

	client.err = errors.New("query failed")
	if err := c.Collect(ctx, snap); err == nil {
		t.Fatal("expected error when GetTableBloat fails")
	}
}
