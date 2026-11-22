package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// MaintenanceRule turns persistent vacuum/analyze backlogs and unusually high
// per-table triggers into advice. It never treats absent history as recovery.
type MaintenanceRule struct{}

func NewMaintenanceRule() *MaintenanceRule         { return &MaintenanceRule{} }
func (*MaintenanceRule) ID() string                { return "table_maintenance" }
func (*MaintenanceRule) Name() string              { return "Table maintenance diagnostics" }
func (*MaintenanceRule) RequiredDomains() []string { return []string{analyzer.DomainTableStats} }

func (*MaintenanceRule) UnobservedTargets(a *analyzer.AnalysisResult) []string {
	if a != nil && a.MaintenanceBaselineObserved {
		return a.MaintenanceUnobservedTargets
	}
	return []string{"vacuum:", "analyze:"}
}

func (r *MaintenanceRule) Evaluate(_ context.Context, a *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if a == nil {
		return nil, nil
	}
	var out []suggester.Suggestion
	for _, issue := range a.MaintenanceIssues {
		name := quoteQualified(issue.SchemaName, issue.TableName)
		var title, target string
		severity := suggester.SeverityWarning
		var description strings.Builder
		switch issue.IssueType {
		case analyzer.TableIssueVacuumBacklog:
			title = "Autovacuum backlog on " + issue.SchemaName + "." + issue.TableName
			target = "vacuum:" + issue.SchemaName + "." + issue.TableName
			fmt.Fprintf(&description, "The estimated dead rows (%d) stayed above twice the effective autovacuum trigger (%.0f) for at least one hour, with no vacuum count increase.\n\n", issue.NDeadTup, *issue.VacuumTrigger)
			description.WriteString("Check for long transactions or an autovacuum already in progress, then investigate why cleanup is waiting. If maintenance is needed now, run as the table owner during a suitable period:\n")
			fmt.Fprintf(&description, "```sql\nVACUUM (ANALYZE) %s;\n```\n", name)
		case analyzer.TableIssueAnalyzeBacklog:
			title = "Analyze backlog on " + issue.SchemaName + "." + issue.TableName
			target = "analyze:" + issue.SchemaName + "." + issue.TableName
			fmt.Fprintf(&description, "Estimated modifications since ANALYZE (%d) stayed above twice the effective autoanalyze trigger (%.0f) for at least one hour, with no analyze count increase. Stale planner statistics can produce poor plans.\n\n", issue.NModSinceAnalyze, *issue.AnalyzeTrigger)
			fmt.Fprintf(&description, "Check autovacuum activity and run `ANALYZE %s;` if statistics need updating now.\n", name)
		case analyzer.TableIssueAutovacuumTuning:
			severity = suggester.SeverityInfo
			title = "Review autovacuum trigger on " + issue.SchemaName + "." + issue.TableName
			target = "tuning:" + issue.SchemaName + "." + issue.TableName
			scale := 0.05
			if issue.VacuumScaleFactor != nil && *issue.VacuumScaleFactor < scale {
				scale = *issue.VacuumScaleFactor
			}
			fmt.Fprintf(&description, "This %.1f GiB heap has roughly %s of dead tuples by built-in statistics, while its effective autovacuum trigger is %.0f rows. The trigger exceeds 10%% of estimated rows, so cleanup can start late on this large table.\n\n", float64(issue.HeapBytes)/(1<<30), formatBytes(*issue.DeadTupleBytes), *issue.VacuumTrigger)
			description.WriteString("Consider a lower table-specific trigger, then watch vacuum frequency and I/O before tuning further:\n")
			fmt.Fprintf(&description, "```sql\nALTER TABLE %s SET (autovacuum_vacuum_threshold = 1000, autovacuum_vacuum_scale_factor = %.3f);\n```\n", name, scale)
		default:
			continue
		}
		out = append(out, suggester.Suggestion{
			RuleID: r.ID(), Severity: severity, Title: title,
			Description: description.String(), TargetObject: target,
			Metadata: map[string]any{
				"schema": issue.SchemaName, "table": issue.TableName,
				"issue_type": issue.IssueType,
			},
		})
	}
	return out, nil
}

var _ suggester.Rule = (*MaintenanceRule)(nil)
var _ suggester.PartiallyObserved = (*MaintenanceRule)(nil)
