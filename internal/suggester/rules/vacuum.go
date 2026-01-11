package rules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// VacuumRule generates suggestions for tables with stale vacuum.
type VacuumRule struct {
	staleDaysThreshold int
}

// NewVacuumRule creates a new VacuumRule with the given threshold.
func NewVacuumRule(config *suggester.Config) *VacuumRule {
	return &VacuumRule{
		staleDaysThreshold: config.VacuumStaleDays,
	}
}

// ID returns the rule identifier.
func (r *VacuumRule) ID() string {
	return "stale_vacuum"
}

// Name returns the human-readable rule name.
func (r *VacuumRule) Name() string {
	return "Stale Vacuum Detection"
}

// Evaluate analyzes table issues and generates suggestions for stale vacuum.
func (r *VacuumRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || len(analysis.TableIssues) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, issue := range analysis.TableIssues {
		// Only handle stale vacuum issues
		if issue.IssueType != analyzer.TableIssueStaleVacuum {
			continue
		}

		title := fmt.Sprintf("VACUUM needed on %s.%s", issue.SchemaName, issue.TableName)

		// Build description
		var desc strings.Builder
		fmt.Fprintf(&desc, "Table `%s.%s` has not been vacuumed recently and has accumulated dead tuples.\n\n",
			issue.SchemaName, issue.TableName)

		desc.WriteString("**Table Statistics:**\n")
		if issue.LastVacuum != nil {
			daysSinceVacuum := int(time.Since(*issue.LastVacuum).Hours() / 24)
			fmt.Fprintf(&desc, "- Last vacuum: %s (%d days ago)\n", issue.LastVacuum.Format("2006-01-02 15:04:05"), daysSinceVacuum)
		} else {
			desc.WriteString("- Last vacuum: Never\n")
		}
		fmt.Fprintf(&desc, "- Dead tuples: %d\n", issue.NDeadTup)
		fmt.Fprintf(&desc, "- Live tuples: %d\n", issue.NLiveTup)
		if issue.NLiveTup > 0 {
			fmt.Fprintf(&desc, "- Dead tuple ratio: %.1f%%\n", float64(issue.NDeadTup)/float64(issue.NLiveTup)*100)
		}
		desc.WriteString("\n")

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("Run VACUUM to clean up dead tuples and update statistics:\n\n")
		fmt.Fprintf(&desc, "```sql\nVACUUM ANALYZE %s.%s;\n```\n\n",
			issue.SchemaName, issue.TableName)

		desc.WriteString("**Note:** If autovacuum is enabled, investigate why it hasn't run:\n")
		desc.WriteString("- Check if table is excluded from autovacuum\n")
		desc.WriteString("- Review autovacuum_vacuum_threshold settings\n")
		desc.WriteString("- Check for long-running transactions blocking vacuum\n")

		// Calculate days since vacuum for metadata
		var daysSinceVacuum int
		if issue.LastVacuum != nil {
			daysSinceVacuum = int(time.Since(*issue.LastVacuum).Hours() / 24)
		} else {
			daysSinceVacuum = -1 // Never vacuumed
		}

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     suggester.SeverityWarning,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("%s.%s", issue.SchemaName, issue.TableName),
			Metadata: map[string]any{
				"schema_name":       issue.SchemaName,
				"table_name":        issue.TableName,
				"last_vacuum":       issue.LastVacuum,
				"dead_tuples":       issue.NDeadTup,
				"live_tuples":       issue.NLiveTup,
				"days_since_vacuum": daysSinceVacuum,
			},
		})
	}

	return suggestions, nil
}

// Ensure VacuumRule implements Rule interface.
var _ suggester.Rule = (*VacuumRule)(nil)
