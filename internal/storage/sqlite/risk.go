package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// SaveOutageRisk stores a snapshot's outage-risk document, replacing any earlier one.
func (s *SQLiteStorage) SaveOutageRisk(ctx context.Context, snapshotID int64, risk *models.OutageRisk) error {
	if risk == nil {
		return nil
	}
	payload, err := json.Marshal(risk)
	if err != nil {
		return fmt.Errorf("encoding outage risk: %w", err)
	}
	if _, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO outage_risk (snapshot_id, payload) VALUES (?, ?)
		ON CONFLICT (snapshot_id) DO UPDATE SET payload = excluded.payload
	`, snapshotID, string(payload)); err != nil {
		return fmt.Errorf("saving outage risk: %w", err)
	}
	return nil
}

// GetOutageRisk returns a snapshot's outage-risk document, or nil when none was stored.
func (s *SQLiteStorage) GetOutageRisk(ctx context.Context, snapshotID int64) (*models.OutageRisk, error) {
	var payload string
	err := s.readDB.QueryRowContext(ctx, `SELECT payload FROM outage_risk WHERE snapshot_id = ?`, snapshotID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting outage risk: %w", err)
	}
	var risk models.OutageRisk
	if err := json.Unmarshal([]byte(payload), &risk); err != nil {
		return nil, fmt.Errorf("decoding outage risk: %w", err)
	}
	return &risk, nil
}

// RecordSize adds a size sample, keeping at most one per instance per hour (the
// latest). Hourly points are plenty for a growth trend and keep 90 days small.
func (s *SQLiteStorage) RecordSize(ctx context.Context, instanceID int64, at time.Time, clusterBytes int64) error {
	hour := at.Truncate(time.Hour).Unix()
	if _, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO size_history (instance_id, hour_unix, captured_at_unix_ns, cluster_bytes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (instance_id, hour_unix) DO UPDATE SET
			captured_at_unix_ns = excluded.captured_at_unix_ns,
			cluster_bytes = excluded.cluster_bytes
	`, instanceID, hour, at.UnixNano(), clusterBytes); err != nil {
		return fmt.Errorf("recording size: %w", err)
	}
	return nil
}

// GetSizeHistory returns size samples captured at or after since, oldest first.
func (s *SQLiteStorage) GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT captured_at_unix_ns, cluster_bytes
		FROM size_history
		WHERE instance_id = ? AND captured_at_unix_ns >= ?
		ORDER BY captured_at_unix_ns
	`, instanceID, since.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("querying size history: %w", err)
	}
	defer rows.Close()

	var samples []models.SizeSample
	for rows.Next() {
		var ns int64
		var sample models.SizeSample
		if err := rows.Scan(&ns, &sample.ClusterBytes); err != nil {
			return nil, fmt.Errorf("scanning size history: %w", err)
		}
		sample.CapturedAt = time.Unix(0, ns)
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

// PurgeOldSizeHistory removes size samples older than the retention period.
func (s *SQLiteStorage) PurgeOldSizeHistory(ctx context.Context, retention time.Duration) (int64, error) {
	result, err := s.writeDB.ExecContext(ctx, `DELETE FROM size_history WHERE captured_at_unix_ns < ?`, time.Now().Add(-retention).UnixNano())
	if err != nil {
		return 0, fmt.Errorf("purging size history: %w", err)
	}
	return result.RowsAffected()
}

// GetConnectionPeak returns the snapshot with the highest connection utilization
// captured in [since, until], or nil when there is none.
func (s *SQLiteStorage) GetConnectionPeak(ctx context.Context, instanceID int64, since, until time.Time) (*models.ConnectionPeak, error) {
	var peak models.ConnectionPeak
	err := s.readDB.QueryRowContext(ctx, `
		SELECT s.captured_at, ca.total_connections, ca.idle_count, ca.max_connections
		FROM connection_activity ca
		JOIN snapshots s ON s.id = ca.snapshot_id
		WHERE s.instance_id = ? AND s.captured_at >= ? AND s.captured_at <= ?
		  AND ca.max_connections > 0
		ORDER BY CAST(ca.total_connections AS REAL) / ca.max_connections DESC, s.captured_at DESC
		LIMIT 1
	`, instanceID, since.Round(0), until.Round(0)).Scan(
		&peak.CapturedAt, &peak.TotalConnections, &peak.IdleCount, &peak.MaxConnections,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting connection peak: %w", err)
	}
	return &peak, nil
}
