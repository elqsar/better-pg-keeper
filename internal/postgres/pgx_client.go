package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elqsar/pganalyzer/internal/models"
)

// PgxClient implements the Client interface using pgx.
type PgxClient struct {
	config ClientConfig
	pool   *pgxpool.Pool
}

// NewClient creates a new PostgreSQL client with the given configuration.
func NewClient(cfg ClientConfig) (*PgxClient, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("postgres: host is required")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("postgres: database is required")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("postgres: user is required")
	}

	return &PgxClient{
		config: cfg,
	}, nil
}

// buildConnString constructs a PostgreSQL connection string from config.
func (c *PgxClient) buildConnString() string {
	port := c.config.Port
	if port == 0 {
		port = 5432
	}

	sslMode := c.config.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}

	user := url.User(c.config.User)
	if c.config.Password != "" {
		user = url.UserPassword(c.config.User, c.config.Password)
	}
	address := &url.URL{
		Scheme: "postgres",
		User:   user,
		Host:   net.JoinHostPort(c.config.Host, strconv.Itoa(port)),
		Path:   "/" + c.config.Database,
	}
	params := url.Values{"sslmode": {sslMode}}
	if c.config.ConnectTimeout > 0 {
		params.Set("connect_timeout", strconv.Itoa(int(c.config.ConnectTimeout.Seconds())))
	}
	address.RawQuery = params.Encode()
	return address.String()
}

// Connect establishes a connection pool to the PostgreSQL database.
func (c *PgxClient) Connect(ctx context.Context) error {
	connStr := c.buildConnString()

	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return fmt.Errorf("postgres: failed to parse connection string: %w", err)
	}

	// Apply pool settings
	if c.config.MaxConnections > 0 {
		poolConfig.MaxConns = int32(c.config.MaxConnections)
	}
	if c.config.MinConnections > 0 {
		poolConfig.MinConns = int32(c.config.MinConnections)
	}
	if c.config.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = c.config.MaxConnLifetime
	}
	if c.config.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = c.config.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("postgres: failed to create connection pool: %w", err)
	}

	c.pool = pool
	return nil
}

// Close closes all connections in the pool.
func (c *PgxClient) Close() error {
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}
	return nil
}

// Ping verifies the database connection is still alive.
func (c *PgxClient) Ping(ctx context.Context) error {
	if c.pool == nil {
		return fmt.Errorf("postgres: not connected")
	}
	return c.pool.Ping(ctx)
}

// GetStatStatements retrieves query statistics from pg_stat_statements.
func (c *PgxClient) GetStatStatements(ctx context.Context) ([]models.QueryStat, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	// pg_stat_statements rows are keyed by (userid, dbid, queryid, toplevel), so the
	// same normalized query executed by two roles - or counted both as a top-level
	// statement and from inside a function - appears as several rows with one queryid.
	// Aggregate them here so every downstream consumer gets exactly one row per
	// queryid instead of an arbitrary one silently winning an upsert.
	query := `
		SELECT
			queryid,
			min(query) AS query,
			sum(calls) AS calls,
			sum(total_exec_time) AS total_exec_time,
			COALESCE(sum(total_exec_time) / nullif(sum(calls), 0), 0) AS mean_exec_time,
			COALESCE(min(min_exec_time) FILTER (WHERE calls > 0), 0) AS min_exec_time,
			max(max_exec_time) AS max_exec_time,
			sum(rows) AS rows,
			sum(shared_blks_hit) AS shared_blks_hit,
			sum(shared_blks_read) AS shared_blks_read,
			sum(COALESCE(plans, 0)) AS plans,
			sum(COALESCE(total_plan_time, 0)) AS total_plan_time
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
		  AND queryid IS NOT NULL
		GROUP BY queryid
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query pg_stat_statements: %w", err)
	}
	defer rows.Close()

	var stats []models.QueryStat
	for rows.Next() {
		var s models.QueryStat
		err := rows.Scan(
			&s.QueryID,
			&s.Query,
			&s.Calls,
			&s.TotalExecTime,
			&s.MeanExecTime,
			&s.MinExecTime,
			&s.MaxExecTime,
			&s.Rows,
			&s.SharedBlksHit,
			&s.SharedBlksRead,
			&s.Plans,
			&s.TotalPlanTime,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan query stat: %w", err)
		}
		stats = append(stats, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating query stats: %w", err)
	}

	return stats, nil
}

// GetStatTables retrieves table statistics from pg_stat_user_tables.
func (c *PgxClient) GetStatTables(ctx context.Context) ([]models.TableStat, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}
	defaults, err := c.autovacuumDefaults(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading autovacuum thresholds: %w", err)
	}

	query := `
		SELECT
			s.schemaname, s.relname, s.seq_scan, s.seq_tup_read,
			COALESCE(s.idx_scan, 0), COALESCE(s.idx_tup_fetch, 0),
			s.n_live_tup, s.n_dead_tup, s.last_vacuum, s.last_autovacuum,
			s.last_analyze, pg_table_size(s.relid), pg_indexes_size(s.relid),
			s.last_autoanalyze, pg_relation_size(s.relid), c.reltuples,
			s.n_mod_since_analyze, s.vacuum_count, s.autovacuum_count,
			s.analyze_count, s.autoanalyze_count, c.reloptions
		FROM pg_stat_user_tables s
		JOIN pg_class c ON c.oid = s.relid
		WHERE c.relkind IN ('r', 'm')
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query pg_stat_user_tables: %w", err)
	}
	defer rows.Close()

	var stats []models.TableStat
	for rows.Next() {
		var s models.TableStat
		var reloptions []string
		err := rows.Scan(
			&s.SchemaName,
			&s.RelName,
			&s.SeqScan,
			&s.SeqTupRead,
			&s.IdxScan,
			&s.IdxTupFetch,
			&s.NLiveTup,
			&s.NDeadTup,
			&s.LastVacuum,
			&s.LastAutovacuum,
			&s.LastAnalyze,
			&s.TableSize,
			&s.IndexSize,
			&s.LastAutoanalyze,
			&s.HeapBytes,
			&s.RelTuples,
			&s.NModSinceAnalyze,
			&s.VacuumCount,
			&s.AutovacuumCount,
			&s.AnalyzeCount,
			&s.AutoanalyzeCount,
			&reloptions,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan table stat: %w", err)
		}
		applyAutovacuumThresholds(&s, defaults, reloptions)
		stats = append(stats, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating table stats: %w", err)
	}

	return stats, nil
}

// GetStatIndexes retrieves index statistics from pg_stat_user_indexes.
func (c *PgxClient) GetStatIndexes(ctx context.Context) ([]models.IndexStat, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			s.schemaname,
			s.relname,
			s.indexrelname,
			s.idx_scan,
			s.idx_tup_read,
			s.idx_tup_fetch,
			pg_relation_size(s.indexrelid) as index_size,
			i.indisunique,
			i.indisprimary,
			am.amname,
			pg_get_indexdef(s.indexrelid),
			-- Key columns in order, with the opclass, collation and sort options
			-- that decide whether one index can stand in for another.
			COALESCE((
				SELECT string_agg(k.attnum || ':' || k.opc || ':' || k.coll || ':' || k.opt, ' ' ORDER BY k.ord)
				FROM unnest(i.indkey::int2[], i.indclass::oid[], i.indcollation::oid[], i.indoption::int2[])
					WITH ORDINALITY AS k(attnum, opc, coll, opt, ord)
				WHERE k.ord <= i.indnkeyatts
			), ''),
			COALESCE((
				SELECT string_agg(k.attnum::text, ' ' ORDER BY k.ord)
				FROM unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
				WHERE k.ord > i.indnkeyatts
			), ''),
			COALESCE(pg_get_expr(i.indexprs, i.indrelid), ''),
			COALESCE(pg_get_expr(i.indpred, i.indrelid), ''),
			-- A non-partial btree/hash index whose leading key columns are exactly
			-- a foreign key's columns serves that FK's lookups on delete/update.
			EXISTS (
				SELECT 1
				FROM pg_constraint c,
					LATERAL (
						SELECT array_agg(k.attnum) AS lead
						FROM unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
						WHERE k.ord <= cardinality(c.conkey)
					) l
				WHERE c.contype = 'f' AND c.conrelid = i.indrelid AND i.indpred IS NULL
					AND am.amname IN ('btree', 'hash')
					AND cardinality(c.conkey) <= i.indnkeyatts
					AND l.lead @> c.conkey AND l.lead <@ c.conkey
			),
			-- A crash discards statistics along with stats_reset, so a NULL reset
			-- time only guarantees counting since the server started.
			(SELECT COALESCE(d.stats_reset, pg_postmaster_start_time())
				FROM pg_stat_database d WHERE d.datname = current_database())
		FROM pg_stat_user_indexes s
		JOIN pg_index i ON s.indexrelid = i.indexrelid
		JOIN pg_class ic ON ic.oid = s.indexrelid
		JOIN pg_am am ON am.oid = ic.relam
		WHERE i.indisvalid
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query pg_stat_user_indexes: %w", err)
	}
	defer rows.Close()

	var stats []models.IndexStat
	for rows.Next() {
		var s models.IndexStat
		err := rows.Scan(
			&s.SchemaName,
			&s.RelName,
			&s.IndexRelName,
			&s.IdxScan,
			&s.IdxTupRead,
			&s.IdxTupFetch,
			&s.IndexSize,
			&s.IsUnique,
			&s.IsPrimary,
			&s.AccessMethod,
			&s.IndexDef,
			&s.KeyColumns,
			&s.IncludeColumns,
			&s.Expressions,
			&s.Predicate,
			&s.BacksForeignKey,
			&s.StatsSince,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan index stat: %w", err)
		}
		stats = append(stats, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating index stats: %w", err)
	}

	return stats, nil
}

// GetDatabaseStats retrieves database-level statistics including cache hit ratio.
func (c *PgxClient) GetDatabaseStats(ctx context.Context) (*models.DatabaseStats, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			datname,
			blks_hit,
			blks_read,
			COALESCE(ROUND(100.0 * blks_hit / NULLIF(blks_hit + blks_read, 0), 2), 100) as cache_hit_ratio
		FROM pg_stat_database
		WHERE datname = current_database()
	`

	var stats models.DatabaseStats
	err := c.pool.QueryRow(ctx, query).Scan(
		&stats.DatabaseName,
		&stats.BlksHit,
		&stats.BlksRead,
		&stats.CacheHitRatio,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query database stats: %w", err)
	}

	return &stats, nil
}

// GetTableBloat retrieves heap space diagnostics for user tables.
func (c *PgxClient) GetTableBloat(ctx context.Context) ([]models.BloatInfo, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			s.relid, s.schemaname, s.relname, s.n_dead_tup, s.n_live_tup,
			COALESCE(ROUND(s.n_dead_tup::numeric / NULLIF(s.n_live_tup, 0) * 100, 2), 0),
			GREATEST(c.relpages, 0)::bigint * current_setting('block_size')::bigint
		FROM pg_stat_user_tables s
		JOIN pg_class c ON c.oid = s.relid
		WHERE c.relkind IN ('r', 'm')
		  AND (s.n_dead_tup >= 1000 OR GREATEST(c.relpages, 0)::bigint * current_setting('block_size')::bigint >= 67108864)
		ORDER BY GREATEST(c.relpages, 0) DESC
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query table bloat: %w", err)
	}
	defer rows.Close()

	var bloatInfo []models.BloatInfo
	for rows.Next() {
		var b models.BloatInfo
		err := rows.Scan(
			&b.RelationOID,
			&b.SchemaName,
			&b.RelName,
			&b.NDeadTup,
			&b.NLiveTup,
			&b.BloatPercent,
			&b.HeapBytes,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan bloat info: %w", err)
		}
		bloatInfo = append(bloatInfo, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating bloat info: %w", err)
	}
	rows.Close()

	return c.measureHeapSpace(ctx, bloatInfo), nil
}

// GetIndexDetails retrieves extended index information including definition.
func (c *PgxClient) GetIndexDetails(ctx context.Context) ([]models.IndexDetail, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			s.schemaname,
			s.relname as table_name,
			s.indexrelname as index_name,
			pg_get_indexdef(s.indexrelid) as index_def,
			pg_relation_size(s.indexrelid) as index_size,
			s.idx_scan,
			s.idx_tup_read,
			s.idx_tup_fetch,
			i.indisunique,
			i.indisprimary,
			i.indisvalid,
			pg_table_size(s.relid) as table_size
		FROM pg_stat_user_indexes s
		JOIN pg_index i ON s.indexrelid = i.indexrelid
		ORDER BY s.schemaname, s.relname, s.indexrelname
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query index details: %w", err)
	}
	defer rows.Close()

	var details []models.IndexDetail
	for rows.Next() {
		var d models.IndexDetail
		err := rows.Scan(
			&d.SchemaName,
			&d.TableName,
			&d.IndexName,
			&d.IndexDef,
			&d.IndexSize,
			&d.IdxScan,
			&d.IdxTupRead,
			&d.IdxTupFetch,
			&d.IsUnique,
			&d.IsPrimary,
			&d.IsValid,
			&d.TableSize,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan index detail: %w", err)
		}
		details = append(details, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating index details: %w", err)
	}

	return details, nil
}

const analyzeStatementTimeout = "10s"

// sqlToken represents a lexical token extracted from a SQL string outside
// comments and quoted literals/identifiers.
type sqlToken struct {
	kind string // "word", "semi", "lparen", "other"
	text string // uppercased text for "word"
}

// tokenizeSQL extracts non-comment, non-literal tokens from PostgreSQL SQL.
// It understands single-line comments (--), nested block comments (/* */),
// single-quoted strings (including E'...' backslash escapes), double-quoted
// identifiers, and dollar-quoted strings ($tag$...$tag$).
func tokenizeSQL(query string) []sqlToken {
	var tokens []sqlToken
	n := len(query)
	i := 0

	for i < n {
		ch := query[i]

		// Whitespace
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == '\f' {
			i++
			continue
		}

		// Line comment: -- ...\n
		if ch == '-' && i+1 < n && query[i+1] == '-' {
			i += 2
			for i < n && query[i] != '\n' {
				i++
			}
			continue
		}

		// Nested block comment: /* ... */
		if ch == '/' && i+1 < n && query[i+1] == '*' {
			depth := 1
			i += 2
			for i < n && depth > 0 {
				if query[i] == '/' && i+1 < n && query[i+1] == '*' {
					depth++
					i += 2
				} else if query[i] == '*' && i+1 < n && query[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}

		// Escape string literal: E'...' or e'...'
		if (ch == 'E' || ch == 'e') && i+1 < n && query[i+1] == '\'' {
			i += 2
			for i < n {
				if query[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if query[i] == '\'' {
					i++
					if i < n && query[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			tokens = append(tokens, sqlToken{kind: "other"})
			continue
		}

		// Single-quoted string literal
		if ch == '\'' {
			i++
			for i < n {
				if query[i] == '\'' {
					i++
					if i < n && query[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			tokens = append(tokens, sqlToken{kind: "other"})
			continue
		}

		// Double-quoted identifier
		if ch == '"' {
			i++
			for i < n {
				if query[i] == '"' {
					i++
					if i < n && query[i] == '"' {
						i++
						continue
					}
					break
				}
				i++
			}
			tokens = append(tokens, sqlToken{kind: "other"})
			continue
		}

		// Dollar-quoted string: $tag$...$tag$ (not positional $1)
		if ch == '$' {
			end := i + 1
			validTag := false
			if end < n && query[end] == '$' {
				validTag = true
			} else if end < n && isSQLIdentStart(query[end]) {
				end++
				for end < n && (isSQLIdentStart(query[end]) || (query[end] >= '0' && query[end] <= '9')) {
					end++
				}
				validTag = end < n && query[end] == '$'
			}
			if validTag {
				tag := query[i : end+1]
				i = end + 1
				if closeIdx := strings.Index(query[i:], tag); closeIdx >= 0 {
					i += closeIdx + len(tag)
				} else {
					i = n
				}
				tokens = append(tokens, sqlToken{kind: "other"})
				continue
			}
		}

		if ch == ';' {
			tokens = append(tokens, sqlToken{kind: "semi"})
			i++
			continue
		}

		if ch == '(' {
			tokens = append(tokens, sqlToken{kind: "lparen"})
			i++
			continue
		}

		if isSQLIdentStart(ch) {
			start := i
			i++
			for i < n && isSQLIdentPart(query[i]) {
				i++
			}
			tokens = append(tokens, sqlToken{
				kind: "word",
				text: strings.ToUpper(query[start:i]),
			})
			continue
		}

		tokens = append(tokens, sqlToken{kind: "other"})
		i++
	}

	return tokens
}

func isSQLIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isSQLIdentPart(b byte) bool {
	return isSQLIdentStart(b) || (b >= '0' && b <= '9') || b == '$'
}

// hasMultipleStatements reports whether query contains more than one SQL
// statement (an unquoted semicolon followed by additional tokens).
func hasMultipleStatements(query string) bool {
	tokens := tokenizeSQL(query)
	for i, tok := range tokens {
		if tok.kind == "semi" && i < len(tokens)-1 {
			return true
		}
	}
	return false
}

var writeCommandKeywords = map[string]bool{
	"INSERT":   true,
	"UPDATE":   true,
	"DELETE":   true,
	"TRUNCATE": true,
	"DROP":     true,
	"ALTER":    true,
	"CREATE":   true,
	"MERGE":    true,
	"COPY":     true,
	"CALL":     true,
	"DO":       true,
	"GRANT":    true,
	"REVOKE":   true,
	"VACUUM":   true,
	"REINDEX":  true,
	"CLUSTER":  true,
	"LOCK":     true,
	"REFRESH":  true,
}

var cteWriteKeywords = map[string]bool{
	"INSERT": true,
	"UPDATE": true,
	"DELETE": true,
	"MERGE":  true,
}

// isWriteQuery reports whether a query modifies data, runs a utility/DDL
// command, or contains multiple statements.
func isWriteQuery(query string) bool {
	tokens := tokenizeSQL(query)
	for i, tok := range tokens {
		if tok.kind == "semi" && i < len(tokens)-1 {
			return true
		}
	}

	idx := 0
	for idx < len(tokens) && tokens[idx].kind == "lparen" {
		idx++
	}
	if idx >= len(tokens) || tokens[idx].kind != "word" {
		return false
	}

	first := tokens[idx].text
	if writeCommandKeywords[first] {
		return true
	}

	// Data-modifying CTEs: WITH d AS (DELETE ... RETURNING *) SELECT ...
	if first == "WITH" {
		for _, tok := range tokens[idx+1:] {
			if tok.kind == "word" && cteWriteKeywords[tok.text] {
				return true
			}
		}
	}

	return false
}

func explainTimeout(analyze bool) string {
	if analyze {
		return analyzeStatementTimeout
	}
	return planStatementTimeout
}

func buildExplainPlan(planJSON string, analyze bool) *models.ExplainPlan {
	plan := &models.ExplainPlan{
		PlanJSON:   planJSON,
		CapturedAt: time.Now(),
	}

	var planData []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &planData); err == nil && len(planData) > 0 {
		if formatted, err := json.MarshalIndent(planData, "", "  "); err == nil {
			plan.PlanText = string(formatted)
		} else {
			plan.PlanText = planJSON
		}

		if analyze {
			if execTime, ok := planData[0]["Execution Time"].(float64); ok {
				plan.ExecutionTime = &execTime
			} else if planInfo, ok := planData[0]["Plan"].(map[string]any); ok {
				if actualTime, ok := planInfo["Actual Total Time"].(float64); ok {
					plan.ExecutionTime = &actualTime
				}
			}
		}
	} else {
		plan.PlanText = planJSON
	}

	return plan
}

// Explain runs EXPLAIN on a query inside a read-only transaction with timeouts
// and always rolls back.
func (c *PgxClient) Explain(ctx context.Context, query string, analyze bool) (*models.ExplainPlan, error) {
	if hasMultipleStatements(query) {
		return nil, fmt.Errorf("postgres: cannot explain multiple statements")
	}
	if analyze && isWriteQuery(query) {
		return nil, fmt.Errorf("postgres: cannot use ANALYZE on write queries (INSERT/UPDATE/DELETE)")
	}
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to begin explain transaction: %w", err)
	}
	defer func() {
		rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()

	for _, set := range []string{
		"SET LOCAL statement_timeout = '" + explainTimeout(analyze) + "'",
		"SET LOCAL lock_timeout = '" + planLockTimeout + "'",
	} {
		if _, err := tx.Exec(ctx, set); err != nil {
			return nil, fmt.Errorf("postgres: %s: %w", set, err)
		}
	}

	var explainQuery string
	if analyze {
		explainQuery = fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, VERBOSE, SETTINGS, FORMAT JSON) %s", query)
	} else {
		explainQuery = fmt.Sprintf("EXPLAIN (BUFFERS, VERBOSE, SETTINGS, FORMAT JSON) %s", query)
	}

	planParts, err := readExplainRows(ctx, tx, explainQuery)
	if err != nil {
		return nil, err
	}

	return buildExplainPlan(strings.Join(planParts, ""), analyze), nil
}

// readExplainRows runs an EXPLAIN query inside tx and returns its output rows.
// It closes the result set before returning so the transaction can accept
// further commands without a "conn busy" error.
func readExplainRows(ctx context.Context, tx pgx.Tx, explainQuery string) ([]string, error) {
	rows, err := tx.Query(ctx, explainQuery)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to explain query: %w", err)
	}
	defer rows.Close()

	var planParts []string
	for rows.Next() {
		var part string
		if err := rows.Scan(&part); err != nil {
			return nil, fmt.Errorf("postgres: failed to scan explain result: %w", err)
		}
		planParts = append(planParts, part)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating explain results: %w", err)
	}

	rows.Close()
	return planParts, nil
}

// ExplainWithParams runs EXPLAIN on a query with actual parameter values.
// It uses a read-only transaction with timeouts on a dedicated connection,
// always rolls back, and explicitly deallocates the prepared statement (closing
// the connection if cleanup fails so session state never leaks into the pool).
func (c *PgxClient) ExplainWithParams(ctx context.Context, query string, params []any, analyze bool) (*models.ExplainPlan, error) {
	if hasMultipleStatements(query) {
		return nil, fmt.Errorf("postgres: cannot explain multiple statements")
	}
	if analyze && isWriteQuery(query) {
		return nil, fmt.Errorf("postgres: cannot use ANALYZE on write queries (INSERT/UPDATE/DELETE)")
	}
	if len(params) == 0 {
		return c.Explain(ctx, query, analyze)
	}

	paramLiterals := make([]string, len(params))
	for i, p := range params {
		lit, err := formatSQLLiteral(p)
		if err != nil {
			return nil, fmt.Errorf("postgres: invalid parameter $%d: %w", i+1, err)
		}
		paramLiterals[i] = lit
	}

	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: acquiring connection: %w", err)
	}

	stmtName := fmt.Sprintf("pganalyzer_explain_%d", time.Now().UnixNano())
	var tx pgx.Tx
	prepared := false

	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if tx != nil {
			_ = tx.Rollback(cleanCtx)
		}
		if prepared {
			if _, err := conn.Exec(cleanCtx, "DEALLOCATE "+stmtName); err != nil {
				var pgErr *pgconn.PgError
				// 26000 = invalid_sql_statement_name (already absent)
				if !errors.As(err, &pgErr) || pgErr.Code != "26000" {
					log.Printf("[postgres] failed to deallocate %s, closing connection to avoid leak: %v", stmtName, err)
					_ = conn.Conn().Close(cleanCtx)
				}
			}
		}
		conn.Release()
	}()

	tx, err = conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to begin transaction: %w", err)
	}

	for _, set := range []string{
		"SET LOCAL statement_timeout = '" + explainTimeout(analyze) + "'",
		"SET LOCAL lock_timeout = '" + planLockTimeout + "'",
	} {
		if _, err := tx.Exec(ctx, set); err != nil {
			return nil, fmt.Errorf("postgres: %s: %w", set, err)
		}
	}

	// Mark prepared before PREPARE so cleanup still attempts DEALLOCATE if the
	// caller context cancels right as the server finishes preparing.
	prepared = true
	prepareQuery := fmt.Sprintf("PREPARE %s AS %s", stmtName, query)
	if _, err := tx.Exec(ctx, prepareQuery); err != nil {
		return nil, fmt.Errorf("postgres: failed to prepare statement: %w", err)
	}

	explainOpts := "BUFFERS, VERBOSE, SETTINGS, FORMAT JSON"
	if analyze {
		explainOpts = "ANALYZE, BUFFERS, VERBOSE, SETTINGS, FORMAT JSON"
	}
	explainQuery := fmt.Sprintf("EXPLAIN (%s) EXECUTE %s(%s)",
		explainOpts, stmtName, strings.Join(paramLiterals, ", "))

	planParts, err := readExplainRows(ctx, tx, explainQuery)
	if err != nil {
		return nil, err
	}

	return buildExplainPlan(strings.Join(planParts, ""), analyze), nil
}

// formatSQLLiteral converts a Go value to a PostgreSQL literal string.
// This is safe for use in EXECUTE statements where parameter binding isn't available.
func formatSQLLiteral(v any) (string, error) {
	if v == nil {
		return "NULL", nil
	}

	switch val := v.(type) {
	case bool:
		if val {
			return "TRUE", nil
		}
		return "FALSE", nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val), nil
	case float32:
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("non-finite float %v", val)
		}
		return strconv.FormatFloat(f, 'f', -1, 32), nil
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return "", fmt.Errorf("non-finite float %v", val)
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case string:
		escaped := strings.ReplaceAll(val, "'", "''")
		return fmt.Sprintf("'%s'", escaped), nil
	case time.Time:
		return fmt.Sprintf("'%s'::timestamptz", val.Format(time.RFC3339)), nil
	default:
		str := fmt.Sprintf("%v", val)
		escaped := strings.ReplaceAll(str, "'", "''")
		return fmt.Sprintf("'%s'", escaped), nil
	}
}

// GetVersion returns the PostgreSQL server version string.
func (c *PgxClient) GetVersion(ctx context.Context) (string, error) {
	if c.pool == nil {
		return "", fmt.Errorf("postgres: not connected")
	}

	var version string
	err := c.pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("postgres: failed to get version: %w", err)
	}

	return version, nil
}

// MinServerVersionNum is the oldest PostgreSQL release PGAnalyzer supports, as
// server_version_num. PostgreSQL 13 is end of life, and 14 is the first release
// with pg_stat_statements_info, which statistics-reset detection relies on.
const MinServerVersionNum = 140000

// GetServerVersionNum returns the server version as an integer (e.g. 160002 for 16.2).
func (c *PgxClient) GetServerVersionNum(ctx context.Context) (int, error) {
	if c.pool == nil {
		return 0, fmt.Errorf("postgres: not connected")
	}

	var raw string
	if err := c.pool.QueryRow(ctx, "SHOW server_version_num").Scan(&raw); err != nil {
		return 0, fmt.Errorf("postgres: failed to get server version: %w", err)
	}

	num, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("postgres: unexpected server_version_num %q: %w", raw, err)
	}

	return num, nil
}

// CheckVersionSupported reports whether the server is new enough to collect from.
// Since PostgreSQL 10, server_version_num is major*10000 + minor.
func (c *PgxClient) CheckVersionSupported(ctx context.Context) error {
	num, err := c.GetServerVersionNum(ctx)
	if err != nil {
		return err
	}

	if num < MinServerVersionNum {
		return fmt.Errorf(
			"postgres: server version %d.%d is not supported, PGAnalyzer requires PostgreSQL %d or later",
			num/10000, num%10000, MinServerVersionNum/10000,
		)
	}

	return nil
}

// GetStatsResetTime returns the time when pg_stat_statements statistics were last reset.
func (c *PgxClient) GetStatsResetTime(ctx context.Context) (*time.Time, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	// pg_stat_statements_info comes with pg_stat_statements 1.9 (PostgreSQL 14).
	var resetTime *time.Time
	err := c.pool.QueryRow(ctx, "SELECT stats_reset FROM pg_stat_statements_info").Scan(&resetTime)
	if err != nil {
		// The view is missing when the extension was never updated to 1.9 after a
		// major upgrade (ALTER EXTENSION pg_stat_statements UPDATE); skip detection.
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		// Handle case where stats have never been reset
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: failed to get stats reset time: %w", err)
	}

	return resetTime, nil
}

// GetConnectionActivity retrieves connection activity from pg_stat_activity.
func (c *PgxClient) GetConnectionActivity(ctx context.Context) (*models.ConnectionActivity, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			COUNT(*) FILTER (WHERE state = 'active') as active_count,
			COUNT(*) FILTER (WHERE state = 'idle') as idle_count,
			COUNT(*) FILTER (WHERE state = 'idle in transaction') as idle_in_tx_count,
			COUNT(*) FILTER (WHERE state = 'idle in transaction (aborted)') as idle_in_tx_aborted,
			COUNT(*) FILTER (WHERE wait_event_type IS NOT NULL AND wait_event_type NOT IN ('Activity', 'Client')) as waiting_count,
			COUNT(*) as total_connections,
			(SELECT setting::int FROM pg_settings WHERE name = 'max_connections') as max_connections
		FROM pg_stat_activity
		WHERE backend_type = 'client backend'
	`

	var activity models.ConnectionActivity
	err := c.pool.QueryRow(ctx, query).Scan(
		&activity.ActiveCount,
		&activity.IdleCount,
		&activity.IdleInTxCount,
		&activity.IdleInTxAborted,
		&activity.WaitingCount,
		&activity.TotalConnections,
		&activity.MaxConnections,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query connection activity: %w", err)
	}

	return &activity, nil
}

// GetLongRunningQueries retrieves queries running longer than threshold.
func (c *PgxClient) GetLongRunningQueries(ctx context.Context, thresholdSeconds float64) ([]models.LongRunningQuery, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			pid,
			COALESCE(usename, '') as usename,
			COALESCE(datname, '') as datname,
			COALESCE(query, '') as query,
			COALESCE(state, '') as state,
			wait_event_type,
			wait_event,
			query_start,
			EXTRACT(EPOCH FROM (NOW() - query_start)) as duration_seconds
		FROM pg_stat_activity
		WHERE state = 'active'
		  AND query_start IS NOT NULL
		  AND backend_type = 'client backend'
		  AND EXTRACT(EPOCH FROM (NOW() - query_start)) > $1
		  AND pid != pg_backend_pid()
		ORDER BY duration_seconds DESC
	`

	rows, err := c.pool.Query(ctx, query, thresholdSeconds)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query long running queries: %w", err)
	}
	defer rows.Close()

	var queries []models.LongRunningQuery
	for rows.Next() {
		var q models.LongRunningQuery
		err := rows.Scan(
			&q.PID,
			&q.Username,
			&q.DatabaseName,
			&q.Query,
			&q.State,
			&q.WaitEventType,
			&q.WaitEvent,
			&q.QueryStart,
			&q.DurationSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan long running query: %w", err)
		}
		queries = append(queries, q)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating long running queries: %w", err)
	}

	return queries, nil
}

// GetIdleInTransaction retrieves connections idle in transaction longer than threshold.
func (c *PgxClient) GetIdleInTransaction(ctx context.Context, thresholdSeconds float64) ([]models.IdleInTransaction, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			pid,
			COALESCE(usename, '') as usename,
			COALESCE(datname, '') as datname,
			state,
			xact_start,
			EXTRACT(EPOCH FROM (NOW() - xact_start)) as duration_seconds,
			COALESCE(query, '') as query
		FROM pg_stat_activity
		WHERE state IN ('idle in transaction', 'idle in transaction (aborted)')
		  AND xact_start IS NOT NULL
		  AND backend_type = 'client backend'
		  AND EXTRACT(EPOCH FROM (NOW() - xact_start)) > $1
		ORDER BY duration_seconds DESC
	`

	rows, err := c.pool.Query(ctx, query, thresholdSeconds)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query idle in transaction: %w", err)
	}
	defer rows.Close()

	var idle []models.IdleInTransaction
	for rows.Next() {
		var i models.IdleInTransaction
		err := rows.Scan(
			&i.PID,
			&i.Username,
			&i.DatabaseName,
			&i.State,
			&i.XactStart,
			&i.DurationSeconds,
			&i.Query,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan idle in transaction: %w", err)
		}
		idle = append(idle, i)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating idle in transaction: %w", err)
	}

	return idle, nil
}

// GetLockStats retrieves lock statistics from pg_locks.
func (c *PgxClient) GetLockStats(ctx context.Context) (*models.LockStats, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			COUNT(*) as total_locks,
			COUNT(*) FILTER (WHERE granted) as granted_locks,
			COUNT(*) FILTER (WHERE NOT granted) as waiting_locks,
			COUNT(*) FILTER (WHERE mode = 'AccessShareLock') as access_share_locks,
			COUNT(*) FILTER (WHERE mode = 'RowExclusiveLock') as row_exclusive_locks,
			COUNT(*) FILTER (WHERE mode IN ('ExclusiveLock', 'AccessExclusiveLock')) as exclusive_locks
		FROM pg_locks
		WHERE database = (SELECT oid FROM pg_database WHERE datname = current_database())
	`

	var stats models.LockStats
	err := c.pool.QueryRow(ctx, query).Scan(
		&stats.TotalLocks,
		&stats.GrantedLocks,
		&stats.WaitingLocks,
		&stats.AccessShareLocks,
		&stats.RowExclusiveLocks,
		&stats.ExclusiveLocks,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query lock stats: %w", err)
	}

	return &stats, nil
}

// GetBlockedQueries retrieves queries blocked by other transactions using
// PostgreSQL's pg_blocking_pids(), scoped to the connected database.
func (c *PgxClient) GetBlockedQueries(ctx context.Context) ([]models.BlockedQuery, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT DISTINCT ON (blocked.pid, blocking.pid)
			blocked.pid AS blocked_pid,
			COALESCE(blocked.usename, '') AS blocked_user,
			COALESCE(blocked.query, '') AS blocked_query,
			COALESCE(blocked.query_start, NOW()) AS blocked_start,
			COALESCE(EXTRACT(EPOCH FROM (NOW() - blocked.query_start)), 0) AS wait_duration_seconds,
			blocking.pid AS blocking_pid,
			COALESCE(blocking.usename, '') AS blocking_user,
			COALESCE(blocking.query, '') AS blocking_query,
			COALESCE(bl.locktype, '') AS lock_type,
			COALESCE(bl.mode, '') AS lock_mode,
			rel.relname AS relation
		FROM pg_stat_activity blocked
		CROSS JOIN LATERAL unnest(pg_blocking_pids(blocked.pid)) AS blocker_pid
		JOIN pg_stat_activity blocking ON blocking.pid = blocker_pid
		LEFT JOIN pg_locks bl ON bl.pid = blocked.pid AND NOT bl.granted
		LEFT JOIN pg_class rel ON bl.relation = rel.oid
			AND bl.database = (SELECT oid FROM pg_database WHERE datname = current_database())
		WHERE blocked.datname = current_database()
		ORDER BY blocked.pid, blocking.pid, bl.locktype NULLS LAST
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query blocked queries: %w", err)
	}
	defer rows.Close()

	var queries []models.BlockedQuery
	for rows.Next() {
		var q models.BlockedQuery
		err := rows.Scan(
			&q.BlockedPID,
			&q.BlockedUser,
			&q.BlockedQuery,
			&q.BlockedStart,
			&q.WaitDuration,
			&q.BlockingPID,
			&q.BlockingUser,
			&q.BlockingQuery,
			&q.LockType,
			&q.LockMode,
			&q.Relation,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan blocked query: %w", err)
		}
		queries = append(queries, q)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating blocked queries: %w", err)
	}

	sort.Slice(queries, func(i, j int) bool {
		if queries[i].WaitDuration != queries[j].WaitDuration {
			return queries[i].WaitDuration > queries[j].WaitDuration
		}
		if queries[i].BlockedPID != queries[j].BlockedPID {
			return queries[i].BlockedPID < queries[j].BlockedPID
		}
		return queries[i].BlockingPID < queries[j].BlockingPID
	})

	return queries, nil
}

// GetExtendedDatabaseStats retrieves extended database statistics including
// standby recovery conflicts from pg_stat_database_conflicts.
func (c *PgxClient) GetExtendedDatabaseStats(ctx context.Context) (*models.ExtendedDatabaseStats, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			d.datname,
			d.xact_commit,
			d.xact_rollback,
			COALESCE(d.temp_files, 0) AS temp_files,
			COALESCE(d.temp_bytes, 0) AS temp_bytes,
			COALESCE(d.deadlocks, 0) AS deadlocks,
			COALESCE(c.confl_lock, 0) AS confl_lock,
			COALESCE(c.confl_snapshot, 0) AS confl_snapshot
		FROM pg_stat_database d
		LEFT JOIN pg_stat_database_conflicts c ON c.datid = d.datid
		WHERE d.datname = current_database()
	`

	var stats models.ExtendedDatabaseStats
	err := c.pool.QueryRow(ctx, query).Scan(
		&stats.DatabaseName,
		&stats.XactCommit,
		&stats.XactRollback,
		&stats.TempFiles,
		&stats.TempBytes,
		&stats.Deadlocks,
		&stats.ConflLock,
		&stats.ConflSnapshot,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query extended database stats: %w", err)
	}

	return &stats, nil
}

// Ensure PgxClient implements Client interface
var _ Client = (*PgxClient)(nil)
