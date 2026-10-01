package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/elqsar/pganalyzer/internal/models"
)

// maxReportedSequences bounds how many sequences are kept per snapshot; only the
// ones closest to their limit matter.
const maxReportedSequences = 20

// riskCheck is one independent part of GetOutageRisk.
type riskCheck struct {
	name string
	run  func(ctx context.Context, risk *models.OutageRisk) error
}

// GetOutageRisk runs every outage-risk check. Checks are independent: one that
// fails, usually for lack of a privilege such as pg_monitor, is recorded in
// Unavailable and the rest still report. It only returns an error when every
// check failed, which means the server itself is unreachable.
func (c *PgxClient) GetOutageRisk(ctx context.Context) (*models.OutageRisk, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	risk := &models.OutageRisk{}
	if err := c.pool.QueryRow(ctx, `SELECT pg_is_in_recovery(), current_database()`).Scan(&risk.InRecovery, &risk.Database); err != nil {
		return nil, fmt.Errorf("postgres: checking recovery state: %w", err)
	}

	checks := []riskCheck{
		{"wraparound", c.riskWraparound},
		{"table_ages", c.riskTableAges},
		{"xmin_backends", c.riskXminBackends},
		{"replication_slots", c.riskSlots},
		{"replication", c.riskReplicas},
		{"prepared_xacts", c.riskPreparedXacts},
		{"sequences", c.riskSequences},
		{"size", c.riskSize},
	}
	for _, check := range checks {
		if err := check.run(ctx, risk); err != nil {
			if risk.Unavailable == nil {
				risk.Unavailable = make(map[string]string)
			}
			risk.Unavailable[check.name] = err.Error()
		}
	}
	if len(risk.Unavailable) == len(checks) {
		return nil, fmt.Errorf("postgres: every outage-risk check failed, e.g. wraparound: %s", risk.Unavailable["wraparound"])
	}
	return risk, nil
}

func (c *PgxClient) riskWraparound(ctx context.Context, risk *models.OutageRisk) error {
	err := c.pool.QueryRow(ctx, `
		SELECT current_setting('autovacuum_freeze_max_age')::bigint,
		       current_setting('autovacuum_multixact_freeze_max_age')::bigint
	`).Scan(&risk.FreezeMaxAge, &risk.MultixactFreezeMaxAge)
	if err != nil {
		return err
	}

	rows, err := c.pool.Query(ctx, `
		SELECT datname, age(datfrozenxid), mxid_age(datminmxid)
		FROM pg_database
		ORDER BY age(datfrozenxid) DESC
	`)
	if err != nil {
		return err
	}
	risk.Databases, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.DatabaseAge, error) {
		var d models.DatabaseAge
		err := row.Scan(&d.Name, &d.XIDAge, &d.MXIDAge)
		return d, err
	})
	return err
}

func (c *PgxClient) riskTableAges(ctx context.Context, risk *models.OutageRisk) error {
	// A table's TOAST data is frozen separately, so its horizon counts too.
	rows, err := c.pool.Query(ctx, `
		SELECT n.nspname, c.relname,
		       GREATEST(age(c.relfrozenxid), COALESCE(age(t.relfrozenxid), 0)),
		       GREATEST(mxid_age(c.relminmxid), COALESCE(mxid_age(t.relminmxid), 0)),
		       pg_total_relation_size(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_class t ON t.oid = c.reltoastrelid
		WHERE c.relkind IN ('r', 'm')
		ORDER BY 3 DESC
		LIMIT 10
	`)
	if err != nil {
		return err
	}
	risk.OldestTables, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.TableAge, error) {
		var t models.TableAge
		err := row.Scan(&t.SchemaName, &t.RelName, &t.XIDAge, &t.MXIDAge, &t.TotalBytes)
		return t, err
	})
	return err
}

func (c *PgxClient) riskXminBackends(ctx context.Context, risk *models.OutageRisk) error {
	rows, err := c.pool.Query(ctx, `
		SELECT pid, COALESCE(usename, ''), COALESCE(datname, ''), COALESCE(state, ''),
		       GREATEST(age(backend_xmin), age(backend_xid)), xact_start,
		       left(COALESCE(query, ''), 500)
		FROM pg_stat_activity
		WHERE (backend_xmin IS NOT NULL OR backend_xid IS NOT NULL)
		  AND pid <> pg_backend_pid()
		ORDER BY 5 DESC
		LIMIT 5
	`)
	if err != nil {
		return err
	}
	risk.XminBackends, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.XminBackend, error) {
		var b models.XminBackend
		err := row.Scan(&b.PID, &b.Username, &b.Database, &b.State, &b.XminAge, &b.XactStart, &b.Query)
		return b, err
	})
	return err
}

func (c *PgxClient) riskSlots(ctx context.Context, risk *models.OutageRisk) error {
	// pg_current_wal_lsn() raises an error during recovery, and CASE keeps it
	// from being evaluated there.
	rows, err := c.pool.Query(ctx, `
		SELECT slot_name, slot_type, active, COALESCE(wal_status, ''), safe_wal_size,
		       CASE WHEN pg_is_in_recovery() OR restart_lsn IS NULL THEN NULL
		            ELSE pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)::bigint END,
		       age(xmin), age(catalog_xmin)
		FROM pg_replication_slots
		ORDER BY slot_name
	`)
	if err != nil {
		return err
	}
	risk.Slots, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.ReplicationSlot, error) {
		var s models.ReplicationSlot
		err := row.Scan(&s.Name, &s.Type, &s.Active, &s.WALStatus, &s.SafeWALSize,
			&s.RetainedBytes, &s.XminAge, &s.CatalogXminAge)
		return s, err
	})
	return err
}

func (c *PgxClient) riskReplicas(ctx context.Context, risk *models.OutageRisk) error {
	rows, err := c.pool.Query(ctx, `
		SELECT COALESCE(application_name, ''), COALESCE(client_addr::text, ''), COALESCE(state, ''),
		       EXTRACT(EPOCH FROM write_lag)::float8,
		       EXTRACT(EPOCH FROM flush_lag)::float8,
		       EXTRACT(EPOCH FROM replay_lag)::float8
		FROM pg_stat_replication
		ORDER BY application_name
	`)
	if err != nil {
		return err
	}
	risk.Replicas, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.ReplicaLag, error) {
		var r models.ReplicaLag
		err := row.Scan(&r.ApplicationName, &r.ClientAddr, &r.State,
			&r.WriteLagSeconds, &r.FlushLagSeconds, &r.ReplayLagSecs)
		return r, err
	})
	return err
}

func (c *PgxClient) riskPreparedXacts(ctx context.Context, risk *models.OutageRisk) error {
	rows, err := c.pool.Query(ctx, `
		SELECT gid, database, owner, prepared, age(transaction)
		FROM pg_prepared_xacts
		ORDER BY prepared
	`)
	if err != nil {
		return err
	}
	risk.PreparedXacts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.PreparedXact, error) {
		var p models.PreparedXact
		err := row.Scan(&p.GID, &p.Database, &p.Owner, &p.Prepared, &p.XIDAge)
		return p, err
	})
	return err
}

func (c *PgxClient) riskSequences(ctx context.Context, risk *models.OutageRisk) error {
	// The effective limit is the smaller of the sequence's own maximum and the
	// type maximum of the column it feeds (via OWNED BY or an identity column).
	// last_value is NULL both before first use and when the role may not read
	// the sequence; only the latter is counted as unreadable.
	rows, err := c.pool.Query(ctx, `
		SELECT s.schemaname, s.sequencename,
		       COALESCE(tbl.relname, ''), COALESCE(a.attname, ''),
		       COALESCE(format_type(a.atttypid, a.atttypmod), ''),
		       s.last_value,
		       LEAST(s.max_value, CASE a.atttypid WHEN 21 THEN 32767 WHEN 23 THEN 2147483647 ELSE s.max_value END),
		       has_sequence_privilege(seq.oid, 'SELECT, USAGE')
		FROM pg_sequences s
		JOIN pg_namespace n ON n.nspname = s.schemaname
		JOIN pg_class seq ON seq.relnamespace = n.oid AND seq.relname = s.sequencename
		LEFT JOIN pg_depend d ON d.classid = 'pg_class'::regclass AND d.objid = seq.oid
		     AND d.refclassid = 'pg_class'::regclass AND d.deptype IN ('a', 'i')
		LEFT JOIN pg_class tbl ON tbl.oid = d.refobjid
		LEFT JOIN pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
		WHERE s.increment_by > 0
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var seqs []models.SequenceUsage
	for rows.Next() {
		var s models.SequenceUsage
		var lastValue *int64
		var readable bool
		if err := rows.Scan(&s.SchemaName, &s.SequenceName, &s.TableName, &s.ColumnName,
			&s.ColumnType, &lastValue, &s.MaxValue, &readable); err != nil {
			return err
		}
		if !readable {
			risk.SequencesUnreadable++
			continue
		}
		if lastValue == nil || s.MaxValue <= 0 {
			continue
		}
		s.LastValue = *lastValue
		s.UsedFraction = float64(s.LastValue) / float64(s.MaxValue)
		seqs = append(seqs, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	sort.Slice(seqs, func(i, j int) bool { return seqs[i].UsedFraction > seqs[j].UsedFraction })
	if len(seqs) > maxReportedSequences {
		seqs = seqs[:maxReportedSequences]
	}
	risk.Sequences = seqs
	return nil
}

func (c *PgxClient) riskSize(ctx context.Context, risk *models.OutageRisk) error {
	// pg_database_size needs CONNECT on each database, so the cluster total only
	// covers databases the role can reach.
	return c.pool.QueryRow(ctx, `
		SELECT pg_database_size(current_database()),
		       (SELECT COALESCE(sum(pg_database_size(oid)), 0)::bigint
		        FROM pg_database
		        WHERE datallowconn AND has_database_privilege(oid, 'CONNECT'))
	`).Scan(&risk.DatabaseSize, &risk.ClusterSize)
}
