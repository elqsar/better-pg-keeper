-- +migrate Up
-- Add migration 016 structural and statistics-window columns to current_index_stats
-- so current-state reads preserve the same index metadata as index_stats.
ALTER TABLE current_index_stats ADD COLUMN access_method TEXT NOT NULL DEFAULT '';
ALTER TABLE current_index_stats ADD COLUMN index_def TEXT NOT NULL DEFAULT '';
ALTER TABLE current_index_stats ADD COLUMN key_columns TEXT NOT NULL DEFAULT '';
ALTER TABLE current_index_stats ADD COLUMN include_columns TEXT NOT NULL DEFAULT '';
ALTER TABLE current_index_stats ADD COLUMN expressions TEXT NOT NULL DEFAULT '';
ALTER TABLE current_index_stats ADD COLUMN predicate TEXT NOT NULL DEFAULT '';
ALTER TABLE current_index_stats ADD COLUMN backs_foreign_key INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_index_stats ADD COLUMN stats_since DATETIME;

-- Preserve all (blocked_pid, blocking_pid) pairs when one session is blocked by
-- multiple lock holders.
CREATE TABLE current_blocked_queries_new (
    instance_id           INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    blocked_pid           INTEGER NOT NULL,
    blocked_user          TEXT,
    blocked_query         TEXT,
    blocked_start         DATETIME,
    wait_duration_seconds REAL,
    blocking_pid          INTEGER NOT NULL,
    blocking_user         TEXT,
    blocking_query        TEXT,
    lock_type             TEXT,
    lock_mode             TEXT,
    relation              TEXT,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, blocked_pid, blocking_pid)
);

INSERT OR REPLACE INTO current_blocked_queries_new (
    instance_id, blocked_pid, blocked_user, blocked_query, blocked_start,
    wait_duration_seconds, blocking_pid, blocking_user, blocking_query,
    lock_type, lock_mode, relation, updated_at
)
SELECT
    instance_id, blocked_pid, blocked_user, blocked_query, blocked_start,
    wait_duration_seconds, COALESCE(blocking_pid, 0), blocking_user, blocking_query,
    lock_type, lock_mode, relation, updated_at
FROM current_blocked_queries;

DROP TABLE current_blocked_queries;
ALTER TABLE current_blocked_queries_new RENAME TO current_blocked_queries;

-- Strip any residual process-local monotonic clock suffixes from text timestamps.
UPDATE explain_plans
SET captured_at = substr(captured_at, 1, instr(captured_at, ' m=') - 1)
WHERE instr(captured_at, ' m=') > 0;
UPDATE query_history
SET sampled_at = substr(sampled_at, 1, instr(sampled_at, ' m=') - 1)
WHERE instr(sampled_at, ' m=') > 0;
UPDATE bloat_stats
SET measured_at = substr(measured_at, 1, instr(measured_at, ' m=') - 1)
WHERE measured_at IS NOT NULL AND instr(measured_at, ' m=') > 0;
UPDATE current_bloat_stats
SET measured_at = substr(measured_at, 1, instr(measured_at, ' m=') - 1)
WHERE measured_at IS NOT NULL AND instr(measured_at, ' m=') > 0;

-- +migrate Down
-- SQLite migrations are append-only in production.
