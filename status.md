# Status

Goal: make PGAnalyzer useful for teams without a dedicated DBA. It should diagnose
problems, tell people when something breaks, and warn before an outage, rather
than show numbers that need an expert to read.

Last updated: 2026-09-28. Everything below is committed on `main`.

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

## Known gaps in what's done

- The dashboard and query pages still use lifetime means and a hard-coded 1000 ms
  (`internal/api/handlers/pages.go:134`, `dashboard.go:95`).
- Per-query cache analysis (`internal/analyzer/cache.go`) uses lifetime counters.
- "Bloat" is the dead-tuple ratio, not an estimate of reclaimable space.
- Alerts stop if the pganalyzer process dies. The README recommends an uptime
  monitor on `/health`.
- The digest approximates resolution time with `last_seen_at`, because no
  `resolved_at` is stored.
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

## Next steps (in order)

1. **Configuration review.** Check `pg_settings`: `shared_buffers`, `work_mem`,
   `random_page_cost`, autovacuum settings, `statement_timeout`,
   `idle_in_transaction_session_timeout`, `log_min_duration_statement`, and a
   too-small `pg_stat_statements.max`.
2. **Fix verification.** After a suggestion resolves or an index is added,
   compare query history before and after.
3. **Multi-database support.** Accept a list of targets in config instead of one
   instance per process (`cmd/pganalyzer/main.go:157`).
4. **Setup and onboarding.**
   - Refuse to start with the default `admin/admin` unless auth is explicitly off.
   - Add a first-run check for `pg_stat_statements`, grants, and how much
     history has been collected.
   - Write setup docs for managed Postgres (RDS/Aurora, Cloud SQL, Supabase).
   - Publish releases, a container image, and a Helm chart.
5. **Housekeeping.**
   - Switch the dashboard to windowed query stats (see gaps).
   - Store `resolved_at` on suggestions.
   - Add tests for the collector subpackages.
   - `configs/config.example.yaml` references a missing `docs/postgresql-setup.md`.
   - Add a dashboard panel for outage risk: wraparound %, slots, sequences, disk forecast.
   - Render suggestion markdown (code blocks, links) on the suggestion page.

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
