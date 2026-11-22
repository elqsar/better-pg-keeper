-- +migrate Up
-- 1 when autovacuum would not act on the table: autovacuum or track_counts off
-- server-wide, or autovacuum_enabled=false on the table. Older rows are 0,
-- which only means "not known to be disabled".
ALTER TABLE table_stats ADD COLUMN autovacuum_disabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN autovacuum_disabled INTEGER NOT NULL DEFAULT 0;

-- +migrate Down
-- SQLite migrations are append-only in production.
