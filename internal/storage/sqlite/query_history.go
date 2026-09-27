package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// GetQueryHistory returns independently retained samples, newest first.
func (s *SQLiteStorage) GetQueryHistory(ctx context.Context, instanceID, queryID int64, from, to time.Time, limit, offset int) ([]models.QueryHistorySample, error) {
	var fromUnix, toUnix int64
	if !from.IsZero() {
		fromUnix = from.UnixNano()
	}
	if !to.IsZero() {
		toUnix = to.UnixNano()
	}
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT sampled_at, queryid, query, calls, total_exec_time, mean_exec_time,
		       min_exec_time, max_exec_time, rows, shared_blks_hit, shared_blks_read,
		       plans, total_plan_time
		FROM query_history
		WHERE instance_id = ? AND queryid = ?
		  AND (? = 0 OR sampled_at_unix_ns >= ?)
		  AND (? = 0 OR sampled_at_unix_ns <= ?)
		ORDER BY sampled_at_unix_ns DESC, id DESC
		LIMIT ? OFFSET ?
	`, instanceID, queryID, !from.IsZero(), fromUnix, !to.IsZero(), toUnix, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("querying query history: %w", err)
	}
	defer rows.Close()

	samples := make([]models.QueryHistorySample, 0)
	for rows.Next() {
		var sample models.QueryHistorySample
		if err := rows.Scan(&sample.SampledAt, &sample.QueryID, &sample.Query,
			&sample.Calls, &sample.TotalExecTime, &sample.MeanExecTime,
			&sample.MinExecTime, &sample.MaxExecTime, &sample.Rows,
			&sample.SharedBlksHit, &sample.SharedBlksRead, &sample.Plans,
			&sample.TotalPlanTime); err != nil {
			return nil, fmt.Errorf("scanning query history: %w", err)
		}
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

// PurgeOldQueryHistory removes samples independently of snapshot retention.
func (s *SQLiteStorage) PurgeOldQueryHistory(ctx context.Context, retention time.Duration) (int64, error) {
	result, err := s.writeDB.ExecContext(ctx, `DELETE FROM query_history WHERE sampled_at_unix_ns < ?`, time.Now().Add(-retention).UnixNano())
	if err != nil {
		return 0, fmt.Errorf("purging query history: %w", err)
	}
	return result.RowsAffected()
}
