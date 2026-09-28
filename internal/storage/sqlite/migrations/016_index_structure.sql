-- +migrate Up
-- Index structure, so duplicates are detected from definitions rather than names,
-- and the statistics window, so "never scanned" can be qualified by how long
-- scans have actually been counted.
ALTER TABLE index_stats ADD COLUMN access_method TEXT NOT NULL DEFAULT '';
ALTER TABLE index_stats ADD COLUMN index_def TEXT NOT NULL DEFAULT '';
ALTER TABLE index_stats ADD COLUMN key_columns TEXT NOT NULL DEFAULT '';
ALTER TABLE index_stats ADD COLUMN include_columns TEXT NOT NULL DEFAULT '';
ALTER TABLE index_stats ADD COLUMN expressions TEXT NOT NULL DEFAULT '';
ALTER TABLE index_stats ADD COLUMN predicate TEXT NOT NULL DEFAULT '';
ALTER TABLE index_stats ADD COLUMN backs_foreign_key BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE index_stats ADD COLUMN stats_since DATETIME;

-- +migrate Down
ALTER TABLE index_stats DROP COLUMN stats_since;
ALTER TABLE index_stats DROP COLUMN backs_foreign_key;
ALTER TABLE index_stats DROP COLUMN predicate;
ALTER TABLE index_stats DROP COLUMN expressions;
ALTER TABLE index_stats DROP COLUMN include_columns;
ALTER TABLE index_stats DROP COLUMN key_columns;
ALTER TABLE index_stats DROP COLUMN index_def;
ALTER TABLE index_stats DROP COLUMN access_method;
