-- +migrate Up
-- Index statistics per snapshot
CREATE TABLE index_stats (
    id              INTEGER PRIMARY KEY,
    snapshot_id     INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    schemaname      TEXT NOT NULL,
    relname         TEXT NOT NULL,
    indexrelname    TEXT NOT NULL,
    idx_scan        INTEGER,
    idx_tup_read    INTEGER,
    idx_tup_fetch   INTEGER,
    index_size      INTEGER,
    is_unique       BOOLEAN,
    is_primary      BOOLEAN
);

CREATE INDEX idx_index_stats_snapshot ON index_stats(snapshot_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_index_stats_snapshot;
DROP TABLE IF EXISTS index_stats;
