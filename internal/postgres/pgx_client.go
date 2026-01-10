package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/pganalyzer/internal/models"
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

	connStr := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s sslmode=%s",
		c.config.Host, port, c.config.Database, c.config.User, sslMode,
	)

	if c.config.Password != "" {
		connStr += fmt.Sprintf(" password=%s", c.config.Password)
	}

	if c.config.ConnectTimeout > 0 {
		connStr += fmt.Sprintf(" connect_timeout=%d", int(c.config.ConnectTimeout.Seconds()))
	}

	return connStr
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

	query := `
		SELECT
			queryid,
			query,
			calls,
			total_exec_time,
			mean_exec_time,
			min_exec_time,
			max_exec_time,
			rows,
			shared_blks_hit,
			shared_blks_read,
			COALESCE(plans, 0) as plans,
			COALESCE(total_plan_time, 0) as total_plan_time
		FROM pg_stat_statements
		WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
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

	query := `
		SELECT
			schemaname,
			relname,
			seq_scan,
			seq_tup_read,
			COALESCE(idx_scan, 0) as idx_scan,
			COALESCE(idx_tup_fetch, 0) as idx_tup_fetch,
			n_live_tup,
			n_dead_tup,
			last_vacuum,
			last_autovacuum,
			last_analyze,
			pg_table_size(relid) as table_size,
			pg_indexes_size(relid) as index_size
		FROM pg_stat_user_tables
	`

	rows, err := c.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to query pg_stat_user_tables: %w", err)
	}
	defer rows.Close()

	var stats []models.TableStat
	for rows.Next() {
		var s models.TableStat
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
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan table stat: %w", err)
		}
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
			i.indisprimary
		FROM pg_stat_user_indexes s
		JOIN pg_index i ON s.indexrelid = i.indexrelid
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
			COALESCE(ROUND(100.0 * blks_hit / NULLIF(blks_hit + blks_read, 0), 2), 0) as cache_hit_ratio
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

// GetTableBloat retrieves bloat information for tables with significant dead tuples.
func (c *PgxClient) GetTableBloat(ctx context.Context) ([]models.BloatInfo, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	query := `
		SELECT
			schemaname,
			relname,
			n_dead_tup,
			n_live_tup,
			COALESCE(ROUND(n_dead_tup::numeric / NULLIF(n_live_tup, 0) * 100, 2), 0) as bloat_percent
		FROM pg_stat_user_tables
		WHERE n_dead_tup > 1000
		ORDER BY n_dead_tup DESC
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
			&b.SchemaName,
			&b.RelName,
			&b.NDeadTup,
			&b.NLiveTup,
			&b.BloatPercent,
		)
		if err != nil {
			return nil, fmt.Errorf("postgres: failed to scan bloat info: %w", err)
		}
		bloatInfo = append(bloatInfo, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: error iterating bloat info: %w", err)
	}

	return bloatInfo, nil
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

// isWriteQuery checks if a query modifies data.
func isWriteQuery(query string) bool {
	upper := strings.ToUpper(strings.TrimSpace(query))
	writeKeywords := []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE", "DROP", "ALTER", "CREATE"}
	for _, keyword := range writeKeywords {
		if strings.HasPrefix(upper, keyword) {
			return true
		}
	}
	return false
}

// Explain runs EXPLAIN on a query and returns the execution plan.
func (c *PgxClient) Explain(ctx context.Context, query string, analyze bool) (*models.ExplainPlan, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	// Never auto-analyze write queries
	if analyze && isWriteQuery(query) {
		return nil, fmt.Errorf("postgres: cannot use ANALYZE on write queries (INSERT/UPDATE/DELETE)")
	}

	var explainQuery string
	if analyze {
		// Use a statement timeout for ANALYZE to prevent long-running queries
		explainQuery = fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, VERBOSE, SETTINGS, FORMAT JSON) %s", query)
	} else {
		explainQuery = fmt.Sprintf("EXPLAIN (BUFFERS, VERBOSE, SETTINGS, FORMAT JSON) %s", query)
	}

	rows, err := c.pool.Query(ctx, explainQuery)
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

	planJSON := strings.Join(planParts, "")

	plan := &models.ExplainPlan{
		PlanJSON:   planJSON,
		CapturedAt: time.Now(),
	}

	// Parse JSON to extract plan text and execution time
	var planData []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &planData); err == nil && len(planData) > 0 {
		// Generate text representation from JSON
		if planText, err := json.MarshalIndent(planData, "", "  "); err == nil {
			plan.PlanText = string(planText)
		}

		// Extract execution time if ANALYZE was used
		if analyze {
			if planInfo, ok := planData[0]["Plan"].(map[string]any); ok {
				if actualTime, ok := planInfo["Actual Total Time"].(float64); ok {
					plan.ExecutionTime = &actualTime
				}
			}
		}
	} else {
		plan.PlanText = planJSON
	}

	return plan, nil
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

// GetStatsResetTime returns the time when pg_stat_statements statistics were last reset.
func (c *PgxClient) GetStatsResetTime(ctx context.Context) (*time.Time, error) {
	if c.pool == nil {
		return nil, fmt.Errorf("postgres: not connected")
	}

	// pg_stat_statements_info is available in PG14+
	var resetTime *time.Time
	err := c.pool.QueryRow(ctx, "SELECT stats_reset FROM pg_stat_statements_info").Scan(&resetTime)
	if err != nil {
		// If the view doesn't exist (PG < 14), return nil without error
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		// Handle case where stats have never been reset
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("postgres: failed to get stats reset time: %w", err)
	}

	return resetTime, nil
}

// Ensure PgxClient implements Client interface
var _ Client = (*PgxClient)(nil)
