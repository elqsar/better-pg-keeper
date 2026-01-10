-- +migrate Up
-- Raw pg_stat_statements data per snapshot (cumulative counters)
CREATE TABLE query_stats (
    id              INTEGER PRIMARY KEY,
    snapshot_id     INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    queryid         INTEGER NOT NULL,
    query           TEXT NOT NULL,
    calls           INTEGER NOT NULL,
    total_exec_time REAL NOT NULL,
    mean_exec_time  REAL NOT NULL,
    min_exec_time   REAL,
    max_exec_time   REAL,
    rows            INTEGER,
    shared_blks_hit INTEGER,
    shared_blks_read INTEGER,
    plans           INTEGER,
    total_plan_time REAL
);

CREATE INDEX idx_query_stats_snapshot ON query_stats(snapshot_id);
CREATE INDEX idx_query_stats_queryid ON query_stats(queryid);

-- +migrate Down
DROP INDEX IF EXISTS idx_query_stats_queryid;
DROP INDEX IF EXISTS idx_query_stats_snapshot;
DROP TABLE IF EXISTS query_stats;
