package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/plans"
)

// Planning limits. Planning takes ACCESS SHARE locks on every table in the
// query, so a short lock_timeout keeps it from queueing behind DDL and in turn
// blocking the application.
const (
	planStatementTimeout = "2s"
	planLockTimeout      = "100ms"
)

var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// MaxPlaceholder returns the highest $n placeholder in a normalized query, or 0.
func MaxPlaceholder(query string) int {
	maxPos := 0
	for _, m := range placeholderPattern.FindAllStringSubmatch(query, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > maxPos {
			maxPos = n
		}
	}
	return maxPos
}

// Explainable reports whether a pg_stat_statements query can be planned
// without side effects. EXPLAIN never executes the statement, but utility
// commands (DDL, VACUUM, SET ...) cannot be explained at all.
func Explainable(query string) bool {
	q := strings.ToUpper(strings.TrimLeft(query, " \t\r\n("))
	for _, kw := range []string{"SELECT", "WITH", "UPDATE", "DELETE", "TABLE", "VALUES"} {
		if strings.HasPrefix(q, kw) {
			return true
		}
	}
	return false
}

// ExplainGeneric returns the generic plan of a normalized query such as
// "SELECT ... WHERE status = $1", without parameter values and without running it.
//
// The query is prepared and explained with NULL for every parameter under
// plan_cache_mode = force_generic_plan. A generic plan never looks at parameter
// values, so the NULLs do not affect it. This is what EXPLAIN (GENERIC_PLAN)
// does from PostgreSQL 16, and works on every supported version.
func (c *PgxClient) ExplainGeneric(ctx context.Context, query string) (*models.ExplainPlan, error) {
	if !Explainable(query) {
		return nil, fmt.Errorf("postgres: only SELECT, WITH, UPDATE and DELETE statements can be explained")
	}
	var planJSON string
	err := c.withPlanner(ctx, func(p *planner) error {
		var err error
		planJSON, err = p.explain(ctx, query)
		return err
	})
	if err != nil {
		return nil, err
	}
	plan := &models.ExplainPlan{PlanJSON: planJSON, CapturedAt: time.Now()}
	plan.PlanText = planText(planJSON)
	return plan, nil
}

// HypoPGAvailable reports whether the hypopg extension is installed in the
// connected database. PGAnalyzer never installs it.
func (c *PgxClient) HypoPGAvailable(ctx context.Context) (bool, error) {
	if c.pool == nil {
		return false, fmt.Errorf("postgres: not connected")
	}
	var ok bool
	err := c.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'hypopg')`).Scan(&ok)
	return ok, err
}

// ValidateIndex asks the planner, through a hypopg hypothetical index, whether
// it would use an index built by createIndex for the query, and at what cost.
// Nothing is built; the hypothetical index is dropped before returning.
func (c *PgxClient) ValidateIndex(ctx context.Context, query, createIndex string) (*models.IndexValidation, error) {
	v := &models.IndexValidation{}
	err := c.withPlanner(ctx, func(p *planner) error {
		before, err := p.explain(ctx, query)
		if err != nil {
			return err
		}
		if v.CostBefore, err = totalCost(before); err != nil {
			return err
		}

		p.hypo = true
		var indexName string
		if err := p.tx.QueryRow(ctx, `SELECT indexname FROM hypopg_create_index($1)`, createIndex).Scan(&indexName); err != nil {
			return fmt.Errorf("postgres: creating hypothetical index: %w", err)
		}
		after, err := p.explain(ctx, query)
		if err != nil {
			return err
		}
		if v.CostAfter, err = totalCost(after); err != nil {
			return err
		}
		root, err := plans.Parse(after)
		if err != nil {
			return err
		}
		v.UsesIndex = root.UsesIndex(indexName)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// GetTableIndexInfo returns what is needed to judge an index proposal on a
// table: the key columns of its valid indexes, its estimated row count and the
// average width of the given columns from pg_stats.
func (c *PgxClient) GetTableIndexInfo(ctx context.Context, schema, table string, columns []string) (*models.TableIndexInfo, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}
	info := &models.TableIndexInfo{}
	err := c.pool.QueryRow(ctx, `
		SELECT c.reltuples::bigint,
		       COALESCE((SELECT sum(s.avg_width) FROM pg_stats s
		                 WHERE s.schemaname = n.nspname AND s.tablename = c.relname
		                   AND s.attname = ANY($3)), 0)::int
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`, schema, table, columns).Scan(&info.EstimatedRows, &info.AvgKeyWidth)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: table %s.%s not found", schema, table)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: reading table size: %w", err)
	}

	// Expression columns (attnum 0) have no name and are left out, so an
	// expression index never counts as covering plain columns.
	rows, err := c.pool.Query(ctx, `
		SELECT ic.relname,
		       array_agg(a.attname ORDER BY k.ord) FILTER (WHERE a.attname IS NOT NULL)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_am am ON am.oid = ic.relam
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
		LEFT JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum AND k.attnum > 0
		WHERE n.nspname = $1 AND t.relname = $2 AND i.indisvalid AND am.amname = 'btree'
		  AND k.ord <= i.indnkeyatts AND i.indpred IS NULL
		GROUP BY ic.relname
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading indexes of %s.%s: %w", schema, table, err)
	}
	info.Indexes, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.IndexKeys, error) {
		var k models.IndexKeys
		err := row.Scan(&k.Name, &k.Columns)
		return k, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: reading indexes of %s.%s: %w", schema, table, err)
	}
	return info, nil
}

// planner runs EXPLAINs in a read-only transaction on one dedicated connection.
type planner struct {
	tx    pgx.Tx
	stmts []string
	hypo  bool
	seq   int
}

// withPlanner runs fn in a read-only transaction with planning limits.
//
// Prepared statements and hypopg indexes belong to the session, not the
// transaction, so rolling back does not remove them. They are cleaned up
// explicitly after the rollback; if that fails the connection is closed rather
// than returned to the pool carrying leftovers.
func (c *PgxClient) withPlanner(ctx context.Context, fn func(p *planner) error) (err error) {
	if c.pool == nil {
		return fmt.Errorf("postgres: not connected")
	}
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("postgres: acquiring connection: %w", err)
	}
	p := &planner{}
	defer func() {
		if !cleanupPlanner(conn, p) {
			_ = conn.Conn().Close(context.Background())
		}
		conn.Release()
	}()

	p.tx, err = conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("postgres: beginning transaction: %w", err)
	}
	defer p.tx.Rollback(context.Background()) //nolint:errcheck // always rolled back; nothing is written

	for _, set := range []string{
		"SET LOCAL statement_timeout = '" + planStatementTimeout + "'",
		"SET LOCAL lock_timeout = '" + planLockTimeout + "'",
		"SET LOCAL plan_cache_mode = force_generic_plan",
	} {
		if _, err := p.tx.Exec(ctx, set); err != nil {
			return fmt.Errorf("postgres: %s: %w", set, err)
		}
	}
	return fn(p)
}

// cleanupPlanner removes session state left by a planner and reports success.
func cleanupPlanner(conn *pgxpool.Conn, p *planner) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.tx != nil {
		_ = p.tx.Rollback(ctx)
	}
	ok := true
	for _, name := range p.stmts {
		if _, err := conn.Exec(ctx, "DEALLOCATE "+name); err != nil && !strings.Contains(err.Error(), "does not exist") {
			ok = false
		}
	}
	if p.hypo {
		if _, err := conn.Exec(ctx, "SELECT hypopg_reset()"); err != nil {
			ok = false
		}
	}
	return ok
}

// explain prepares query and returns its generic plan as JSON.
func (p *planner) explain(ctx context.Context, query string) (string, error) {
	p.seq++
	name := fmt.Sprintf("pganalyzer_plan_%d_%d", time.Now().UnixNano(), p.seq)
	// Record the name before PREPARE: if PREPARE itself fails nothing exists, and
	// DEALLOCATE of an unknown name is tolerated during cleanup.
	p.stmts = append(p.stmts, name)
	if _, err := p.tx.Exec(ctx, "PREPARE "+name+" AS "+query); err != nil {
		return "", fmt.Errorf("postgres: preparing query: %w", err)
	}

	args := ""
	if n := MaxPlaceholder(query); n > 0 {
		args = "(" + strings.TrimSuffix(strings.Repeat("NULL, ", n), ", ") + ")"
	}
	parts, err := readExplainRows(ctx, p.tx, "EXPLAIN (VERBOSE, FORMAT JSON) EXECUTE "+name+args)
	if err != nil {
		return "", err
	}
	return strings.Join(parts, ""), nil
}

func totalCost(planJSON string) (float64, error) {
	root, err := plans.Parse(planJSON)
	if err != nil {
		return 0, err
	}
	return root.TotalCost, nil
}

// planText renders plan JSON indented for display, or returns it unchanged
// when it does not parse.
func planText(planJSON string) string {
	var doc any
	if err := json.Unmarshal([]byte(planJSON), &doc); err != nil {
		return planJSON
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return planJSON
	}
	return string(out)
}
