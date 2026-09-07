-- +migrate Up
-- Records which collectors contributed to each snapshot.
-- Collectors run on different intervals (30s to 1h) while snapshots are cut about
-- once a minute, so a snapshot only ever holds data from the collectors that were
-- due at that moment. Without this table there is no way to tell "this domain was
-- observed and is clean" apart from "this domain was never collected", which made
-- analysis resolve suggestions whose backing data was simply absent.
CREATE TABLE snapshot_collectors (
    snapshot_id  INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    collector    TEXT NOT NULL,
    status       TEXT NOT NULL,          -- 'success' | 'error'
    collected_at DATETIME NOT NULL,
    error        TEXT,
    PRIMARY KEY (snapshot_id, collector)
);

CREATE INDEX idx_snapshot_collectors_lookup
    ON snapshot_collectors(collector, status, snapshot_id DESC);

-- +migrate Down
DROP INDEX IF EXISTS idx_snapshot_collectors_lookup;
DROP TABLE IF EXISTS snapshot_collectors;
