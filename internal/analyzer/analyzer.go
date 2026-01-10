// Package analyzer provides analysis logic for identifying performance issues
// in PostgreSQL databases based on collected metrics.
package analyzer

import (
	"context"
	"time"

	"github.com/user/pganalyzer/internal/config"
	"github.com/user/pganalyzer/internal/models"
	"github.com/user/pganalyzer/internal/storage/sqlite"
)

// Analyzer defines the interface for analyzing PostgreSQL performance data.
type Analyzer interface {
	// Analyze processes a snapshot and returns analysis results.
	Analyze(ctx context.Context, snapshotID int64) (*AnalysisResult, error)
}

// AnalysisResult contains all analysis findings from a snapshot.
type AnalysisResult struct {
	SnapshotID   int64            `json:"snapshot_id"`
	InstanceID   int64            `json:"instance_id"`
	AnalyzedAt   time.Time        `json:"analyzed_at"`
	SlowQueries  []SlowQuery      `json:"slow_queries"`
	CacheStats   *CacheAnalysis   `json:"cache_stats"`
	TableIssues  []TableIssue     `json:"table_issues"`
	IndexIssues  []IndexIssue     `json:"index_issues"`
	ErrorCount   int              `json:"error_count"`
	Errors       []string         `json:"errors,omitempty"`
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
}

// CacheAnalysis contains database-level and query-level cache statistics.
type CacheAnalysis struct {
	OverallHitRatio   float64               `json:"overall_hit_ratio"` // 0-100 percentage
	BelowThreshold    bool                  `json:"below_threshold"`
	Threshold         float64               `json:"threshold"` // configured threshold
	PoorCacheQueries  []PoorCacheQuery      `json:"poor_cache_queries,omitempty"`
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
	SchemaName    string    `json:"schema_name"`
	TableName     string    `json:"table_name"`
	IssueType     string    `json:"issue_type"` // "high_bloat", "stale_vacuum", "stale_analyze", "missing_index"
	Severity      string    `json:"severity"`   // "critical", "warning", "info"
	CurrentValue  float64   `json:"current_value"`
	Threshold     float64   `json:"threshold"`
	Description   string    `json:"description"`
	LastVacuum    *time.Time `json:"last_vacuum,omitempty"`
	LastAnalyze   *time.Time `json:"last_analyze,omitempty"`
	NDeadTup      int64     `json:"n_dead_tup,omitempty"`
	NLiveTup      int64     `json:"n_live_tup,omitempty"`
	TableSize     int64     `json:"table_size,omitempty"`
	SeqScanRatio  float64   `json:"seq_scan_ratio,omitempty"`
}

// TableIssueType constants.
const (
	TableIssueHighBloat     = "high_bloat"
	TableIssueStaleVacuum   = "stale_vacuum"
	TableIssueStaleAnalyze  = "stale_analyze"
	TableIssueMissingIndex  = "missing_index"
)

// IndexIssue represents a detected issue with an index.
type IndexIssue struct {
	SchemaName     string `json:"schema_name"`
	TableName      string `json:"table_name"`
	IndexName      string `json:"index_name"`
	IssueType      string `json:"issue_type"` // "unused", "duplicate"
	Severity       string `json:"severity"`   // "critical", "warning", "info"
	Description    string `json:"description"`
	IndexSize      int64  `json:"index_size"`
	IdxScan        int64  `json:"idx_scan"`
	IsUnique       bool   `json:"is_unique"`
	IsPrimary      bool   `json:"is_primary"`
	DuplicateOf    string `json:"duplicate_of,omitempty"`  // for duplicate indexes
	SpaceSavings   int64  `json:"space_savings,omitempty"` // potential bytes saved
}

// IndexIssueType constants.
const (
	IndexIssueUnused    = "unused"
	IndexIssueDuplicate = "duplicate"
)

// Config holds analyzer configuration derived from thresholds.
type Config struct {
	SlowQueryMs          float64       // queries slower than this are flagged
	CacheHitRatioWarning float64       // warn below this ratio (0-1)
	BloatPercentWarning  float64       // tables with > this % bloat
	UnusedIndexDays      int           // days without scans
	SeqScanRatioWarning  float64       // seq_scan / total_scan ratio
	MinTableSizeForIndex int64         // skip index suggestions for tiny tables
	VacuumStaleDays      int           // days since last vacuum to consider stale
	AnalyzeStaleDays     int           // days since last analyze to consider stale
}

// DefaultConfig returns the default analyzer configuration.
func DefaultConfig() *Config {
	return &Config{
		SlowQueryMs:          1000,
		CacheHitRatioWarning: 0.95,
		BloatPercentWarning:  20,
		UnusedIndexDays:      30,
		SeqScanRatioWarning:  0.5,
		MinTableSizeForIndex: 10000,
		VacuumStaleDays:      7,
		AnalyzeStaleDays:     7,
	}
}

// ConfigFromThresholds creates an analyzer Config from ThresholdsConfig.
func ConfigFromThresholds(t config.ThresholdsConfig) *Config {
	return &Config{
		SlowQueryMs:          float64(t.SlowQueryMs),
		CacheHitRatioWarning: t.CacheHitRatioWarning,
		BloatPercentWarning:  float64(t.BloatPercentWarning),
		UnusedIndexDays:      t.UnusedIndexDays,
		SeqScanRatioWarning:  t.SeqScanRatioWarning,
		MinTableSizeForIndex: int64(t.MinTableSizeForIndex),
		VacuumStaleDays:      7,  // Default, not in threshold config
		AnalyzeStaleDays:     7,  // Default, not in threshold config
	}
}

// Storage defines the storage interface needed by the analyzer.
// This is a subset of sqlite.Storage to allow for easier testing.
type Storage interface {
	GetSnapshotByID(ctx context.Context, id int64) (*models.Snapshot, error)
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error)
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error)
	GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error)
	GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error)
	GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error)
}

// Ensure sqlite.SQLiteStorage implements Storage interface.
var _ Storage = (*sqlite.SQLiteStorage)(nil)
