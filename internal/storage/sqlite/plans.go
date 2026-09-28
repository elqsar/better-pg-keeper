package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// SaveQueryPlans stores a snapshot's query plan report, replacing any earlier one.
func (s *SQLiteStorage) SaveQueryPlans(ctx context.Context, snapshotID int64, report *models.QueryPlanReport) error {
	if report == nil {
		return nil
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encoding query plans: %w", err)
	}
	if _, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO query_plans (snapshot_id, payload) VALUES (?, ?)
		ON CONFLICT (snapshot_id) DO UPDATE SET payload = excluded.payload
	`, snapshotID, string(payload)); err != nil {
		return fmt.Errorf("saving query plans: %w", err)
	}
	return nil
}

// GetQueryPlans returns a snapshot's query plan report, or nil when none was stored.
func (s *SQLiteStorage) GetQueryPlans(ctx context.Context, snapshotID int64) (*models.QueryPlanReport, error) {
	var payload string
	err := s.readDB.QueryRowContext(ctx, `SELECT payload FROM query_plans WHERE snapshot_id = ?`, snapshotID).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting query plans: %w", err)
	}
	var report models.QueryPlanReport
	if err := json.Unmarshal([]byte(payload), &report); err != nil {
		return nil, fmt.Errorf("decoding query plans: %w", err)
	}
	return &report, nil
}

// PurgeOldExplainPlans removes explain plans captured before the retention period.
func (s *SQLiteStorage) PurgeOldExplainPlans(ctx context.Context, retention time.Duration) (int64, error) {
	result, err := s.writeDB.ExecContext(ctx, `DELETE FROM explain_plans WHERE captured_at < ?`, time.Now().Add(-retention).Round(0))
	if err != nil {
		return 0, fmt.Errorf("purging explain plans: %w", err)
	}
	return result.RowsAffected()
}
