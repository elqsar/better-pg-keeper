-- +migrate Up
-- Older Go time values were serialized with a process-local monotonic suffix.
-- Remove it so a timestamp read back from SQLite compares equal to the row.
UPDATE snapshots
SET captured_at = substr(captured_at, 1, instr(captured_at, ' m=') - 1)
WHERE instr(captured_at, ' m=') > 0;
UPDATE snapshot_collectors
SET collected_at = substr(collected_at, 1, instr(collected_at, ' m=') - 1)
WHERE instr(collected_at, ' m=') > 0;
UPDATE query_history
SET sampled_at = substr(sampled_at, 1, instr(sampled_at, ' m=') - 1)
WHERE instr(sampled_at, ' m=') > 0;

-- +migrate Down
-- Monotonic readings cannot be reconstructed from wall-clock timestamps.
