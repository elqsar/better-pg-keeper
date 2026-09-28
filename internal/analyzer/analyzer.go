// Package analyzer provides analysis logic for identifying performance issues
// in PostgreSQL databases based on collected metrics.
package analyzer

import (
	"context"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// Analyzer defines the interface for analyzing PostgreSQL performance data.
type Analyzer interface {
	// Analyze processes a snapshot and returns analysis results.
	Analyze(ctx context.Context, snapshotID int64) (*AnalysisResult, error)
}

// Analysis data domains. These match the collector names recorded in
// snapshot_collectors, so a domain can be traced back to the collector that fills it.
const (
	DomainQueryStats    = "query_stats"
	DomainTableStats    = "table_stats"
	DomainIndexStats    = "index_stats"
	DomainBloat         = "bloat"
	DomainActivity      = "activity"
	DomainLocks         = "locks"
	DomainDatabaseStats = "database_stats"
	DomainOutageRisk    = "outage_risk"
	DomainQueryPlans    = "query_plans"
	DomainSettings      = "server_settings"
)

// AllDomains lists every domain analysis resolves.
var AllDomains = []string{
	DomainQueryStats, DomainTableStats, DomainIndexStats,
	DomainBloat, DomainActivity, DomainLocks, DomainDatabaseStats,
	DomainOutageRisk, DomainQueryPlans, DomainSettings,
}

// DomainCoverage records whether a data domain was actually observed, and how
// recently. Collectors run on intervals from 30s to 1h while snapshots are cut about
// once a minute, so any single snapshot carries only the domains that were due at the
// time. Without this, an absent domain is indistinguishable from a clean one and
// suggestions get resolved on missing data rather than on recovery.
type DomainCoverage struct {
	Present    bool          `json:"present"`
	SnapshotID int64         `json:"snapshot_id,omitempty"`
	CapturedAt time.Time     `json:"captured_at,omitempty"`
	Age        time.Duration `json:"age_ns,omitempty"`
	Stale      bool          `json:"stale"`
}

// Usable reports whether the domain was observed recently enough to draw
// conclusions from - in particular, to conclude that an issue has gone away.
func (c DomainCoverage) Usable() bool {
	return c.Present && !c.Stale
}

// FullCoverage returns coverage marking every domain as present and fresh.
//
// Production results always come from MainAnalyzer, which resolves real coverage;
// this exists for tests and for fixtures that would otherwise get a nil map, which
// deliberately reads as "nothing was observed" so that a missing map can never be
// mistaken for a clean bill of health.
func FullCoverage() map[string]DomainCoverage {
	now := time.Now()
	coverage := make(map[string]DomainCoverage, len(AllDomains))
	for _, d := range AllDomains {
		coverage[d] = DomainCoverage{Present: true, CapturedAt: now}
	}
	return coverage
}

// AnalysisResult contains all analysis findings from a snapshot.
type AnalysisResult struct {
	SnapshotID  int64          `json:"snapshot_id"`
	InstanceID  int64          `json:"instance_id"`
	AnalyzedAt  time.Time      `json:"analyzed_at"`
	SlowQueries []SlowQuery    `json:"slow_queries"`
	CacheStats  *CacheAnalysis `json:"cache_stats"`
	TableIssues []TableIssue   `json:"table_issues"`
	IndexIssues []IndexIssue   `json:"index_issues"`
	// Operational state analysis
	ActivityStats    *ActivityAnalysis    `json:"activity_stats,omitempty"`
	LockStats        *LockAnalysis        `json:"lock_stats,omitempty"`
	TransactionStats *TransactionAnalysis `json:"transaction_stats,omitempty"`
	Risk             *RiskAnalysis        `json:"risk,omitempty"`
	QueryPlans       *QueryPlanAnalysis   `json:"query_plans,omitempty"`
	Settings         *SettingsAnalysis    `json:"settings,omitempty"`
	ErrorCount       int                  `json:"error_count"`
	Errors           []string             `json:"errors,omitempty"`
	// Coverage records which data domains this result actually observed, keyed by
	// domain. Consumers must not read "no issues" as "no problems" for a domain
	// whose coverage is missing or stale.
	Coverage map[string]DomainCoverage `json:"coverage,omitempty"`
}

// DomainCoverage returns the coverage for a domain, zero-valued when unrecorded.
func (r *AnalysisResult) DomainCoverage(domain string) DomainCoverage {
	if r.Coverage == nil {
		return DomainCoverage{}
	}
	return r.Coverage[domain]
}

// DomainsUsable reports whether every listed domain was observed and is fresh.
func (r *AnalysisResult) DomainsUsable(domains ...string) bool {
	for _, d := range domains {
		if !r.DomainCoverage(d).Usable() {
			return false
		}
	}
	return true
}

// SlowQuery represents a query that exceeds the execution time threshold.
type SlowQuery struct {
	QueryID       int64   `json:"queryid"`
	Query         string  `json:"query"`
	MeanExecTime  float64 `json:"mean_exec_time_ms"`  // milliseconds
	MaxExecTime   float64 `json:"max_exec_time_ms"`   // milliseconds
	TotalExecTime float64 `json:"total_exec_time_ms"` // milliseconds
	Calls         int64   `json:"calls"`
	CacheHitRatio float64 `json:"cache_hit_ratio"` // 0-1
	AvgRows       float64 `json:"avg_rows"`
	// Delta values for "recent" analysis (if available)
	DeltaCalls        int64   `json:"delta_calls,omitempty"`
	DeltaTotalTime    float64 `json:"delta_total_time_ms,omitempty"`
	DeltaMeanExecTime float64 `json:"delta_mean_exec_time_ms,omitempty"`
	// Window is the period the mean, total, calls, rows and cache figures cover.
	// Zero means they are cumulative since pg_stat_statements was last reset,
	// which happens only until enough history has been collected.
	Window time.Duration `json:"window_ns,omitempty"`
	// LifetimeMeanExecTime is the mean since statistics were reset, for comparing
	// the window against the query's long-run behaviour.
	LifetimeMeanExecTime float64 `json:"lifetime_mean_exec_time_ms,omitempty"`
}

// CacheAnalysis contains database-level and query-level cache statistics.
type CacheAnalysis struct {
	OverallHitRatio  float64          `json:"overall_hit_ratio"` // 0-100 percentage
	BelowThreshold   bool             `json:"below_threshold"`
	Threshold        float64          `json:"threshold"`       // configured threshold
	TrackedQueries   int              `json:"tracked_queries"` // distinct queries in pg_stat_statements
	PoorCacheQueries []PoorCacheQuery `json:"poor_cache_queries,omitempty"`
}

// PoorCacheQuery represents a query with below-threshold cache performance.
type PoorCacheQuery struct {
	QueryID       int64   `json:"queryid"`
	Query         string  `json:"query"`
	CacheHitRatio float64 `json:"cache_hit_ratio"` // 0-1
	BlksHit       int64   `json:"blks_hit"`
	BlksRead      int64   `json:"blks_read"`
	Calls         int64   `json:"calls"`
}

// TableIssue represents a detected issue with a table.
type TableIssue struct {
	SchemaName   string     `json:"schema_name"`
	TableName    string     `json:"table_name"`
	IssueType    string     `json:"issue_type"` // "high_bloat", "stale_vacuum", "stale_analyze", "missing_index"
	Severity     string     `json:"severity"`   // "critical", "warning", "info"
	CurrentValue float64    `json:"current_value"`
	Threshold    float64    `json:"threshold"`
	Description  string     `json:"description"`
	LastVacuum   *time.Time `json:"last_vacuum,omitempty"`
	LastAnalyze  *time.Time `json:"last_analyze,omitempty"`
	NDeadTup     int64      `json:"n_dead_tup,omitempty"`
	NLiveTup     int64      `json:"n_live_tup,omitempty"`
	TableSize    int64      `json:"table_size,omitempty"`
	SeqScanRatio float64    `json:"seq_scan_ratio,omitempty"`
}

// TableIssueType constants.
const (
	TableIssueHighBloat    = "high_bloat"
	TableIssueStaleVacuum  = "stale_vacuum"
	TableIssueStaleAnalyze = "stale_analyze"
	TableIssueMissingIndex = "missing_index"
)

// IndexIssue represents a detected issue with an index.
type IndexIssue struct {
	SchemaName         string `json:"schema_name"`
	TableName          string `json:"table_name"`
	IndexName          string `json:"index_name"`
	IssueType          string `json:"issue_type"` // "unused", "duplicate"
	Severity           string `json:"severity"`   // "critical", "warning", "info"
	Description        string `json:"description"`
	IndexSize          int64  `json:"index_size"`
	IdxScan            int64  `json:"idx_scan"`
	IsUnique           bool   `json:"is_unique"`
	IsPrimary          bool   `json:"is_primary"`
	DuplicateOf        string `json:"duplicate_of,omitempty"`          // for duplicate indexes
	DuplicateOfIdxScan int64  `json:"duplicate_of_idx_scan,omitempty"` // scans on the retained index
	SpaceSavings       int64  `json:"space_savings,omitempty"`         // potential bytes saved
	IndexDef           string `json:"index_def,omitempty"`
	DuplicateOfDef     string `json:"duplicate_of_def,omitempty"`
	// ExactDuplicate distinguishes an identical index from one whose columns are
	// a leading prefix of the retained index.
	ExactDuplicate bool `json:"exact_duplicate,omitempty"`
	// StatsWindow is how long idx_scan has been counting when the index was
	// judged unused.
	StatsWindow time.Duration `json:"stats_window_ns,omitempty"`
}

// IndexIssueType constants.
const (
	IndexIssueUnused    = "unused"
	IndexIssueDuplicate = "duplicate"
)

// ActivityAnalysis contains connection activity analysis.
type ActivityAnalysis struct {
	ConnectionUtilization float64                    `json:"connection_utilization"` // percentage of max_connections used
	TotalConnections      int                        `json:"total_connections"`
	MaxConnections        int                        `json:"max_connections"`
	ActiveCount           int                        `json:"active_count"`
	IdleCount             int                        `json:"idle_count"`
	IdleInTxCount         int                        `json:"idle_in_tx_count"`
	WaitingCount          int                        `json:"waiting_count"`
	LongRunningQueries    []models.LongRunningQuery  `json:"long_running_queries"`
	IdleInTransaction     []models.IdleInTransaction `json:"idle_in_transaction"`
	// Peak is the busiest sample over ConnectionPeakWindow, nil without history.
	Peak *ConnectionPeakAnalysis `json:"peak,omitempty"`
}

// LockAnalysis contains lock-related analysis.
type LockAnalysis struct {
	TotalLocks         int                   `json:"total_locks"`
	GrantedLocks       int                   `json:"granted_locks"`
	WaitingLocks       int                   `json:"waiting_locks"`
	BlockedQueries     []models.BlockedQuery `json:"blocked_queries"`
	LockContentionHigh bool                  `json:"lock_contention_high"`
}

// TransactionAnalysis contains transaction rate analysis.
type TransactionAnalysis struct {
	XactCommit   int64 `json:"xact_commit"`
	XactRollback int64 `json:"xact_rollback"`
	TempFiles    int64 `json:"temp_files"`
	TempBytes    int64 `json:"temp_bytes"`
	Deadlocks    int64 `json:"deadlocks"`
}

// Config holds analyzer configuration derived from thresholds.
type Config struct {
	SlowQueryMs float64 // queries slower than this are flagged
	// SlowQueryWindow is the recent period a query's mean time is judged over.
	// A lifetime mean hides regressions and keeps flagging queries already fixed.
	SlowQueryWindow      time.Duration
	CacheHitRatioWarning float64 // warn below this ratio (0-1)
	BloatPercentWarning  float64 // tables with > this % bloat
	UnusedIndexDays      int     // days without scans
	SeqScanRatioWarning  float64 // seq_scan / total_scan ratio
	MinTableSizeForIndex int64   // skip index suggestions for tiny tables
	VacuumStaleDays      int     // days since last vacuum to consider stale
	AnalyzeStaleDays     int     // days since last analyze to consider stale

	// CollectorIntervals maps a domain (collector name) to how often it is collected.
	// Used to decide when a domain's data is too old to conclude anything from.
	CollectorIntervals map[string]time.Duration
	// StalenessFactor multiplies a domain's collection interval to get its staleness
	// budget. A domain with no known interval is never considered stale.
	StalenessFactor float64
}

// StalenessBudget returns how old a domain's data may be before conclusions drawn
// from its absence become unsafe. Returns 0 when the domain has no known interval.
func (c *Config) StalenessBudget(domain string) time.Duration {
	interval, ok := c.CollectorIntervals[domain]
	if !ok || interval <= 0 {
		return 0
	}
	factor := c.StalenessFactor
	if factor <= 0 {
		factor = DefaultStalenessFactor
	}
	return time.Duration(float64(interval) * factor)
}

// DefaultStalenessFactor is how many collection intervals a domain may lag before
// its data is treated as too old to resolve issues from. Three intervals tolerates
// a couple of missed cycles without letting genuinely stale data drive decisions.
const DefaultStalenessFactor = 3.0

// DefaultSlowQueryWindow covers a full daily cycle, so nightly jobs stay visible
// during the day while a regression still shows within hours.
const DefaultSlowQueryWindow = 24 * time.Hour

// DefaultConfig returns the default analyzer configuration.
func DefaultConfig() *Config {
	return &Config{
		SlowQueryMs:          1000,
		SlowQueryWindow:      DefaultSlowQueryWindow,
		CacheHitRatioWarning: 0.95,
		BloatPercentWarning:  20,
		UnusedIndexDays:      30,
		SeqScanRatioWarning:  0.5,
		MinTableSizeForIndex: 10000,
		VacuumStaleDays:      7,
		AnalyzeStaleDays:     7,
		StalenessFactor:      DefaultStalenessFactor,
	}
}

// ConfigFromThresholds creates an analyzer Config from ThresholdsConfig.
func ConfigFromThresholds(t config.ThresholdsConfig) *Config {
	return &Config{
		SlowQueryMs:          float64(t.SlowQueryMs),
		SlowQueryWindow:      time.Duration(t.SlowQueryWindow),
		CacheHitRatioWarning: t.CacheHitRatioWarning,
		BloatPercentWarning:  float64(t.BloatPercentWarning),
		UnusedIndexDays:      t.UnusedIndexDays,
		SeqScanRatioWarning:  t.SeqScanRatioWarning,
		MinTableSizeForIndex: int64(t.MinTableSizeForIndex),
		VacuumStaleDays:      7, // Default, not in threshold config
		AnalyzeStaleDays:     7, // Default, not in threshold config
		StalenessFactor:      DefaultStalenessFactor,
	}
}

// Storage defines the storage interface needed by the analyzer.
// This is a subset of sqlite.Storage to allow for easier testing.
type Storage interface {
	GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error)
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error)
	GetSnapshotCollectors(ctx context.Context, snapshotID int64) ([]models.SnapshotCollector, error)
	ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error)
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error)
	GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error)
	GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error)
	GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error)
	// Operational stats
	GetConnectionActivity(ctx context.Context, snapshotID int64) (*models.ConnectionActivity, error)
	GetLongRunningQueries(ctx context.Context, snapshotID int64) ([]models.LongRunningQuery, error)
	GetIdleInTransaction(ctx context.Context, snapshotID int64) ([]models.IdleInTransaction, error)
	GetLockStats(ctx context.Context, snapshotID int64) (*models.LockStats, error)
	GetBlockedQueries(ctx context.Context, snapshotID int64) ([]models.BlockedQuery, error)
	GetExtendedDatabaseStats(ctx context.Context, snapshotID int64) (*models.ExtendedDatabaseStats, error)
	// Query plans
	GetQueryPlans(ctx context.Context, snapshotID int64) (*models.QueryPlanReport, error)
	// Outage risk
	GetOutageRisk(ctx context.Context, snapshotID int64) (*models.OutageRisk, error)
	// Server settings
	GetServerSettings(ctx context.Context, snapshotID int64) (*models.ServerSettings, error)
	GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error)
	GetConnectionPeak(ctx context.Context, instanceID int64, since, until time.Time) (*models.ConnectionPeak, error)
}

// Ensure sqlite.SQLiteStorage implements Storage interface.
var _ Storage = (*sqlite.SQLiteStorage)(nil)
