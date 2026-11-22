package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

const maintenanceBaselineWindow = time.Hour

// analyzeMaintenance compares table counters against a sample at least an hour
// old. A single high counter is not evidence that autovacuum is stuck.
func (a *MainAnalyzer) analyzeMaintenance(ctx context.Context, result *AnalysisResult) error {
	result.MaintenanceIssues = nil
	result.MaintenanceBaselineObserved = false
	result.MaintenanceUnobservedTargets = nil
	currentID := snapshotFor(result.Coverage, DomainTableStats)
	if currentID == 0 || !result.DomainsUsable(DomainTableStats) {
		return nil
	}
	currentSnapshot, err := a.storage.GetSnapshotByID(ctx, currentID)
	if err != nil {
		return fmt.Errorf("current table snapshot: %w", err)
	}
	if currentSnapshot == nil {
		return fmt.Errorf("table snapshot %d disappeared", currentID)
	}
	current, err := a.storage.GetTableStats(ctx, currentID)
	if err != nil {
		return fmt.Errorf("current table stats: %w", err)
	}
	baselineSnapshot, err := a.storage.GetLatestSnapshotWithCollector(ctx, result.InstanceID, DomainTableStats, currentSnapshot.CapturedAt.Add(-maintenanceBaselineWindow))
	if err != nil {
		return fmt.Errorf("table baseline: %w", err)
	}
	var baseline map[string]models.TableStat
	if baselineSnapshot != nil && baselineSnapshot.ID != currentID && sameReset(baselineSnapshot.StatsReset, currentSnapshot.StatsReset) {
		old, err := a.storage.GetTableStats(ctx, baselineSnapshot.ID)
		if err != nil {
			return fmt.Errorf("baseline table stats: %w", err)
		}
		baseline = make(map[string]models.TableStat, len(old))
		for _, t := range old {
			baseline[tableKey(t.SchemaName, t.RelName)] = t
		}
		for _, t := range old {
			if t.VacuumTrigger != nil && t.AnalyzeTrigger != nil {
				result.MaintenanceBaselineObserved = true
				break
			}
		}
	}

	// The existing bloat and stale-maintenance suggestions remain the primary
	// finding when they already explain the same table.
	covered := make(map[string]bool)
	for _, issue := range result.TableIssues {
		switch issue.IssueType {
		case TableIssueHighBloat, TableIssueStaleVacuum, TableIssueStaleAnalyze:
			covered[tableKey(issue.SchemaName, issue.TableName)] = true
		}
	}
	for _, t := range current {
		key := tableKey(t.SchemaName, t.RelName)
		if t.AutovacuumDisabled {
			// With autovacuum off, a backlog is expected and trigger tuning is
			// moot. The configuration rule reports the cause and its fix, so
			// these targets count as observed and an earlier finding resolves.
			continue
		}
		if result.MaintenanceBaselineObserved {
			previous, ok := baseline[key]
			if !ok || previous.VacuumTrigger == nil || t.VacuumTrigger == nil {
				result.MaintenanceUnobservedTargets = append(result.MaintenanceUnobservedTargets, "vacuum:"+t.SchemaName+"."+t.RelName)
			}
			if !ok || previous.AnalyzeTrigger == nil || t.AnalyzeTrigger == nil {
				result.MaintenanceUnobservedTargets = append(result.MaintenanceUnobservedTargets, "analyze:"+t.SchemaName+"."+t.RelName)
			}
		}
		if covered[key] {
			continue
		}
		if t.HeapBytes < 64<<20 {
			continue
		}
		if result.MaintenanceBaselineObserved {
			if previous, ok := baseline[key]; ok {
				if issue := maintenanceBacklog(t, previous); issue != nil {
					result.MaintenanceIssues = append(result.MaintenanceIssues, *issue)
					continue
				}
			}
		}
		if issue := maintenanceTuning(t); issue != nil {
			result.MaintenanceIssues = append(result.MaintenanceIssues, *issue)
		}
	}
	return nil
}

func tableKey(schema, table string) string { return schema + "\x00" + table }

func maintenanceBacklog(now, before models.TableStat) *TableIssue {
	if now.VacuumTrigger != nil && before.VacuumTrigger != nil &&
		*now.VacuumTrigger > 0 && *before.VacuumTrigger > 0 &&
		now.NDeadTup >= 10000 &&
		float64(now.NDeadTup) > 2*(*now.VacuumTrigger) &&
		float64(before.NDeadTup) > 2*(*before.VacuumTrigger) &&
		now.VacuumCount == before.VacuumCount &&
		now.AutovacuumCount == before.AutovacuumCount {
		return &TableIssue{
			SchemaName: now.SchemaName, TableName: now.RelName,
			IssueType: TableIssueVacuumBacklog, Severity: models.SeverityWarning,
			NDeadTup: now.NDeadTup, NLiveTup: now.NLiveTup,
			HeapBytes: now.HeapBytes, VacuumTrigger: now.VacuumTrigger,
		}
	}
	if now.AnalyzeTrigger != nil && before.AnalyzeTrigger != nil &&
		*now.AnalyzeTrigger > 0 && *before.AnalyzeTrigger > 0 &&
		now.NModSinceAnalyze >= 10000 &&
		float64(now.NModSinceAnalyze) > 2*(*now.AnalyzeTrigger) &&
		float64(before.NModSinceAnalyze) > 2*(*before.AnalyzeTrigger) &&
		now.AnalyzeCount == before.AnalyzeCount &&
		now.AutoanalyzeCount == before.AutoanalyzeCount {
		return &TableIssue{
			SchemaName: now.SchemaName, TableName: now.RelName,
			IssueType: TableIssueAnalyzeBacklog, Severity: models.SeverityWarning,
			NModSinceAnalyze: now.NModSinceAnalyze, AnalyzeTrigger: now.AnalyzeTrigger,
			HeapBytes: now.HeapBytes,
		}
	}
	return nil
}

func maintenanceTuning(t models.TableStat) *TableIssue {
	if t.HeapBytes < 1<<30 || t.RelTuples <= 0 || t.VacuumTrigger == nil ||
		*t.VacuumTrigger <= 100000 || *t.VacuumTrigger <= 0.1*t.RelTuples {
		return nil
	}
	var deadBytes int64
	if t.NDeadTup > 0 && t.NLiveTup+t.NDeadTup > 0 {
		deadBytes = int64(float64(t.HeapBytes) * float64(t.NDeadTup) / float64(t.NLiveTup+t.NDeadTup))
	}
	if deadBytes < 128<<20 {
		return nil
	}
	return &TableIssue{
		SchemaName: t.SchemaName, TableName: t.RelName,
		IssueType: TableIssueAutovacuumTuning, Severity: models.SeverityInfo,
		NDeadTup: t.NDeadTup, NLiveTup: t.NLiveTup, HeapBytes: t.HeapBytes,
		DeadTupleBytes: &deadBytes, MeasurementSource: "stats_estimate",
		VacuumTrigger: t.VacuumTrigger, VacuumScaleFactor: t.VacuumScaleFactor,
	}
}
