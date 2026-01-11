-- +migrate Up

-- Single-row tables (one row per instance)
CREATE TABLE current_connection_activity (
    instance_id      INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    active_count     INTEGER NOT NULL DEFAULT 0,
    idle_count       INTEGER NOT NULL DEFAULT 0,
    idle_in_tx_count INTEGER NOT NULL DEFAULT 0,
    idle_in_tx_aborted INTEGER NOT NULL DEFAULT 0,
    waiting_count    INTEGER NOT NULL DEFAULT 0,
    total_connections INTEGER NOT NULL DEFAULT 0,
    max_connections  INTEGER NOT NULL DEFAULT 100,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE current_lock_stats (
    instance_id         INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    total_locks         INTEGER NOT NULL DEFAULT 0,
    granted_locks       INTEGER NOT NULL DEFAULT 0,
    waiting_locks       INTEGER NOT NULL DEFAULT 0,
    access_share_locks  INTEGER NOT NULL DEFAULT 0,
    row_exclusive_locks INTEGER NOT NULL DEFAULT 0,
    exclusive_locks     INTEGER NOT NULL DEFAULT 0,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE current_database_stats (
    instance_id     INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    database_name   TEXT NOT NULL,
    xact_commit     INTEGER NOT NULL DEFAULT 0,
    xact_rollback   INTEGER NOT NULL DEFAULT 0,
    temp_files      INTEGER NOT NULL DEFAULT 0,
    temp_bytes      INTEGER NOT NULL DEFAULT 0,
    deadlocks       INTEGER NOT NULL DEFAULT 0,
    confl_lock      INTEGER NOT NULL DEFAULT 0,
    confl_snapshot  INTEGER NOT NULL DEFAULT 0,
    cache_hit_ratio REAL,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Multi-row tables (multiple rows per instance)
CREATE TABLE current_long_running_queries (
    instance_id      INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    pid              INTEGER NOT NULL,
    usename          TEXT,
    datname          TEXT,
    query            TEXT,
    state            TEXT,
    wait_event_type  TEXT,
    wait_event       TEXT,
    query_start      DATETIME,
    duration_seconds REAL,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, pid)
);

CREATE TABLE current_idle_in_transaction (
    instance_id      INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    pid              INTEGER NOT NULL,
    usename          TEXT,
    datname          TEXT,
    state            TEXT,
    xact_start       DATETIME,
    duration_seconds REAL,
    query            TEXT,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, pid)
);

CREATE TABLE current_blocked_queries (
    instance_id           INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    blocked_pid           INTEGER NOT NULL,
    blocked_user          TEXT,
    blocked_query         TEXT,
    blocked_start         DATETIME,
    wait_duration_seconds REAL,
    blocking_pid          INTEGER,
    blocking_user         TEXT,
    blocking_query        TEXT,
    lock_type             TEXT,
    lock_mode             TEXT,
    relation              TEXT,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, blocked_pid)
);

CREATE TABLE current_query_stats (
    instance_id      INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    queryid          INTEGER NOT NULL,
    query            TEXT NOT NULL,
    calls            INTEGER NOT NULL,
    total_exec_time  REAL NOT NULL,
    mean_exec_time   REAL NOT NULL,
    min_exec_time    REAL,
    max_exec_time    REAL,
    rows             INTEGER,
    shared_blks_hit  INTEGER,
    shared_blks_read INTEGER,
    plans            INTEGER,
    total_plan_time  REAL,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, queryid)
);
CREATE INDEX idx_current_query_stats_time ON current_query_stats(instance_id, total_exec_time DESC);

CREATE TABLE current_table_stats (
    instance_id     INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    schemaname      TEXT NOT NULL,
    relname         TEXT NOT NULL,
    seq_scan        INTEGER NOT NULL DEFAULT 0,
    seq_tup_read    INTEGER NOT NULL DEFAULT 0,
    idx_scan        INTEGER,
    idx_tup_fetch   INTEGER,
    n_live_tup      INTEGER NOT NULL DEFAULT 0,
    n_dead_tup      INTEGER NOT NULL DEFAULT 0,
    last_vacuum     DATETIME,
    last_autovacuum DATETIME,
    last_analyze    DATETIME,
    table_size      INTEGER NOT NULL DEFAULT 0,
    index_size      INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, schemaname, relname)
);

CREATE TABLE current_index_stats (
    instance_id   INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    schemaname    TEXT NOT NULL,
    relname       TEXT NOT NULL,
    indexrelname  TEXT NOT NULL,
    idx_scan      INTEGER NOT NULL DEFAULT 0,
    idx_tup_read  INTEGER NOT NULL DEFAULT 0,
    idx_tup_fetch INTEGER NOT NULL DEFAULT 0,
    index_size    INTEGER NOT NULL DEFAULT 0,
    is_unique     INTEGER NOT NULL DEFAULT 0,
    is_primary    INTEGER NOT NULL DEFAULT 0,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, schemaname, relname, indexrelname)
);

CREATE TABLE current_bloat_stats (
    instance_id   INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    schemaname    TEXT NOT NULL,
    relname       TEXT NOT NULL,
    n_dead_tup    INTEGER NOT NULL DEFAULT 0,
    n_live_tup    INTEGER NOT NULL DEFAULT 0,
    bloat_percent REAL NOT NULL DEFAULT 0,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, schemaname, relname)
);

-- +migrate Down
DROP TABLE IF EXISTS current_bloat_stats;
DROP TABLE IF EXISTS current_index_stats;
DROP TABLE IF EXISTS current_table_stats;
DROP INDEX IF EXISTS idx_current_query_stats_time;
DROP TABLE IF EXISTS current_query_stats;
DROP TABLE IF EXISTS current_blocked_queries;
DROP TABLE IF EXISTS current_idle_in_transaction;
DROP TABLE IF EXISTS current_long_running_queries;
DROP TABLE IF EXISTS current_database_stats;
DROP TABLE IF EXISTS current_lock_stats;
DROP TABLE IF EXISTS current_connection_activity;
