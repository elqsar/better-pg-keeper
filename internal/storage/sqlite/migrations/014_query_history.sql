-- +migrate Up
-- Query samples outlive their source snapshots when query retention is longer.
CREATE TABLE query_history (
    id INTEGER PRIMARY KEY,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    sampled_at DATETIME NOT NULL,
    sampled_at_unix_ns INTEGER NOT NULL,
    queryid INTEGER NOT NULL,
    query TEXT NOT NULL,
    calls INTEGER NOT NULL,
    total_exec_time REAL NOT NULL,
    mean_exec_time REAL NOT NULL,
    min_exec_time REAL,
    max_exec_time REAL,
    rows INTEGER,
    shared_blks_hit INTEGER,
    shared_blks_read INTEGER,
    plans INTEGER,
    total_plan_time REAL
);
CREATE INDEX idx_query_history_lookup ON query_history(instance_id, queryid, sampled_at_unix_ns DESC);
CREATE INDEX idx_query_history_retention ON query_history(sampled_at_unix_ns);
-- Existing query_stats are backfilled in Go within this migration transaction,
-- so legacy timestamps with local zones and monotonic suffixes are normalized.

-- +migrate Down
DROP INDEX IF EXISTS idx_query_history_retention;
DROP INDEX IF EXISTS idx_query_history_lookup;
DROP TABLE IF EXISTS query_history;
