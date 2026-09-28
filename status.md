# Status

Goal: make PGAnalyzer useful for teams without a dedicated DBA. It should diagnose
problems, tell people when something breaks, and warn before an outage, rather
than show numbers that need an expert to read.

Last updated: 2026-09-28. Everything below is committed.

## Done

### 1. Accurate advice (2026-09-27)

- **Slow queries use a recent window.** Queries are judged on their mean time over
  `thresholds.slow_query_window` (default 24h, minimum 1h) rather than the
  lifetime mean. Queries that didn't run in the window aren't flagged. The
  lifetime mean is used until a window's worth of history exists. The suggestion
  shows the window, the lifetime mean, and "Nx slower" for regressions.
  (`internal/analyzer/slow_queries.go`)
- **Unused indexes need evidence.** An index is flagged only once PostgreSQL has
  counted scans for `unused_index_days`. That window starts at
  `pg_stat_database.stats_reset`, or at server start if statistics were never
  reset. Indexes that back foreign keys are skipped, and these findings are
  never critical. The suggestion warns that replica scans aren't visible and
  gives `DROP INDEX CONCURRENTLY` with quoted names.
- **Duplicate indexes are compared by definition.** This replaces a name/size
  heuristic. It compares access method, key columns with opclass, collation and
  sort order, INCLUDE columns, expressions and predicate. It reports exact
  duplicates (warning) and leading-prefix redundancy (info, btree only), and
  never flags unique or PK indexes. Each finding points at an index that is kept.
  (`internal/analyzer/indexes.go`)
- Migration `016_index_structure.sql` adds these columns to `index_stats`.
- The missing-index rule lost a dead severity branch. The README rule table now
  lists all 12 rules.
- Tests: unit tests, plus `tests/integration/index_structure_test.go`, which
  passed on PG13 and PG17.

### 2. Alerts and digest (2026-09-27)

- `internal/notifier/`: Slack incoming webhook and generic JSON webhook channels.
- **Issue alerts:**
  - Sent when an issue reaches `min_severity` (default critical) and again on
    escalation.
  - Reminders every `renotify_after` (24h).
  - Resolved messages only after `resolve_grace` (1h), so flapping stays quiet.
  - Dismissed issues are dropped silently.
  - One message per check.
- **Collection health:** alerts after `collection_stale_after` (15m) without a
  successful collection, then sends a recovery message.
- **Digest:** daily or weekly (default Monday 09:00, server local time). It
  covers active issues by severity, issues new and resolved in the period, and
  the top 5 queries by share of DB time.
- State lives in SQLite (migration `017_notifications.sql`), so restarts don't
  re-send anything.
- `pganalyzer -notify-test` / `task notify:test -- -config <file>` checks the
  channels without connecting to PostgreSQL.
- Verified end to end against PG17 with a local webhook receiver: test message,
  critical alert, collection failure, and recovery.

### 3. Outage-risk checks (2026-09-28)

- New `outage_risk` collector (every 5m, `internal/collector/risk/`) backed by
  `postgres.Client.GetOutageRisk` (`internal/postgres/risk.go`). Each check runs
  on its own, so a missing privilege goes into `Unavailable` and a warning in the
  log instead of failing the collector.
  - Wraparound: per-database `age(datfrozenxid)` and `mxid_age`, the oldest 10
    tables (including TOAST) in the connected DB, and the freeze_max_age settings.
  - xmin holders: sessions, slot `xmin`/`catalog_xmin`, prepared transactions.
  - Replication slots (retained WAL, `wal_status`, `safe_wal_size`) and
    `pg_stat_replication` lag. Safe on a standby.
  - Sequences, limited by the owning column's type (bigint sequence feeding an
    int column).
  - Prepared transactions; database and cluster size.
- Storage (migration `018_outage_risk.sql`):
  - One JSON document per snapshot in `outage_risk`, instead of a table per check.
  - Hourly `size_history`, kept `storage.retention.size_history` (default 90d).
- Analyzer: new `outage_risk` domain with the usual coverage and staleness.
  - Least-squares size trend over 7 days, needing at least 24h of history.
  - Fastest-growing tables from `table_stats` compared against a snapshot at
    least 24h old.
  - Peak connection utilization over 24h.
- 7 new rules, 19 in total:
  - `xid_wraparound` and `multixact_wraparound`: name the blockers and give
    `VACUUM (FREEZE)` commands.
  - `replication_slot`
  - `sequence_exhaustion`: gives the `ALTER ... TYPE bigint` DDL with lock notes.
  - `prepared_transaction`
  - `disk_growth`: needs `thresholds.disk_capacity_gb` for a forecast.
  - `connection_saturation`: recommends a pooler when most connections are idle.
- `main.go` now builds the suggester config from `cfg.Thresholds`
  (`suggester.ConfigFromThresholds`). Before this, the configured thresholds never
  reached the rules. Critical levels are kept at least as strict as a raised
  warning level.
- Tests:
  - Unit tests for the rules, the trend fit and storage.
  - `tests/integration/outage_risk_test.go` passed on PG13 and PG17 (started with
    `-c max_prepared_transactions=10`).
  - End to end: the binary against PG17 sent a critical `sequence_exhaustion`
    alert to a local webhook.
  - A role without extra privileges collects with no failed checks; sequences it
    can't read are counted and skipped.

### 4. Index advisor (2026-09-28)

- `postgres.ExplainGeneric` plans normalized `pg_stat_statements` text (`$1`)
  without values or execution:
  - prepares the query, then runs `EXPLAIN EXECUTE` with NULLs under
    `force_generic_plan`
  - read-only transaction with a 2s statement timeout and a 100ms lock timeout
  - prepared statements and hypopg indexes are session state, so they are removed
    explicitly and the connection is closed if that fails. Verified no leaks.
- `internal/plans`, a pure package:
  - plan walker
  - filter parser: equality columns first, one range column; OR, functions and
    patterns give no proposal
  - DDL helpers
  - plain-language `Describe`
- `query_plans` collector, every 1h:
  - top `index_advisor.max_queries` queries by time over 24h
  - reuses plans for 24h, but re-checks existing indexes every run
  - skips small tables, filters matching over 20% of rows, and columns already
    covered by an index
  - validates with hypopg when installed
  - saves plans to `explain_plans` for the query page
  - fails at startup until query stats exist, so it is retried the next cycle
  - Migration `019_query_plans.sql`. `explain_plans` is now purged with snapshot
    retention.
- New rule `index_recommendation` (20 rules):
  - `CREATE INDEX CONCURRENTLY` DDL, the queries it serves with links, hypopg
    cost before and after, estimated size, INVALID-index check, and rollback
  - warning when the queries hold at least 5% of DB time
- `missing_index` defers to it for tables with a recommendation. It dropped the
  placeholder DDL and now explains why busy queries' scans got no proposal.
- Query page: plain-language plan summary; "Generate" without values uses the
  generic plan instead of failing on `$1`.
- Fixes:
  - the suggestion page kept no line breaks, so markdown descriptions ran together
    (now `pre-wrap`)
  - the suggestions API decoded metadata through float64, corrupting 64-bit query
    ids (now passed through raw)
- Tests:
  - unit tests for the parser, describe, collector (fake planner, real SQLite),
    grouping and rules
  - `tests/integration/index_advice_test.go` passed on PG14 and PG17 with hypopg:
    - proposes `(status, created_at)`
    - UPDATE is not executed
    - no real index is built
    - the proposed DDL runs
    - the proposal retires once the index exists
  - End to end on PG17: a recommendation with hypopg "5k → 213" appeared 41s after
    start, and both pages rendered.

### 5. Configuration review (2026-09-28)

- `postgres.GetServerSettings` (`internal/postgres/settings.go`) uses the same
  independent-check pattern as outage risk:
  - `settings`: about 20 reviewed `pg_settings` rows, plus any row with
    `pending_restart`
  - `stat_statements`: entries, `pg_stat_statements.max`, and `dealloc` and
    `stats_reset` from `pg_stat_statements_info`
  - `autovacuum_disabled`: tables with `autovacuum_enabled=false`, largest 20
  - All three work for a role without extra privileges.
- `server_settings` collector, every 1h (`internal/collector/settings/`). Stored
  as one JSON document per snapshot (migration `020_server_settings.sql`) and
  purged with snapshot retention.
- Analyzer: new `server_settings` domain. It counts pg_stat_statements evictions
  over 24h against a baseline snapshot and ignores a reset in between.
- `models.Setting` parses units (`8kB`, `ms`, ...) into bytes or a duration and
  renders values as PostgreSQL does.
- 2 new rules, 22 in total:
  - `configuration`: one finding per target, so each resolves on its own.
    - autovacuum or `track_counts` off (critical)
    - tables with autovacuum disabled (critical over 1GB)
    - `idle_in_transaction_session_timeout` = 0 (warning while idle sessions
      exist, otherwise info)
    - `statement_timeout` = 0 (info, recommends setting it per role)
    - one grouped "diagnostics off" finding for `log_min_duration_statement`,
      `log_lock_waits`, `log_temp_files` and `track_io_timing`
    - `shared_buffers` still at 128MB on a database over 2GB; with
      `thresholds.server_memory_gb`, anything outside 15–40% of RAM
    - `work_mem` × `max_connections` + `shared_buffers` over RAM
    - `random_page_cost` ≥ 4
    - settings waiting for a restart
    - Each finding gives `ALTER SYSTEM` + reload, or says a restart is needed,
      with a note for managed Postgres.
  - `stat_statements_capacity`: warning at 90% full, on evictions in the last
    24h, or when `track = none`.
  - A failed check never resolves the findings built from it (see section 7).
  - `high_temp_usage` now shows the current `work_mem`.
- New optional `thresholds.server_memory_gb`.
- Tests:
  - unit tests for unit parsing, storage round trip and purge, eviction
    baseline, and the rules
  - `tests/integration/settings_test.go` passed on PG14 and PG17 (started with
    `-c pg_stat_statements.max=100`). It covers evictions, a table with
    autovacuum off, `ALTER SYSTEM shared_buffers` showing as pending restart,
    and an unprivileged role with no failed checks.
  - Full integration suite still passes on both versions.
  - End to end on PG17 with `ALTER SYSTEM SET autovacuum = off`: a critical
    "Autovacuum is disabled" webhook alert arrived about 60s after start, and the
    suggestion page rendered.

### 6. Fix verification (2026-09-28)

- **`resolved_at` on suggestions** (migration `021_suggestion_resolved_at.sql`):
  - Set when a suggestion resolves. Resolving again keeps the first time, and
    the issue coming back clears it.
  - Backfilled from `last_seen_at` for rows resolved earlier.
  - The migration also strips the Go monotonic-clock suffix (`m=+…`) from
    suggestion timestamps, which 015 missed. Suggestion writes now use
    `Round(0)`.
  - Exposed in the suggestions API and on the suggestion page.
  - The digest now counts resolved issues by `resolved_at`.
- **`internal/verify`** compares query history before and after a resolved
  `slow_query` or `index_recommendation` suggestion:
  - Means come from deltas between cumulative `query_history` samples, so a
    `pg_stat_statements` reset counts from zero instead of going negative.
  - Each side covers up to `slow_query_window`. For `slow_query`, the "before"
    period ends a window earlier, because the rule resolves only once the
    trailing mean drops, up to a window after the fix.
  - Verdicts: improved (≤0.8×), regressed (≥1.2×), unchanged, stopped (no calls
    over a full window after), pending (under 1h or 10 calls after), no_data.
  - Query ids are decoded with `UseNumber`, so 64-bit ids stay exact.
- Shown on the suggestion page as a "Fix verification" card, per query with
  links, using only CSS classes already compiled. In the digest, resolved items
  carry an `outcome` ("3.2s → 40ms (99% faster)"); pending and no-data verdicts
  are left out.
- Tests:
  - unit tests for verify: verdicts, the slow-query baseline shift, counter
    reset, multi-query summary, 64-bit ids
  - storage tests for the `resolved_at` lifecycle and the 021 backfill and
    suffix strip
  - verifier run on real SQLite `query_history` rows
  - page rendering and digest outcome
  - End to end: the new binary migrated a database from the previous build and
    resolved the autovacuum finding once autovacuum was back on. The page showed
    the resolution time.
- Also fixed: the suggestions API labelled local times with `Z`; it now
  converts to UTC first.

### 7. Failed checks no longer resolve findings (2026-09-28)

- Bug: when one outage-risk check failed (for example, `replication_slots`
  after the role lost `pg_monitor`), its rule saw empty data and resolved live
  findings, sending "resolved" alerts.
- Fix: new optional `suggester.PartiallyObserved` interface. A rule returns the
  target prefixes it couldn't observe, and the suggester leaves those existing
  suggestions unresolved. Findings from checks that succeeded still resolve
  normally.
- Implemented by:
  - wraparound (`database:`)
  - replication_slot (`slot:`, `replica:`)
  - sequence_exhaustion (`sequence:`)
  - prepared_transaction (`prepared:`)
  - disk_growth (`instance:disk`)
  - configuration: `setting:`, `autovacuum_disabled:`, and
    `setting:shared_buffers` when outage-risk data is stale and RAM is unknown
  - stat_statements_capacity
- This replaces the configuration rules' "return an error" guard, which
  dropped every finding of the rule for the cycle and logged an error each
  time.
- Tests: rule prefix mapping, and a suggester test where a failed slot check
  keeps `slot:old` active while a fixed sequence resolves. The suggester test
  fails with the guard removed.

## Known gaps in what's done

- The dashboard and query pages still use lifetime means and a hard-coded 1000 ms
  (`internal/api/handlers/pages.go:134`, `dashboard.go:95`).
- Per-query cache analysis (`internal/analyzer/cache.go`) uses lifetime counters.
- "Bloat" is the dead-tuple ratio, not an estimate of reclaimable space.
- Alerts stop if the pganalyzer process dies. The README recommends an uptime
  monitor on `/health`.
- Fix verification:
  - It covers only query-bound rules. Other fixes, such as dropping an unused
    index or turning autovacuum on, aren't measured.
  - The fix time is inferred from the resolution, not recorded. For
    `slow_query` it can be up to a window off, which the shifted baseline
    absorbs. An unrelated change in the same window is attributed to the fix.
  - A query that changes shape after the fix (a new queryid) looks "stopped".
  - It is computed on each page view and digest, not stored. A verdict can
    change until the "after" window completes.
  - The immediate "resolved" alert has no outcome, because there is no "after"
    data yet.
- A persistently failing single collector (e.g. a missing permission) counts as
  a collection failure and alerts. This is intended, but it can surprise people.
- `golangci-lint` isn't installed locally, so `task lint` hasn't been run.
- Outage risk:
  - Per-table wraparound ages and sequences cover only the connected database.
    Other databases get only a database-level age and a SQL snippet.
  - Disk forecast counts database files only, not WAL, logs or temp files, and
    needs `disk_capacity_gb`. Free space isn't visible from SQL.
  - `SequencesUnreadable` is collected but not shown anywhere yet.
  - The new signals appear only as suggestions. There is no dashboard panel yet.
- Index advisor:
  - Only btree proposals from scan filters. Join keys, ORDER BY, expression and
    partial indexes are not proposed.
  - Queries recorded as SQL-level `PREPARE name AS ...` are skipped. Protocol-level
    prepared statements, which drivers use, are fine.
  - Suggestion descriptions are markdown shown as preformatted text; a renderer
    would make code blocks and links clickable.
- Configuration review:
  - RAM isn't visible from SQL. Without `server_memory_gb`, only the untuned
    128MB `shared_buffers` default is flagged, and `work_mem` isn't checked.
  - Autovacuum tuning (scale factors, cost limits, worker count) is collected but
    not judged yet. Per-table bloat and vacuum rules cover the effects.
  - A default install gets about 4 info findings (timeouts, diagnostics,
    `random_page_cost`). This is intended but can look noisy.
- The repo-root `migrations/sqlite/` is a stale 001–007 copy. The real
  migrations are in `internal/storage/sqlite/migrations/`, and only old
  `tasks/*.md` files reference the copy.

## Next steps (in order)

1. **Setup checks and safe defaults.**
   - Refuse to start with `admin/admin` while auth is enabled (breaking, `feat!:`).
   - Add `postgres.CheckSetup`: version, `pg_stat_statements` preloaded and
     created, `pg_monitor` or equivalent grants, `track_io_timing`, hypopg
     (optional). Each check comes with its fix.
   - History readiness ("windowed advice ready in ~N hours").
   - Show it all through `pganalyzer -check` / `task check`, startup logs, a
     `/setup` page with a dashboard banner, and a `setup` field on `/health`.
2. **Setup docs**: `docs/postgresql-setup.md` (fixes the broken link in
   `configs/config.example.yaml`), with RDS/Aurora, Cloud SQL and Supabase
   sections.
3. **Dashboard catches up with the analyzer.**
   - Windowed slow-query stats and the configured threshold (see gaps).
   - An outage-risk panel, including `SequencesUnreadable`.
   - Rendered suggestion markdown.
4. **Housekeeping.**
   - Delete the stale root `migrations/sqlite/`.
   - Run golangci-lint.
   - Add collector subpackage tests.
   - Merge `feat/config-review` into `main`.
5. **Releases.**
   - CI: unit, lint, and integration tests on PG14/PG17.
   - Goreleaser and a multi-arch image (the Dockerfile hard-codes amd64).
   - A Helm chart.

Deferred: **multi-database support** (a list of targets in config instead of
one instance per process, `cmd/pganalyzer/main.go:157`).

## Dev notes

- Integration tests run against throwaway podman containers:
  ```bash
  podman run -d --rm --name pgk-it-17 -p 15417:5432 -e POSTGRES_PASSWORD=postgres \
    -e POSTGRES_DB=testdb docker.io/library/postgres:17 -c shared_preload_libraries=pg_stat_statements \
    -c max_prepared_transactions=10
  podman exec pgk-it-17 psql -U postgres -d testdb -c 'CREATE EXTENSION pg_stat_statements'
  POSTGRES_HOST=localhost POSTGRES_PORT=15417 POSTGRES_USER=postgres POSTGRES_PASSWORD=postgres \
    POSTGRES_DATABASE=testdb go test -count=1 -tags=integration ./tests/integration/...
  ```
- Minimum supported server is PostgreSQL 14 (raised from 13 on 2026-09-28), so
  catalog SQL must work there (e.g. `indnkeyatts` is fine; `last_idx_scan` from
  PG16 is not). Run integration tests on PG14 and the newest release.
