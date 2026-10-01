# Status

Goal: make PGAnalyzer useful for teams without a dedicated DBA. It should diagnose
problems, tell people when something breaks, and warn before an outage, rather
than show numbers that need an expert to read.

Last updated: 2026-09-29. Everything below is committed.

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

### 8. Setup checks and safe defaults (2026-09-28)

- **Breaking: no default dashboard password.** With auth enabled,
  PGAnalyzer refuses to start without a password or with `admin`/`admin`. The
  message says to set `SERVER_PASSWORD` or `server.auth.enabled: false`. The
  example config no longer defaults the password.
- **`postgres.CheckSetup`** (`internal/postgres/setup.go`). Each check is
  independent and comes with its fix:
  - version ≥ 14
  - `pg_stat_statements` created in the database and loaded. This is detected
    by reading the view, which works for any role, unlike reading
    `shared_preload_libraries`.
  - `pg_monitor` (or `pg_read_all_stats` + `pg_read_all_settings`), else a
    warning with `GRANT pg_monitor TO ...`
  - hypopg: info only, and left out when the index advisor is off
  - It also returns when index statistics started counting.
- **`internal/setup`** adds history readiness from storage:
  - slow-query window
  - `unused_index_days` since the statistics reset
  - 24h of size history for the disk forecast
  - Each item says "ready in ~N hours/days".
  - Reports are cached for 10 minutes; `Refresh` bypasses the cache.
  - `track_io_timing` and `pg_stat_statements.max` are left to the
    configuration rules rather than repeated here.
- **Where it shows:**
  - `pganalyzer -check` / `task check -- -config <file>`: prints a checklist
    and exits non-zero on a failure. It reads history only if the storage file
    already exists.
  - startup logs: warn/fail once, pending as info
  - `/setup` page, with "Check again" (`?refresh=1`)
  - a dashboard banner: amber when something failed or warned, blue while only
    history is pending
  - a `setup` count on `/health`, which doesn't change `status`
- `task check` rebuilt `static/style.css` through the `css` dependency, so it
  now includes the classes used by the new templates.
- Tests:
  - unit tests for readiness math, caching, connection failure, credential
    validation, banner and page rendering
  - `tests/integration/setup_test.go` passed on PG14 and PG17: superuser all
    OK, a plain role warns with the GRANT fix, a database without the
    extension fails with `CREATE EXTENSION`
  - End to end on PG17 without preload:
    - `task check` printed the preload fix and exited non-zero
    - startup logged it
    - `/health` showed `failed: 1`
    - the dashboard showed the amber banner
    - after restarting Postgres with the library preloaded, "Check again" showed
      OK and the banner switched to "still collecting history"
    - `admin/admin` was refused at startup

### 9. Setup docs (2026-09-28)

- `docs/postgresql-setup.md`, a how-to guide. It fixes the broken link in
  `configs/config.example.yaml` and is linked from the README Quick Start. It
  covers:
  - requirements
  - self-managed setup
  - RDS/Aurora (parameter groups, `rds_superuser`)
  - Cloud SQL (flags, Auth Proxy)
  - Supabase (the `extensions` schema, direct vs session pooler)
  - why transaction-mode poolers don't work
  - a troubleshooting table keyed on setup-check messages
- The optional grants are spelled out: `pg_read_all_data` or schema `SELECT`
  enables index advice and sequence checks. Without them those queries are
  skipped.
- Found while testing the guide: with the extension in a schema the role
  doesn't search (as on Supabase), the setup check failed without a fix. It
  now names the schema and prints the `ALTER ROLE … SET search_path` fix.
  Covered by a new case in `tests/integration/setup_test.go`.
- The self-managed steps were verified on PG14: role created as documented,
  `-check` all OK after the `search_path` fix. The managed-service sections
  aren't verified against live services; each points to `-check` to confirm.

### 10. Dashboard catches up with the analyzer (2026-09-28)

- **Windowed query figures.** The dashboard page and `/api/v1/dashboard` use
  `analyzer.RecentQueryStats`: the last `slow_query_window`, against the same
  baseline snapshot as the slow-query analyzer, and the configured
  `slow_query_ms`.
  - Before a full window of history exists they show lifetime figures, and the
    page labels which one it shows.
  - The API adds `query_window_seconds` and `slow_query_ms`.
  - The queries page's slow filter uses the configured threshold.
- **Outage-risk panel** on the dashboard:
  - wraparound as a share of 2^31
  - replication slots, retained WAL and replay lag
  - the top sequence, sequences over 50%, and `SequencesUnreadable`
  - open prepared transactions
  - size, growth and "full in ~N days" (same fit and forecast as
    `disk_growth`)
  - checks that could not run
  - Each row shows its rule's active suggestion count rather than new
    thresholds.
- **Rendered markdown** in suggestion descriptions, on the detail page and the
  list, with goldmark:
  - Raw HTML is dropped and unsafe link schemes aren't rendered, because
    descriptions include text from the monitored database. Tests cover
    `<script>`, `<img onerror>` and `javascript:` links.
  - The `.markdown` styles are in `tailwind/input.css`.
- Fixed while checking the UI in a browser: Tailwind only scanned templates,
  so class names returned by Go helpers (`badge-critical`,
  `suggestion-card-*`, `cache-*`) were purged. Critical badges showed as plain
  text. `internal/web/*.go` is now in Tailwind's `content`.
- `go mod tidy`: goldmark added, prometheus marked direct, and the unused
  `rogpeppe/go-internal` dropped.
- Verified in the browser against PG17 with a 93% sequence: the history
  banner, the panel with "1 active" on sequences, the rendered suggestion
  page, and the list with severity styling.

### 11. Housekeeping and CI (2026-09-29)

- **The setup guide was never committed.** `.gitignore` ignored `docs/`, so
  `docs/postgresql-setup.md` (section 9) existed only locally, and the README and
  example-config links were broken on GitHub. `docs/` is now tracked.
- Deleted the stale root `migrations/sqlite/` and the merged
  `feat/config-review` branch (merged as PR #1).
- **Taskfile:**
  - `silent: true`, and `default` runs `task --list`.
  - The `docker:*` tasks became `image:*` (podman) and `compose:*`
    (podman-compose).
  - `css` also rebuilds when `internal/web/*.go` changes.
- **`task test:integration:pg -- <14|17>`** (`scripts/integration-pg.sh`)
  replaces `test:integration:docker`. It starts a throwaway `pgk-ci-<ver>`
  container on port `155<ver>` with the settings the tests check for and
  hypopg installed, runs the suite, and removes the container.
  `CONTAINER_ENGINE=docker` selects docker, which CI uses.
- **Lint:** `.golangci.yml` (v2) with the standard linters plus `bodyclose`,
  `errorlint`, `misspell`, `unparam`, and gofmt/goimports. golangci-lint is
  pinned to v2.14.0 in `install:tools` and CI. Fixes:
  - `==` comparisons against sentinel errors changed to `errors.Is`/`errors.As`.
    None was wrapped in practice.
  - echo's deprecated `LoggerWithConfig` replaced with `RequestLoggerWithConfig`,
    in the same format.
  - Scheduler `Stop` and error-response write errors are now logged.
  - Unchecked setup calls in storage tests.
  - Unused test helpers removed.
  - Excluded on purpose: unchecked `Close`/`Fprintf` (the `std-error-handling`
    preset) and `Tx.Rollback` on error paths.
- **Collector tests** for `activity`, `locks`, `risk` and `settings`. They cover
  saving to real SQLite (historical and current tables, size history), logging
  unavailable checks, and error propagation. `internal/collector/collectortest`
  holds the shared storage setup.
- **CI** (`.github/workflows/ci.yml`, push to main and PRs):
  - unit: build, vet, `test -race`
  - lint
  - CSS drift: rebuilds `style.css` and fails if it differs
  - integration on PG14 and PG17
- Verified locally: lint 0 issues, unit tests pass, `task
  test:integration:pg` passes on PG14 and PG17 with no prerequisite skips,
  actionlint clean, and the committed CSS matches a fresh build. CI hasn't run
  on GitHub yet.

## Known gaps in what's done

- The queries list (`/queries`) still sorts and shows lifetime
  `pg_stat_statements` figures; only its slow filter uses the configured
  threshold.
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
- Outage risk:
  - Per-table wraparound ages and sequences cover only the connected database.
    Other databases get only a database-level age and a SQL snippet.
  - Disk forecast counts database files only, not WAL, logs or temp files, and
    needs `disk_capacity_gb`. Free space isn't visible from SQL.
- Index advisor:
  - Only btree proposals from scan filters. Join keys, ORDER BY, expression and
    partial indexes are not proposed.
  - Queries recorded as SQL-level `PREPARE name AS ...` are skipped. Protocol-level
    prepared statements, which drivers use, are fine.
- Configuration review:
  - RAM isn't visible from SQL. Without `server_memory_gb`, only the untuned
    128MB `shared_buffers` default is flagged, and `work_mem` isn't checked.
  - Autovacuum tuning (scale factors, cost limits, worker count) is collected but
    not judged yet. Per-table bloat and vacuum rules cover the effects.
  - A default install gets about 4 info findings (timeouts, diagnostics,
    `random_page_cost`). This is intended but can look noisy.

## Next steps (in order)

1. **Releases.**
   - Goreleaser for binaries and a changelog from tags.
   - A multi-arch image (linux/amd64 and arm64): drop the hard-coded
     `GOARCH=amd64` in `Dockerfile`. Registry not chosen yet.
2. **A Helm chart.**

Deferred: **multi-database support** (a list of targets in config instead of
one instance per process, `cmd/pganalyzer/main.go:157`).

## Dev notes

- Integration tests: `task test:integration:pg -- 14` (or `17`) runs the whole
  suite against a throwaway podman container (`scripts/integration-pg.sh`).
  To use your own server, set `POSTGRES_HOST`, `POSTGRES_PORT`,
  `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DATABASE` and run
  `go test -count=1 -tags=integration ./tests/integration/...`.
- Minimum supported server is PostgreSQL 14 (raised from 13 on 2026-09-28), so
  catalog SQL must work there (e.g. `indnkeyatts` is fine; `last_idx_scan` from
  PG16 is not). Run integration tests on PG14 and the newest release.
