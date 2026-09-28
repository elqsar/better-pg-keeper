-- +migrate Up
-- What each issue was last alerted as, so alerts are not repeated every analysis
-- cycle and an issue that briefly disappears is not announced as resolved.
CREATE TABLE notification_state (
    instance_id   INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    rule_id       TEXT NOT NULL,
    target_object TEXT NOT NULL,
    severity      TEXT NOT NULL,
    notified_at   DATETIME NOT NULL,
    -- Set when the issue stopped being alertable; cleared if it comes back.
    cleared_at    DATETIME,
    PRIMARY KEY (instance_id, rule_id, target_object)
);

-- Small per-instance values for the notifier, such as when the last digest went out.
CREATE TABLE notifier_meta (
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (instance_id, key)
);

-- +migrate Down
DROP TABLE IF EXISTS notifier_meta;
DROP TABLE IF EXISTS notification_state;
