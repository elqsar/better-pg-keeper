package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/elqsar/pganalyzer/internal/models"
)

// reviewedSettings are the pg_settings rows the configuration review reads.
// Settings with a pending restart are collected as well.
var reviewedSettings = []string{
	"shared_buffers", "effective_cache_size", "work_mem", "maintenance_work_mem",
	"max_connections", "random_page_cost", "effective_io_concurrency",
	"autovacuum", "track_counts", "autovacuum_max_workers", "autovacuum_naptime",
	"autovacuum_vacuum_scale_factor", "autovacuum_vacuum_cost_limit", "autovacuum_vacuum_cost_delay",
	"statement_timeout", "idle_in_transaction_session_timeout", "lock_timeout",
	"log_min_duration_statement", "log_lock_waits", "log_temp_files", "track_io_timing",
	"pg_stat_statements.max", "pg_stat_statements.track",
}

// maxAutovacuumDisabledTables bounds how many tables with autovacuum turned off
// are kept per snapshot.
const maxAutovacuumDisabledTables = 20

// settingsCheck is one independent part of GetServerSettings.
type settingsCheck struct {
	name string
	run  func(ctx context.Context, s *models.ServerSettings) error
}

// GetServerSettings reads the server configuration for review. As with
// GetOutageRisk, checks are independent: a failed one is recorded in
// Unavailable, and an error is returned only when all of them failed.
func (c *PgxClient) GetServerSettings(ctx context.Context) (*models.ServerSettings, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	s := &models.ServerSettings{}
	checks := []settingsCheck{
		{"settings", c.settingsRows},
		{"stat_statements", c.settingsStatStatements},
		{"autovacuum_disabled", c.settingsAutovacuumDisabled},
	}
	for _, check := range checks {
		if err := check.run(ctx, s); err != nil {
			if s.Unavailable == nil {
				s.Unavailable = make(map[string]string)
			}
			s.Unavailable[check.name] = err.Error()
		}
	}
	if len(s.Unavailable) == len(checks) {
		return nil, fmt.Errorf("postgres: every settings check failed, e.g. settings: %s", s.Unavailable["settings"])
	}
	return s, nil
}

func (c *PgxClient) settingsRows(ctx context.Context, s *models.ServerSettings) error {
	rows, err := c.pool.Query(ctx, `
		SELECT name, COALESCE(setting, ''), COALESCE(unit, ''), COALESCE(source, ''),
		       COALESCE(context, ''), COALESCE(boot_val, ''), COALESCE(pending_restart, false)
		FROM pg_settings
		WHERE name = ANY($1) OR pending_restart
	`, reviewedSettings)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Setting, error) {
		var st models.Setting
		err := row.Scan(&st.Name, &st.Setting, &st.Unit, &st.Source, &st.Context, &st.BootVal, &st.PendingRestart)
		return st, err
	})
	if err != nil {
		return err
	}
	s.Settings = make(map[string]models.Setting, len(list))
	for _, st := range list {
		s.Settings[st.Name] = st
	}
	return nil
}

func (c *PgxClient) settingsStatStatements(ctx context.Context, s *models.ServerSettings) error {
	var installed bool
	if err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')
	`).Scan(&installed); err != nil {
		return err
	}
	if !installed {
		return nil
	}

	// Counting rows needs no pg_read_all_stats: other roles' rows are still
	// listed, only with their query text hidden.
	u := &models.StatStatementsUsage{}
	if err := c.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM pg_stat_statements),
		       COALESCE(current_setting('pg_stat_statements.max', true), '0')::bigint,
		       i.dealloc, i.stats_reset
		FROM pg_stat_statements_info i
	`).Scan(&u.Entries, &u.Max, &u.Dealloc, &u.StatsReset); err != nil {
		return err
	}
	s.StatStatements = u
	return nil
}

func (c *PgxClient) settingsAutovacuumDisabled(ctx context.Context, s *models.ServerSettings) error {
	// Partitioned parents hold no rows and are never vacuumed themselves, so
	// only their partitions matter.
	rows, err := c.pool.Query(ctx, `
		SELECT n.nspname, c.relname, pg_total_relation_size(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'm')
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND EXISTS (
		      SELECT 1 FROM unnest(c.reloptions) o
		      WHERE o ~* '^autovacuum_enabled=(false|off|no|0|f|n)$'
		  )
		ORDER BY 3 DESC
		LIMIT $1
	`, maxAutovacuumDisabledTables)
	if err != nil {
		return err
	}
	s.AutovacuumDisabled, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.TableSize, error) {
		var t models.TableSize
		err := row.Scan(&t.SchemaName, &t.RelName, &t.TotalBytes)
		return t, err
	})
	return err
}
