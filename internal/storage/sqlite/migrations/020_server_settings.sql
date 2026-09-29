-- +migrate Up
-- Server configuration for review (pg_settings rows, pg_stat_statements
-- capacity, tables with autovacuum disabled) as one JSON document per
-- snapshot, like outage_risk.
CREATE TABLE server_settings (
    snapshot_id INTEGER PRIMARY KEY REFERENCES snapshots(id) ON DELETE CASCADE,
    payload     TEXT NOT NULL
);

-- +migrate Down
DROP TABLE IF EXISTS server_settings;
