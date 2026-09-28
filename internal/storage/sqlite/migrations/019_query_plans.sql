-- +migrate Up
-- Generic plans of the busiest queries and the index proposals derived from
-- them, as one JSON document per snapshot (see models.QueryPlanReport).
CREATE TABLE query_plans (
    snapshot_id INTEGER PRIMARY KEY REFERENCES snapshots(id) ON DELETE CASCADE,
    payload     TEXT NOT NULL
);

-- explain_plans had no cleanup; plans are now also captured automatically.
CREATE INDEX IF NOT EXISTS idx_explain_plans_captured_at ON explain_plans(captured_at);

-- +migrate Down
DROP INDEX IF EXISTS idx_explain_plans_captured_at;
DROP TABLE IF EXISTS query_plans;
