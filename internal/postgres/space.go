package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/elqsar/pganalyzer/internal/models"
)

const minMeasuredHeapBytes = 64 << 20

// measureHeapSpace always provides a conservative, explicitly rough fallback.
// pgstattuple_approx is budgeted to ten tables per hourly collector pass.
func (c *PgxClient) measureHeapSpace(ctx context.Context, stats []models.BloatInfo) []models.BloatInfo {
	now := time.Now().UTC()
	for i := range stats {
		b := &stats[i]
		if v, ok := roughDeadTupleBytes(*b); ok {
			b.DeadTupleBytes = &v
		}
		b.MeasurementSource = "stats_estimate"
		b.MeasuredAt = &now
	}

	var schema string
	err := c.pool.QueryRow(ctx, `
		SELECT n.nspname FROM pg_extension e
		JOIN pg_namespace n ON n.oid = e.extnamespace
		WHERE e.extname = 'pgstattuple'`).Scan(&schema)
	if err != nil {
		return stats
	}

	for _, i := range selectHeapCandidates(stats) {
		heap, dead, free, measured, err := c.measureOneHeap(ctx, schema, stats[i].RelationOID)
		if err != nil {
			// A permission failure usually applies to every table. Keep the
			// fallback and stop costly repeated attempts for this pass.
			if isPermissionError(err) {
				break
			}
			continue
		}
		stats[i].DeadTupleBytes = &dead
		stats[i].FreeBytes = &free
		stats[i].HeapBytes = heap
		stats[i].MeasurementSource = "pgstattuple_approx"
		stats[i].MeasuredAt = &measured
	}
	return stats
}

func roughDeadTupleBytes(b models.BloatInfo) (int64, bool) {
	if b.HeapBytes <= 0 || b.NDeadTup <= 0 || b.NLiveTup < 0 || b.NLiveTup+b.NDeadTup <= 0 {
		return 0, false
	}
	return int64(float64(b.HeapBytes) * float64(b.NDeadTup) / float64(b.NLiveTup+b.NDeadTup)), true
}

// Seven largest rough dead footprints plus three large heaps reveal both
// active churn and already-vacuumed space without scanning every relation.
func selectHeapCandidates(stats []models.BloatInfo) []int {
	byDead := make([]int, 0, len(stats))
	bySize := make([]int, 0, len(stats))
	for i, b := range stats {
		if b.HeapBytes >= minMeasuredHeapBytes {
			byDead = append(byDead, i)
			bySize = append(bySize, i)
		}
	}
	sort.Slice(byDead, func(i, j int) bool {
		a, b := stats[byDead[i]], stats[byDead[j]]
		var av, bv int64
		if a.DeadTupleBytes != nil {
			av = *a.DeadTupleBytes
		}
		if b.DeadTupleBytes != nil {
			bv = *b.DeadTupleBytes
		}
		if av == bv {
			return a.HeapBytes > b.HeapBytes
		}
		return av > bv
	})
	sort.Slice(bySize, func(i, j int) bool { return stats[bySize[i]].HeapBytes > stats[bySize[j]].HeapBytes })
	selected := make(map[int]bool)
	indexes := make([]int, 0, 10)
	appendTop := func(list []int, limit int) {
		for _, i := range list {
			if len(indexes) >= 10 || limit == 0 {
				return
			}
			if selected[i] {
				continue
			}
			selected[i] = true
			indexes = append(indexes, i)
			limit--
		}
	}
	appendTop(byDead, 7)
	appendTop(bySize, 3)
	return indexes
}

func (c *PgxClient) measureOneHeap(ctx context.Context, schema string, oid uint32) (int64, int64, int64, time.Time, error) {
	measureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := c.pool.Begin(measureCtx)
	if err != nil {
		return 0, 0, 0, time.Time{}, err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // read-only; always rolled back
	if _, err := tx.Exec(measureCtx, `SET LOCAL lock_timeout = '100ms'`); err != nil {
		return 0, 0, 0, time.Time{}, err
	}
	if _, err := tx.Exec(measureCtx, `SET LOCAL statement_timeout = '5s'`); err != nil {
		return 0, 0, 0, time.Time{}, err
	}
	query := fmt.Sprintf(`SELECT table_len, dead_tuple_len, approx_free_space FROM %s.pgstattuple_approx($1::oid::regclass)`, quoteIdentifier(schema))
	var heap, dead, free int64
	if err := tx.QueryRow(measureCtx, query, oid).Scan(&heap, &dead, &free); err != nil {
		return 0, 0, 0, time.Time{}, err
	}
	return heap, dead, free, time.Now().UTC(), nil
}

func isPermissionError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "42501" || pgErr.Code == "42883")
}
