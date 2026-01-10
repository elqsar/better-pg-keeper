-- +migrate Up
ALTER TABLE snapshots ADD COLUMN cache_hit_ratio REAL;

-- +migrate Down
-- SQLite doesn't support DROP COLUMN directly, so we recreate the table
CREATE TABLE snapshots_new (
    id            INTEGER PRIMARY KEY,
    instance_id   INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    captured_at   DATETIME NOT NULL,
    pg_version    TEXT,
    stats_reset   DATETIME,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO snapshots_new (id, instance_id, captured_at, pg_version, stats_reset, created_at)
SELECT id, instance_id, captured_at, pg_version, stats_reset, created_at FROM snapshots;

DROP TABLE snapshots;
ALTER TABLE snapshots_new RENAME TO snapshots;

CREATE INDEX idx_snapshots_instance_time ON snapshots(instance_id, captured_at);
