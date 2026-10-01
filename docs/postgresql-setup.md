# Setting up PostgreSQL for PGAnalyzer

This guide prepares a PostgreSQL server for PGAnalyzer: the
`pg_stat_statements` extension, a dedicated monitoring role, and a few
optional extras. It covers self-managed servers, then Amazon RDS and Aurora,
Google Cloud SQL, and Supabase.

Every section ends the same way: run the setup check and fix what it reports.

```bash
task check -- -config configs/config.yaml
# or: ./bin/pganalyzer -check -config configs/config.yaml
```

The check prints each item with its fix and exits non-zero while something
fails. The same list is on the `/setup` page once PGAnalyzer is running.

## What PGAnalyzer needs

| Requirement | Why | Required |
|---|---|---|
| PostgreSQL 14 or later | Catalog views and `pg_stat_statements_info` | Yes |
| `pg_stat_statements` loaded at server start | Query statistics: slow queries, top queries, index advice | Yes |
| `pg_stat_statements` extension created in the monitored database | The view PGAnalyzer reads | Yes |
| A role with `pg_monitor` | Other roles' query texts, session details, replication and wraparound checks | Strongly recommended |
| `SELECT` on application tables | The index advisor plans the busiest queries with `EXPLAIN` (never `ANALYZE`) | For index advice |
| `hypopg` extension | Checks index proposals against hypothetical indexes | Optional |
| `track_io_timing = on` | Separates slow I/O from slow CPU per query | Optional |

PGAnalyzer never writes to your database. It reads statistics and catalogs,
and for index advice runs `EXPLAIN` on normalized queries inside a read-only
transaction with a 2s statement timeout.

## Self-managed PostgreSQL

### 1. Load pg_stat_statements

In `postgresql.conf`, add `pg_stat_statements` to `shared_preload_libraries`,
keeping any libraries already listed:

```ini
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.max = 10000
```

Or, as a superuser:

```sql
SHOW shared_preload_libraries;  -- note what is already there
ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';  -- append to the existing list
ALTER SYSTEM SET pg_stat_statements.max = 10000;
```

Restart the server. A reload is not enough for either setting.

`pg_stat_statements.max` defaults to 5000. When it fills, PostgreSQL drops the
least-used statements and their history. PGAnalyzer warns about this
(`stat_statements_capacity`), and 10000 leaves headroom for most applications.

### 2. Create the extension

Connect to the database PGAnalyzer will monitor (the `postgres.database`
setting) and run:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

The extension is per database. The statistics it shows cover the whole
server, but the view has to exist in the database PGAnalyzer connects to.

### 3. Create a monitoring role

```sql
CREATE ROLE pganalyzer LOGIN PASSWORD 'choose-a-password';
GRANT pg_monitor TO pganalyzer;
GRANT CONNECT ON DATABASE mydb TO pganalyzer;
```

`pg_monitor` bundles `pg_read_all_stats`, `pg_read_all_settings` and
`pg_stat_scan_tables`. Without it PGAnalyzer still runs, but:
- other roles' queries show as `<insufficient privilege>`
- sessions holding back vacuum can't be named
- some outage-risk checks are unavailable

The setup check warns in that case.

For index advice, the role also needs to plan the application's queries, which
requires `SELECT` on the tables they use. Pick one:

```sql
-- Everything, PostgreSQL 14+:
GRANT pg_read_all_data TO pganalyzer;

-- Or only the application schema:
GRANT USAGE ON SCHEMA app TO pganalyzer;
GRANT SELECT ON ALL TABLES IN SCHEMA app TO pganalyzer;
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT SELECT ON TABLES TO pganalyzer;
```

Either grant lets the role read your data, even though PGAnalyzer only runs
`EXPLAIN`. Without it, the index advisor skips the queries it can't plan and
everything else works. The same privilege lets the sequence-exhaustion check
read sequence values. Sequences it can't read are counted and skipped.

### 4. Optional: hypopg and I/O timing

```sql
CREATE EXTENSION IF NOT EXISTS hypopg;   -- if the package is installed (e.g. postgresql-17-hypopg)
ALTER SYSTEM SET track_io_timing = on;
SELECT pg_reload_conf();
```

With hypopg, index proposals show the planner's cost before and after, e.g.
"5k → 213". `track_io_timing` has a small overhead on some virtual machines;
`pg_test_timing` measures it.

### 5. Point PGAnalyzer at the server

```yaml
postgres:
  host: db.internal
  port: 5432
  database: mydb
  user: pganalyzer
  password: ${POSTGRES_PASSWORD}
  sslmode: require
```

Then run the setup check.

## Amazon RDS and Aurora PostgreSQL

On RDS you can't use `ALTER SYSTEM` or edit `postgresql.conf`. Server
settings live in a **DB parameter group**, or a **DB cluster parameter group**
for Aurora, and the master user has `rds_superuser` rather than superuser.

1. **Preload the library.** The default parameter groups usually include
   `pg_stat_statements` in `shared_preload_libraries` already. If yours
   doesn't:
   - edit the custom parameter group attached to the instance (or cluster) and
     add it to `shared_preload_libraries`
   - set `pg_stat_statements.max` to `10000` while you are there
   - reboot the instance; both are static parameters

   A default parameter group can't be edited. Create a custom one, attach it,
   and reboot.
2. **Create the extension**, connected as the master user to the monitored
   database:

   ```sql
   CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
   ```
3. **Create the role**, as the master user:

   ```sql
   CREATE ROLE pganalyzer LOGIN PASSWORD 'choose-a-password';
   GRANT pg_monitor TO pganalyzer;
   GRANT pg_read_all_data TO pganalyzer;  -- optional, for index advice
   ```
4. **Optional:**
   - Set `track_io_timing = 1` in the parameter group. It's dynamic, so no
     reboot is needed.
   - `CREATE EXTENSION hypopg;` if this returns a row:
     `SELECT * FROM pg_available_extensions WHERE name = 'hypopg';`
5. **Connection:**
   - Use the instance endpoint (or the Aurora writer endpoint) and
     `sslmode: require`.
   - The security group must allow PGAnalyzer's address on port 5432.
   - Don't connect through RDS Proxy. See [Connection poolers](#connection-poolers).

Set `thresholds.disk_capacity_gb` to the allocated storage and
`thresholds.server_memory_gb` to the instance class's memory. That enables the
"disk full in N days" forecast and the memory-setting checks. RDS doesn't
expose either through SQL.

## Google Cloud SQL for PostgreSQL

Server settings on Cloud SQL are **database flags**, set in the console
(Instance → Edit → Flags) or with `gcloud sql instances patch --database-flags`.
Some flags restart the instance when changed, and the console says which.

1. **Create the extension.** Cloud SQL loads `pg_stat_statements` without extra
   flags. Connected as a user with `cloudsqlsuperuser` (such as `postgres`), in
   the monitored database:

   ```sql
   CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
   ```

   To raise the statement limit, set the `pg_stat_statements.max` flag to
   `10000`.
2. **Create the role:**

   ```sql
   CREATE ROLE pganalyzer LOGIN PASSWORD 'choose-a-password';
   GRANT pg_monitor TO pganalyzer;
   GRANT pg_read_all_data TO pganalyzer;  -- optional, for index advice
   ```

   You can also create the user in the console (Users → Add user account) and
   run only the grants. If a grant is refused, run `-check` as the user you'll
   actually use to see which checks are affected.
3. **Optional:**
   - Set the `track_io_timing` flag to `on`.
   - `CREATE EXTENSION hypopg;` where it's listed in `pg_available_extensions`.
4. **Connection:** use the instance's private or public IP with
   `sslmode: require`, or run the Cloud SQL Auth Proxy next to PGAnalyzer and
   connect to it on `localhost` with `sslmode: disable`, since the proxy
   encrypts the connection. The instance must allow PGAnalyzer's network
   (authorized networks or private IP).

As with RDS, set `thresholds.disk_capacity_gb` (the instance's storage) and
`thresholds.server_memory_gb` (the machine type's memory).

## Supabase

Supabase projects come with `pg_stat_statements` loaded and the extension
enabled, in the `extensions` schema.

1. **Check the extension.** In the dashboard, open Database → Extensions and
   confirm `pg_stat_statements` is enabled. Enable `hypopg` there too if you
   want validated index proposals. Supabase's own index advisor uses it.
2. **Create the role**, in the SQL editor:

   ```sql
   CREATE ROLE pganalyzer LOGIN PASSWORD 'choose-a-password';
   GRANT pg_monitor TO pganalyzer;
   GRANT pg_read_all_data TO pganalyzer;  -- optional, for index advice
   -- The extension lives in the "extensions" schema:
   ALTER ROLE pganalyzer SET search_path = "$user", public, extensions;
   ```

   Without the `search_path` line, the role doesn't find the
   `pg_stat_statements` view. The setup check fails and prints this fix.
3. **Connection:** from the project's Connect dialog, use either:
   - the **direct connection**, `db.<project-ref>.supabase.co` on port 5432.
     This is IPv6 only unless the IPv4 add-on is enabled.
   - the **session pooler**, the pooler host on port **5432**, with the user
     written as `pganalyzer.<project-ref>`.

   Don't use the transaction pooler on port 6543 (see below). Use
   `sslmode: require`.

Supabase shows disk size and compute memory on the project's settings pages.
Use them for `thresholds.disk_capacity_gb` and `thresholds.server_memory_gb`.

## Connection poolers

Connect PGAnalyzer directly to PostgreSQL, or through a pooler in **session**
mode. Transaction-mode poolers don't work: PgBouncer with
`pool_mode = transaction`, the Supabase transaction pooler, RDS Proxy with
multiplexing. PGAnalyzer relies on session state:
- the index advisor prepares statements and creates hypothetical indexes, then
  removes them on the same connection
- `EXPLAIN` runs inside a read-only transaction

PGAnalyzer holds only a few connections, so it doesn't need a pooler.

## Troubleshooting

| Setup check says | Cause | Fix |
|---|---|---|
| `pg_stat_statements`: not created in database "…" | The extension is missing in the monitored database | `CREATE EXTENSION pg_stat_statements;` in that database |
| `pg_stat_statements`: exists but the library is not loaded | The extension was created, but the library isn't in `shared_preload_libraries` | Add it (parameter group / flags on managed services) and restart |
| `pg_stat_statements`: the extension is in schema "…", which is not on the role's search_path | The extension was created in another schema (as on Supabase) | `ALTER ROLE pganalyzer SET search_path = "$user", public, extensions;`, using the schema the check names |
| Monitoring privileges: lacks pg_monitor | The role has no monitoring grants | `GRANT pg_monitor TO pganalyzer;` |
| PostgreSQL connection failed | Host, port, credentials, TLS or network | Check `postgres.*` in the config; managed services usually need `sslmode: require` and an allow-listed address |
| Slow-query history / Unused-index evidence: pending | Not enough history yet | Nothing to fix. Each item says when it will be ready. |

After changing something, click **Check again** on the `/setup` page, or rerun
`task check`.
