-- +migrate Up
CREATE TABLE bloat_stats (
    id              INTEGER PRIMARY KEY,
    snapshot_id     INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    schemaname      TEXT NOT NULL,
    relname         TEXT NOT NULL,
    n_dead_tup      INTEGER NOT NULL,
    n_live_tup      INTEGER NOT NULL,
    bloat_percent   REAL NOT NULL
);
CREATE INDEX idx_bloat_stats_snapshot ON bloat_stats(snapshot_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_bloat_stats_snapshot;
DROP TABLE IF EXISTS bloat_stats;
