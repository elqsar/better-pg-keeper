-- +migrate Up
-- When a suggestion was last resolved, so resolution can be reported and fixes
-- verified against query history. Cleared when the issue comes back.
ALTER TABLE suggestions ADD COLUMN resolved_at DATETIME;
-- Suggestion timestamps were written with Go's monotonic-clock suffix, which
-- 015 stripped from other tables; strip it here before copying last_seen_at.
UPDATE suggestions
SET first_seen_at = substr(first_seen_at, 1, instr(first_seen_at, ' m=') - 1)
WHERE instr(first_seen_at, ' m=') > 0;
UPDATE suggestions
SET last_seen_at = substr(last_seen_at, 1, instr(last_seen_at, ' m=') - 1)
WHERE instr(last_seen_at, ' m=') > 0;
UPDATE suggestions
SET dismissed_at = substr(dismissed_at, 1, instr(dismissed_at, ' m=') - 1)
WHERE instr(dismissed_at, ' m=') > 0;
-- Earlier resolutions were not timed; the last analysis that still saw the
-- issue is the closest record of when it went away.
UPDATE suggestions SET resolved_at = last_seen_at WHERE status = 'resolved';

-- +migrate Down
ALTER TABLE suggestions DROP COLUMN resolved_at;
