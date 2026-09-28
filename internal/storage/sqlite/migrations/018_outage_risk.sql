-- +migrate Up
-- Outage-risk signals (wraparound, replication slots, sequences, prepared
-- transactions, size) as one JSON document per snapshot. The analyzer always
-- reads the whole document and nothing filters on its fields.
CREATE TABLE outage_risk (
    snapshot_id INTEGER PRIMARY KEY REFERENCES snapshots(id) ON DELETE CASCADE,
    payload     TEXT NOT NULL
);

-- Database size over time, kept longer than snapshots so disk growth can be
-- forecast. At most one row per instance per hour.
CREATE TABLE size_history (
    instance_id         INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    hour_unix           INTEGER NOT NULL, -- start of the hour, unix seconds
    captured_at_unix_ns INTEGER NOT NULL,
    cluster_bytes       INTEGER NOT NULL,
    PRIMARY KEY (instance_id, hour_unix)
);

-- +migrate Down
DROP TABLE IF EXISTS size_history;
DROP TABLE IF EXISTS outage_risk;
