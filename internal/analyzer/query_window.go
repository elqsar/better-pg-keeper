package analyzer

import (
	"context"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// QueryWindowStorage is what RecentQueryStats reads.
type QueryWindowStorage interface {
	GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error)
	GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error)
}

// QueryWindow is query execution over a recent window.
type QueryWindow struct {
	From time.Time
	To   time.Time
	// Queries holds only queries that ran in the window.
	Queries []models.QueryStatDelta
}

// Span is the length of the window actually covered.
func (w *QueryWindow) Span() time.Duration {
	return w.To.Sub(w.From)
}

// RecentQueryStats returns query execution over the last window, measured
// against the newest query_stats snapshot at least that old, as the slow-query
// analyzer does. It returns nil until a window's worth of history exists;
// callers then fall back to lifetime statistics.
func RecentQueryStats(ctx context.Context, storage QueryWindowStorage, instanceID int64, window time.Duration, now time.Time) (*QueryWindow, error) {
	if window <= 0 {
		window = DefaultSlowQueryWindow
	}
	latest, err := storage.GetLatestSnapshotWithCollector(ctx, instanceID, DomainQueryStats, now)
	if err != nil || latest == nil {
		return nil, err
	}
	baseline, err := storage.GetLatestSnapshotWithCollector(ctx, instanceID, DomainQueryStats, latest.CapturedAt.Add(-window))
	if err != nil || baseline == nil || baseline.ID == latest.ID {
		return nil, err
	}
	deltas, err := storage.GetQueryStatsDelta(ctx, baseline.ID, latest.ID)
	if err != nil {
		return nil, err
	}
	w := &QueryWindow{From: baseline.CapturedAt, To: latest.CapturedAt}
	for _, d := range deltas {
		if d.DeltaCalls > 0 {
			w.Queries = append(w.Queries, d)
		}
	}
	return w, nil
}
