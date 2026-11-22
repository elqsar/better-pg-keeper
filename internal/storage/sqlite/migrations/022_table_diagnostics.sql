-- +migrate Up
ALTER TABLE table_stats ADD COLUMN last_autoanalyze DATETIME;
ALTER TABLE table_stats ADD COLUMN heap_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN rel_tuples REAL NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN n_mod_since_analyze INTEGER NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN vacuum_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN autovacuum_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN analyze_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN autoanalyze_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE table_stats ADD COLUMN vacuum_trigger REAL;
ALTER TABLE table_stats ADD COLUMN analyze_trigger REAL;
ALTER TABLE table_stats ADD COLUMN vacuum_scale_factor REAL;

ALTER TABLE current_table_stats ADD COLUMN last_autoanalyze DATETIME;
ALTER TABLE current_table_stats ADD COLUMN heap_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN rel_tuples REAL NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN n_mod_since_analyze INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN vacuum_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN autovacuum_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN analyze_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN autoanalyze_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_table_stats ADD COLUMN vacuum_trigger REAL;
ALTER TABLE current_table_stats ADD COLUMN analyze_trigger REAL;
ALTER TABLE current_table_stats ADD COLUMN vacuum_scale_factor REAL;

ALTER TABLE bloat_stats ADD COLUMN heap_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE bloat_stats ADD COLUMN dead_tuple_bytes INTEGER;
ALTER TABLE bloat_stats ADD COLUMN free_bytes INTEGER;
ALTER TABLE bloat_stats ADD COLUMN measurement_source TEXT NOT NULL DEFAULT '';
ALTER TABLE bloat_stats ADD COLUMN measured_at DATETIME;

ALTER TABLE current_bloat_stats ADD COLUMN heap_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE current_bloat_stats ADD COLUMN dead_tuple_bytes INTEGER;
ALTER TABLE current_bloat_stats ADD COLUMN free_bytes INTEGER;
ALTER TABLE current_bloat_stats ADD COLUMN measurement_source TEXT NOT NULL DEFAULT '';
ALTER TABLE current_bloat_stats ADD COLUMN measured_at DATETIME;

-- +migrate Down
-- SQLite migrations are append-only in production.
