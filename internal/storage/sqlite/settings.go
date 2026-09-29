package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/elqsar/pganalyzer/internal/models"
)

// SaveServerSettings stores a snapshot's server settings, replacing any earlier ones.
func (s *SQLiteStorage) SaveServerSettings(ctx context.Context, snapshotID int64, settings *models.ServerSettings) error {
	if settings == nil {
		return nil
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encoding server settings: %w", err)
	}
	if _, err := s.writeDB.ExecContext(ctx, `
		INSERT INTO server_settings (snapshot_id, payload) VALUES (?, ?)
		ON CONFLICT (snapshot_id) DO UPDATE SET payload = excluded.payload
	`, snapshotID, string(payload)); err != nil {
		return fmt.Errorf("saving server settings: %w", err)
	}
	return nil
}

// GetServerSettings returns a snapshot's server settings, or nil when none were stored.
func (s *SQLiteStorage) GetServerSettings(ctx context.Context, snapshotID int64) (*models.ServerSettings, error) {
	var payload string
	err := s.readDB.QueryRowContext(ctx, `SELECT payload FROM server_settings WHERE snapshot_id = ?`, snapshotID).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting server settings: %w", err)
	}
	var settings models.ServerSettings
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return nil, fmt.Errorf("decoding server settings: %w", err)
	}
	return &settings, nil
}
