package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// TableAnalyzer detects issues with tables such as bloat, stale vacuum/analyze,
// and missing indexes.
type TableAnalyzer struct {
	storage Storage
	config  *Config
}

// NewTableAnalyzer creates a new TableAnalyzer.
func NewTableAnalyzer(storage Storage, cfg *Config) *TableAnalyzer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &TableAnalyzer{
		storage: storage,
		config:  cfg,
	}
}

// Analyze detects table-related issues from the given snapshot.
func (a *TableAnalyzer) Analyze(ctx context.Context, snapshotID int64) ([]TableIssue, error) {
	// Get table stats
	tableStats, err := a.storage.GetTableStats(ctx, snapshotID)
	if err != nil {
		return nil, err
	}

	// Get bloat stats
	bloatStats, err := a.storage.GetBloatStats(ctx, snapshotID)
	if err != nil {
		// Bloat stats might not be available; continue with other checks
		bloatStats = nil
	}

	// Build a map of bloat info by table
	bloatMap := make(map[string]models.BloatInfo)
	for _, b := range bloatStats {
		key := b.SchemaName + "." + b.RelName
		bloatMap[key] = b
	}

	var issues []TableIssue
	now := time.Now()

	for _, stat := range tableStats {
		tableKey := stat.SchemaName + "." + stat.RelName

		// Check for high bloat
		if bloat, ok := bloatMap[tableKey]; ok {
			if bloat.BloatPercent >= a.config.BloatPercentWarning {
				severity := models.SeverityWarning
				if bloat.BloatPercent >= a.config.BloatPercentWarning*2 {
					severity = models.SeverityCritical
				}

				issues = append(issues, TableIssue{
					SchemaName:   stat.SchemaName,
					TableName:    stat.RelName,
					IssueType:    TableIssueHighBloat,
					Severity:     severity,
					CurrentValue: bloat.BloatPercent,
					Threshold:    a.config.BloatPercentWarning,
					Description: fmt.Sprintf(
						"Table has %.1f%% dead tuples (%d dead / %d live). Consider running VACUUM.",
						bloat.BloatPercent, bloat.NDeadTup, bloat.NLiveTup,
					),
					NDeadTup:  bloat.NDeadTup,
					NLiveTup:  bloat.NLiveTup,
					TableSize: stat.TableSize,
				})
			}
		}

		// Check for stale vacuum
		if issue := a.checkStaleVacuum(stat, now); issue != nil {
			issues = append(issues, *issue)
		}

		// Check for stale analyze
		if issue := a.checkStaleAnalyze(stat, now); issue != nil {
			issues = append(issues, *issue)
		}

		// Check for potential missing index (high seq scan ratio on large tables)
		if issue := a.checkMissingIndex(stat); issue != nil {
			issues = append(issues, *issue)
		}
	}

	return issues, nil
}

// checkStaleVacuum checks if a table has a stale vacuum.
func (a *TableAnalyzer) checkStaleVacuum(stat models.TableStat, now time.Time) *TableIssue {
	// Skip small tables
	if stat.NLiveTup < 1000 {
		return nil
	}

	// Skip if no dead tuples
	if stat.NDeadTup == 0 {
		return nil
	}

	staleDays := a.config.VacuumStaleDays
	staleThreshold := now.AddDate(0, 0, -staleDays)

	// Get the most recent vacuum time
	var lastVacuum *time.Time
	if stat.LastAutovacuum != nil {
		lastVacuum = stat.LastAutovacuum
	}
	if stat.LastVacuum != nil {
		if lastVacuum == nil || stat.LastVacuum.After(*lastVacuum) {
			lastVacuum = stat.LastVacuum
		}
	}

	// If never vacuumed and has dead tuples, that's an issue
	if lastVacuum == nil {
		if stat.NDeadTup > 1000 { // Only flag if significant dead tuples
			return &TableIssue{
				SchemaName:   stat.SchemaName,
				TableName:    stat.RelName,
				IssueType:    TableIssueStaleVacuum,
				Severity:     models.SeverityWarning,
				CurrentValue: float64(staleDays + 1), // More than threshold
				Threshold:    float64(staleDays),
				Description: fmt.Sprintf(
					"Table has never been vacuumed and has %d dead tuples. Run VACUUM to reclaim space.",
					stat.NDeadTup,
				),
				NDeadTup:  stat.NDeadTup,
				NLiveTup:  stat.NLiveTup,
				TableSize: stat.TableSize,
			}
		}
		return nil
	}

	// Check if vacuum is stale
	if lastVacuum.Before(staleThreshold) && stat.NDeadTup > 1000 {
		daysSinceVacuum := int(now.Sub(*lastVacuum).Hours() / 24)
		severity := models.SeverityWarning
		if daysSinceVacuum > staleDays*2 {
			severity = models.SeverityCritical
		}

		return &TableIssue{
			SchemaName:   stat.SchemaName,
			TableName:    stat.RelName,
			IssueType:    TableIssueStaleVacuum,
			Severity:     severity,
			CurrentValue: float64(daysSinceVacuum),
			Threshold:    float64(staleDays),
			Description: fmt.Sprintf(
				"Table hasn't been vacuumed in %d days and has %d dead tuples. Consider running VACUUM.",
				daysSinceVacuum, stat.NDeadTup,
			),
			LastVacuum: lastVacuum,
			NDeadTup:   stat.NDeadTup,
			NLiveTup:   stat.NLiveTup,
			TableSize:  stat.TableSize,
		}
	}

	return nil
}

// checkStaleAnalyze checks if a table has stale statistics.
func (a *TableAnalyzer) checkStaleAnalyze(stat models.TableStat, now time.Time) *TableIssue {
	// Skip small tables
	if stat.NLiveTup < 1000 {
		return nil
	}

	staleDays := a.config.AnalyzeStaleDays
	staleThreshold := now.AddDate(0, 0, -staleDays)

	// If never analyzed and has rows, that's an issue
	if stat.LastAnalyze == nil {
		return &TableIssue{
			SchemaName:   stat.SchemaName,
			TableName:    stat.RelName,
			IssueType:    TableIssueStaleAnalyze,
			Severity:     models.SeverityWarning,
			CurrentValue: float64(staleDays + 1),
			Threshold:    float64(staleDays),
			Description: fmt.Sprintf(
				"Table has never been analyzed. Run ANALYZE to update planner statistics for %d rows.",
				stat.NLiveTup,
			),
			NLiveTup:  stat.NLiveTup,
			TableSize: stat.TableSize,
		}
	}

	// Check if analyze is stale
	if stat.LastAnalyze.Before(staleThreshold) {
		daysSinceAnalyze := int(now.Sub(*stat.LastAnalyze).Hours() / 24)

		return &TableIssue{
			SchemaName:   stat.SchemaName,
			TableName:    stat.RelName,
			IssueType:    TableIssueStaleAnalyze,
			Severity:     models.SeverityInfo,
			CurrentValue: float64(daysSinceAnalyze),
			Threshold:    float64(staleDays),
			Description: fmt.Sprintf(
				"Table statistics are %d days old. Consider running ANALYZE for better query plans.",
				daysSinceAnalyze,
			),
			LastAnalyze: stat.LastAnalyze,
			NLiveTup:    stat.NLiveTup,
			TableSize:   stat.TableSize,
		}
	}

	return nil
}

// checkMissingIndex checks if a table might benefit from an index.
func (a *TableAnalyzer) checkMissingIndex(stat models.TableStat) *TableIssue {
	// Skip small tables
	if stat.NLiveTup < a.config.MinTableSizeForIndex {
		return nil
	}

	// Skip if no scans at all
	totalScans := stat.SeqScan + stat.IdxScan
	if totalScans == 0 {
		return nil
	}

	// Calculate sequential scan ratio
	seqScanRatio := float64(stat.SeqScan) / float64(totalScans)

	// Flag if sequential scan ratio is above threshold
	if seqScanRatio < a.config.SeqScanRatioWarning {
		return nil
	}

	// Only flag if there's significant sequential scan activity
	if stat.SeqScan < 100 {
		return nil
	}

	severity := models.SeverityInfo
	if seqScanRatio > 0.8 && stat.SeqScan > 1000 {
		severity = models.SeverityWarning
	}

	return &TableIssue{
		SchemaName:   stat.SchemaName,
		TableName:    stat.RelName,
		IssueType:    TableIssueMissingIndex,
		Severity:     severity,
		CurrentValue: seqScanRatio * 100,
		Threshold:    a.config.SeqScanRatioWarning * 100,
		Description: fmt.Sprintf(
			"Table has %.1f%% sequential scans (%d seq vs %d idx). Consider adding an index for frequent query patterns.",
			seqScanRatio*100, stat.SeqScan, stat.IdxScan,
		),
		SeqScanRatio: seqScanRatio,
		NLiveTup:     stat.NLiveTup,
		TableSize:    stat.TableSize,
	}
}
