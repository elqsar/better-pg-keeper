-- +migrate Up
-- Point-in-time snapshot metadata
CREATE TABLE snapshots (
    id          INTEGER PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    captured_at DATETIME NOT NULL,
    pg_version  TEXT,
    stats_reset DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_snapshots_instance_time ON snapshots(instance_id, captured_at);

-- +migrate Down
DROP INDEX IF EXISTS idx_snapshots_instance_time;
DROP TABLE IF EXISTS snapshots;
