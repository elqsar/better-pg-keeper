-- +migrate Up
-- Table statistics per snapshot
CREATE TABLE table_stats (
    id              INTEGER PRIMARY KEY,
    snapshot_id     INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    schemaname      TEXT NOT NULL,
    relname         TEXT NOT NULL,
    seq_scan        INTEGER,
    seq_tup_read    INTEGER,
    idx_scan        INTEGER,
    idx_tup_fetch   INTEGER,
    n_live_tup      INTEGER,
    n_dead_tup      INTEGER,
    last_vacuum     DATETIME,
    last_autovacuum DATETIME,
    last_analyze    DATETIME,
    table_size      INTEGER,
    index_size      INTEGER
);

CREATE INDEX idx_table_stats_snapshot ON table_stats(snapshot_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_table_stats_snapshot;
DROP TABLE IF EXISTS table_stats;
