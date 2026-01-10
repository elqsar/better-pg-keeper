-- +migrate Up
-- Generated suggestions/recommendations
CREATE TABLE suggestions (
    id              INTEGER PRIMARY KEY,
    instance_id     INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    rule_id         TEXT NOT NULL,
    severity        TEXT NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    target_object   TEXT,
    metadata        TEXT,
    status          TEXT DEFAULT 'active',
    first_seen_at   DATETIME NOT NULL,
    last_seen_at    DATETIME NOT NULL,
    dismissed_at    DATETIME,
    UNIQUE(instance_id, rule_id, target_object)
);

CREATE INDEX idx_suggestions_status ON suggestions(instance_id, status);

-- +migrate Down
DROP INDEX IF EXISTS idx_suggestions_status;
DROP TABLE IF EXISTS suggestions;
