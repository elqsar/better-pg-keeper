-- +migrate Up
-- Collection now aggregates pg_stat_statements by queryid, so a snapshot holds at
-- most one row per queryid. Enforce that invariant: GetQueryStatsDelta joins two
-- snapshots on queryid alone, and duplicate rows would multiply the result set.
-- Drop any pre-existing duplicates first, keeping the row with the largest
-- cumulative total_exec_time (the closest stand-in for the aggregate).
DELETE FROM query_stats
WHERE id NOT IN (
    SELECT id FROM (
        SELECT id,
               ROW_NUMBER() OVER (
                   PARTITION BY snapshot_id, queryid
                   ORDER BY total_exec_time DESC, id ASC
               ) AS rn
        FROM query_stats
    )
    WHERE rn = 1
);

CREATE UNIQUE INDEX idx_query_stats_snapshot_queryid
    ON query_stats(snapshot_id, queryid);

-- +migrate Down
DROP INDEX IF EXISTS idx_query_stats_snapshot_queryid;
