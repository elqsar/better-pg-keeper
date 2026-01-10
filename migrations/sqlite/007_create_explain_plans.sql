-- +migrate Up
-- Cached EXPLAIN plans
CREATE TABLE explain_plans (
    id              INTEGER PRIMARY KEY,
    queryid         INTEGER NOT NULL,
    plan_text       TEXT NOT NULL,
    plan_json       TEXT,
    captured_at     DATETIME NOT NULL,
    execution_time  REAL
);

CREATE INDEX idx_explain_plans_queryid ON explain_plans(queryid);

-- +migrate Down
DROP INDEX IF EXISTS idx_explain_plans_queryid;
DROP TABLE IF EXISTS explain_plans;
