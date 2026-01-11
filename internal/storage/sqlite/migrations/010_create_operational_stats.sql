-- +migrate Up
-- Connection activity snapshots
CREATE TABLE connection_activity (
    id               INTEGER PRIMARY KEY,
    snapshot_id      INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    active_count     INTEGER NOT NULL DEFAULT 0,
    idle_count       INTEGER NOT NULL DEFAULT 0,
    idle_in_tx_count INTEGER NOT NULL DEFAULT 0,
    idle_in_tx_aborted INTEGER NOT NULL DEFAULT 0,
    waiting_count    INTEGER NOT NULL DEFAULT 0,
    total_connections INTEGER NOT NULL DEFAULT 0,
    max_connections  INTEGER NOT NULL DEFAULT 100
);
CREATE INDEX idx_connection_activity_snapshot ON connection_activity(snapshot_id);

-- Long running queries
CREATE TABLE long_running_queries (
    id               INTEGER PRIMARY KEY,
    snapshot_id      INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    pid              INTEGER NOT NULL,
    usename          TEXT,
    datname          TEXT,
    query            TEXT,
    state            TEXT,
    wait_event_type  TEXT,
    wait_event       TEXT,
    query_start      DATETIME,
    duration_seconds REAL
);
CREATE INDEX idx_long_running_snapshot ON long_running_queries(snapshot_id);

-- Idle in transaction connections
CREATE TABLE idle_in_transaction (
    id               INTEGER PRIMARY KEY,
    snapshot_id      INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    pid              INTEGER NOT NULL,
    usename          TEXT,
    datname          TEXT,
    state            TEXT,
    xact_start       DATETIME,
    duration_seconds REAL,
    query            TEXT
);
CREATE INDEX idx_idle_in_tx_snapshot ON idle_in_transaction(snapshot_id);

-- Lock statistics
CREATE TABLE lock_stats (
    id                  INTEGER PRIMARY KEY,
    snapshot_id         INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    total_locks         INTEGER NOT NULL DEFAULT 0,
    granted_locks       INTEGER NOT NULL DEFAULT 0,
    waiting_locks       INTEGER NOT NULL DEFAULT 0,
    access_share_locks  INTEGER NOT NULL DEFAULT 0,
    row_exclusive_locks INTEGER NOT NULL DEFAULT 0,
    exclusive_locks     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_lock_stats_snapshot ON lock_stats(snapshot_id);

-- Blocked queries
CREATE TABLE blocked_queries (
    id               INTEGER PRIMARY KEY,
    snapshot_id      INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    blocked_pid      INTEGER NOT NULL,
    blocked_user     TEXT,
    blocked_query    TEXT,
    blocked_start    DATETIME,
    wait_duration_seconds REAL,
    blocking_pid     INTEGER,
    blocking_user    TEXT,
    blocking_query   TEXT,
    lock_type        TEXT,
    lock_mode        TEXT,
    relation         TEXT
);
CREATE INDEX idx_blocked_queries_snapshot ON blocked_queries(snapshot_id);

-- Extended database stats
CREATE TABLE extended_database_stats (
    id             INTEGER PRIMARY KEY,
    snapshot_id    INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    database_name  TEXT NOT NULL,
    xact_commit    INTEGER NOT NULL DEFAULT 0,
    xact_rollback  INTEGER NOT NULL DEFAULT 0,
    temp_files     INTEGER NOT NULL DEFAULT 0,
    temp_bytes     INTEGER NOT NULL DEFAULT 0,
    deadlocks      INTEGER NOT NULL DEFAULT 0,
    confl_lock     INTEGER NOT NULL DEFAULT 0,
    confl_snapshot INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_extended_db_stats_snapshot ON extended_database_stats(snapshot_id);

-- +migrate Down
DROP INDEX IF EXISTS idx_extended_db_stats_snapshot;
DROP TABLE IF EXISTS extended_database_stats;
DROP INDEX IF EXISTS idx_blocked_queries_snapshot;
DROP TABLE IF EXISTS blocked_queries;
DROP INDEX IF EXISTS idx_lock_stats_snapshot;
DROP TABLE IF EXISTS lock_stats;
DROP INDEX IF EXISTS idx_idle_in_tx_snapshot;
DROP TABLE IF EXISTS idle_in_transaction;
DROP INDEX IF EXISTS idx_long_running_snapshot;
DROP TABLE IF EXISTS long_running_queries;
DROP INDEX IF EXISTS idx_connection_activity_snapshot;
DROP TABLE IF EXISTS connection_activity;
