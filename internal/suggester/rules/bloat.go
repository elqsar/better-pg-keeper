package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/suggester"
)

// BloatRule generates suggestions for tables with high dead tuple ratios.
type BloatRule struct {
	warningThreshold  float64
	criticalThreshold float64
}

// NewBloatRule creates a new BloatRule with the given thresholds.
func NewBloatRule(config *suggester.Config) *BloatRule {
	return &BloatRule{
		warningThreshold:  config.BloatPercentWarning,
		criticalThreshold: config.BloatPercentCritical,
	}
}

// ID returns the rule identifier.
func (r *BloatRule) ID() string {
	return "table_bloat"
}

// Name returns the human-readable rule name.
func (r *BloatRule) Name() string {
	return "Table Bloat Detection"
}

// Evaluate analyzes table issues and generates suggestions for high bloat.
func (r *BloatRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || len(analysis.TableIssues) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, issue := range analysis.TableIssues {
		// Only handle high bloat issues
		if issue.IssueType != analyzer.TableIssueHighBloat {
			continue
		}

		// Determine severity based on bloat percentage
		severity := suggester.SeverityWarning
		if issue.CurrentValue >= r.criticalThreshold {
			severity = suggester.SeverityCritical
		}

		title := fmt.Sprintf("High bloat on %s.%s", issue.SchemaName, issue.TableName)

		// Calculate dead tuple ratio
		deadTupleRatio := issue.CurrentValue

		// Build description
		var desc strings.Builder
		fmt.Fprintf(&desc, "Table `%s.%s` has %.1f%% dead tuples (threshold: %.0f%%).\n\n",
			issue.SchemaName, issue.TableName, deadTupleRatio, r.warningThreshold)

		desc.WriteString("**Table Statistics:**\n")
		fmt.Fprintf(&desc, "- Dead tuples: %d\n", issue.NDeadTup)
		fmt.Fprintf(&desc, "- Live tuples: %d\n", issue.NLiveTup)
		fmt.Fprintf(&desc, "- Dead tuple ratio: %.1f%%\n", deadTupleRatio)
		if issue.TableSize > 0 {
			fmt.Fprintf(&desc, "- Table size: %s\n", formatBytes(issue.TableSize))
		}
		desc.WriteString("\n")

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("High bloat indicates many dead tuples that need to be cleaned up.\n\n")

		if deadTupleRatio >= r.criticalThreshold {
			desc.WriteString("**Critical bloat level detected!** Consider running VACUUM FULL:\n")
			fmt.Fprintf(&desc, "```sql\n-- Warning: VACUUM FULL locks the table\nVACUUM FULL %s.%s;\n```\n\n",
				issue.SchemaName, issue.TableName)
		} else {
			desc.WriteString("Run VACUUM to reclaim space:\n")
			fmt.Fprintf(&desc, "```sql\nVACUUM ANALYZE %s.%s;\n```\n\n",
				issue.SchemaName, issue.TableName)
		}

		desc.WriteString("**To prevent future bloat:**\n")
		desc.WriteString("- Review autovacuum settings for this table\n")
		desc.WriteString("- Consider more aggressive autovacuum thresholds\n")
		desc.WriteString("- Investigate if long-running transactions are blocking cleanup\n")

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("%s.%s", issue.SchemaName, issue.TableName),
			Metadata: map[string]any{
				"schema_name":  issue.SchemaName,
				"table_name":   issue.TableName,
				"dead_tuples":  issue.NDeadTup,
				"live_tuples":  issue.NLiveTup,
				"bloat_ratio":  deadTupleRatio,
				"table_size":   issue.TableSize,
			},
		})
	}

	return suggestions, nil
}

// Ensure BloatRule implements Rule interface.
var _ suggester.Rule = (*BloatRule)(nil)
