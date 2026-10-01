package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// ListNotificationStates returns every issue the notifier is tracking for an instance.
func (s *SQLiteStorage) ListNotificationStates(ctx context.Context, instanceID int64) ([]models.NotificationState, error) {
	rows, err := s.readDB.QueryContext(ctx, `
		SELECT instance_id, rule_id, target_object, severity, notified_at, cleared_at
		FROM notification_state
		WHERE instance_id = ?
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("querying notification state: %w", err)
	}
	defer rows.Close()

	var states []models.NotificationState
	for rows.Next() {
		var st models.NotificationState
		if err := rows.Scan(&st.InstanceID, &st.RuleID, &st.TargetObject, &st.Severity,
			&st.NotifiedAt, &st.ClearedAt); err != nil {
			return nil, fmt.Errorf("scanning notification state: %w", err)
		}
		states = append(states, st)
	}
	return states, rows.Err()
}

// UpsertNotificationState creates or replaces the state for one issue.
func (s *SQLiteStorage) UpsertNotificationState(ctx context.Context, st *models.NotificationState) error {
	var cleared any
	if st.ClearedAt != nil {
		cleared = st.ClearedAt.Round(0)
	}
	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO notification_state (instance_id, rule_id, target_object, severity, notified_at, cleared_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (instance_id, rule_id, target_object) DO UPDATE SET
			severity = excluded.severity,
			notified_at = excluded.notified_at,
			cleared_at = excluded.cleared_at
	`, st.InstanceID, st.RuleID, st.TargetObject, st.Severity, st.NotifiedAt.Round(0), cleared)
	if err != nil {
		return fmt.Errorf("upserting notification state: %w", err)
	}
	return nil
}

// DeleteNotificationState stops tracking one issue.
func (s *SQLiteStorage) DeleteNotificationState(ctx context.Context, instanceID int64, ruleID, targetObject string) error {
	_, err := s.writeDB.ExecContext(ctx, `
		DELETE FROM notification_state
		WHERE instance_id = ? AND rule_id = ? AND target_object = ?
	`, instanceID, ruleID, targetObject)
	if err != nil {
		return fmt.Errorf("deleting notification state: %w", err)
	}
	return nil
}

// GetNotifierTime reads a timestamp stored under key. ok is false when unset.
func (s *SQLiteStorage) GetNotifierTime(ctx context.Context, instanceID int64, key string) (t time.Time, ok bool, err error) {
	var value string
	err = s.readDB.QueryRowContext(ctx, `
		SELECT value FROM notifier_meta WHERE instance_id = ? AND key = ?
	`, instanceID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading notifier %s: %w", key, err)
	}
	t, err = time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parsing notifier %s: %w", key, err)
	}
	return t, true, nil
}

// SetNotifierTime stores a timestamp under key.
func (s *SQLiteStorage) SetNotifierTime(ctx context.Context, instanceID int64, key string, t time.Time) error {
	_, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO notifier_meta (instance_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT (instance_id, key) DO UPDATE SET value = excluded.value
	`, instanceID, key, t.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("writing notifier %s: %w", key, err)
	}
	return nil
}

// GetEarliestSnapshotWithCollector returns the oldest snapshot at or after
// notBefore that the named collector successfully contributed to, or nil.
func (s *SQLiteStorage) GetEarliestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notBefore time.Time) (*models.Snapshot, error) {
	var snap models.Snapshot
	err := s.readDB.QueryRowContext(ctx, `
		SELECT s.id, s.instance_id, s.captured_at, s.pg_version, s.stats_reset, s.cache_hit_ratio, s.created_at
		FROM snapshots s
		JOIN snapshot_collectors sc ON sc.snapshot_id = s.id
		WHERE s.instance_id = ? AND sc.collector = ? AND sc.status = ? AND s.captured_at >= ?
		ORDER BY s.captured_at ASC
		LIMIT 1
	`, instanceID, collector, models.CollectorStatusSuccess, notBefore.Round(0)).Scan(
		&snap.ID, &snap.InstanceID, &snap.CapturedAt, &snap.PGVersion,
		&snap.StatsReset, &snap.CacheHitRatio, &snap.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting earliest snapshot with collector %s: %w", collector, err)
	}
	return &snap, nil
}
