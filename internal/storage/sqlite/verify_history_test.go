package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/verify"
)

// TestVerifyAgainstQueryHistory runs fix verification on real query_history
// rows, covering the time filtering and ordering the verifier relies on.
func TestVerifyAgainstQueryHistory(t *testing.T) {
	storage := setupTestStorage(t)
	ctx := context.Background()
	instID, err := storage.CreateInstance(ctx, &models.Instance{Name: "v", Host: "h", Port: 5432, Database: "d"})
	if err != nil {
		t.Fatal(err)
	}

	const queryID = int64(-8870412367183748000)
	resolved := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	now := resolved.Add(30 * time.Hour)
	var calls int64
	var total float64
	for ts := resolved.Add(-50 * time.Hour); !ts.After(now); ts = ts.Add(5 * time.Minute) {
		mean := 500.0
		if ts.After(resolved) {
			mean = 50
		}
		calls += 20
		total += 20 * mean
		if _, err := storage.writeDB.ExecContext(ctx, `
			INSERT INTO query_history (instance_id, sampled_at, sampled_at_unix_ns, queryid, query, calls, total_exec_time, mean_exec_time,
				min_exec_time, max_exec_time, rows, shared_blks_hit, shared_blks_read, plans, total_plan_time)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0)
		`, instID, ts, ts.UnixNano(), queryID, "SELECT 1", calls, total, total/float64(calls)); err != nil {
			t.Fatal(err)
		}
	}

	sug := models.Suggestion{ID: 1, InstanceID: instID, RuleID: "index_recommendation", Status: models.StatusResolved,
		ResolvedAt: &resolved, Metadata: `{"query_ids":[-8870412367183748000]}`}
	res, err := verify.New(storage, 24*time.Hour).Verify(ctx, sug, now)
	if err != nil {
		t.Fatal(err)
	}
	q := res.Queries[0]
	if res.Verdict != verify.VerdictImproved || q.Before.MeanMs != 500 || q.After.MeanMs != 50 {
		t.Fatalf("result = %+v, before %+v, after %+v", res, q.Before, q.After)
	}
	// 24h on each side at 5-minute samples.
	if q.Before.Calls != 288*20 || q.After.Calls != 288*20 {
		t.Errorf("calls = %d before, %d after; want %d each", q.Before.Calls, q.After.Calls, 288*20)
	}
}
