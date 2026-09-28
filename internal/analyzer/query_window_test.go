package analyzer

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

func TestRecentQueryStats(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	storage := newMockStorage()
	storage.snapshots[1] = &models.Snapshot{ID: 1, InstanceID: 1, CapturedAt: now.Add(-2 * time.Hour)}
	storage.snapshots[2] = &models.Snapshot{ID: 2, InstanceID: 1, CapturedAt: now}
	storage.queryStats[1] = []models.QueryStat{{QueryID: 1}}
	storage.queryStats[2] = []models.QueryStat{{QueryID: 1}}

	// Two hours of history is less than the 24h window.
	if w, err := RecentQueryStats(ctx, storage, 1, 24*time.Hour, now); err != nil || w != nil {
		t.Fatalf("short history = %+v, %v; want nil", w, err)
	}

	storage.queryDeltas = []models.QueryStatDelta{
		{QueryID: 1, DeltaCalls: 10, DeltaTotalTime: 5000, MeanExecTime: 500},
		{QueryID: 2, DeltaCalls: 0},
	}
	var from, to int64
	storage.onDelta = func(f, t int64) { from, to = f, t }
	w, err := RecentQueryStats(ctx, storage, 1, time.Hour, now)
	if err != nil || w == nil {
		t.Fatalf("window = %+v, %v", w, err)
	}
	if from != 1 || to != 2 || w.Span() != 2*time.Hour {
		t.Errorf("delta %d->%d span %s, want 1->2 over 2h", from, to, w.Span())
	}
	if len(w.Queries) != 1 || w.Queries[0].QueryID != 1 {
		t.Errorf("queries = %+v, want only the one that ran", w.Queries)
	}
}
