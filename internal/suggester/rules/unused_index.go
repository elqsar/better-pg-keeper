package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// UnusedIndexRule generates suggestions for indexes that have zero scans.
type UnusedIndexRule struct {
	unusedDaysThreshold int
}

// NewUnusedIndexRule creates a new UnusedIndexRule with the given threshold.
func NewUnusedIndexRule(config *suggester.Config) *UnusedIndexRule {
	return &UnusedIndexRule{
		unusedDaysThreshold: config.UnusedIndexDays,
	}
}

// ID returns the rule identifier.
func (r *UnusedIndexRule) ID() string {
	return "unused_index"
}

// Name returns the human-readable rule name.
func (r *UnusedIndexRule) Name() string {
	return "Unused Index Detection"
}

// Evaluate analyzes index issues and generates suggestions for unused indexes.
func (r *UnusedIndexRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || len(analysis.IndexIssues) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, issue := range analysis.IndexIssues {
		// Only handle unused indexes
		if issue.IssueType != analyzer.IndexIssueUnused {
			continue
		}

		// Skip primary keys and unique indexes (used for constraints)
		if issue.IsPrimary || issue.IsUnique {
			continue
		}

		title := fmt.Sprintf("Unused index: %s", issue.IndexName)

		// Build description
		var desc strings.Builder
		fmt.Fprintf(&desc, "Index `%s.%s.%s` has 0 scans since statistics were reset.\n\n",
			issue.SchemaName, issue.TableName, issue.IndexName)

		desc.WriteString("**Index Details:**\n")
		fmt.Fprintf(&desc, "- Table: %s.%s\n", issue.SchemaName, issue.TableName)
		fmt.Fprintf(&desc, "- Index size: %s\n", formatBytes(issue.IndexSize))
		fmt.Fprintf(&desc, "- Index scans: %d\n\n", issue.IdxScan)

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("Consider dropping this index to save disk space and reduce write overhead.\n\n")
		fmt.Fprintf(&desc, "```sql\nDROP INDEX %s.%s;\n```\n\n", issue.SchemaName, issue.IndexName)
		desc.WriteString("**Before dropping:**\n")
		desc.WriteString("- Verify the index is truly unused (check during peak hours)\n")
		desc.WriteString("- Check if it's needed for foreign key lookups\n")
		desc.WriteString("- Confirm no application changes are pending that might need this index\n")

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     suggester.SeverityWarning,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("%s.%s.%s", issue.SchemaName, issue.TableName, issue.IndexName),
			Metadata: map[string]any{
				"schema_name":   issue.SchemaName,
				"table_name":    issue.TableName,
				"index_name":    issue.IndexName,
				"index_size":    issue.IndexSize,
				"idx_scan":      issue.IdxScan,
				"is_unique":     issue.IsUnique,
				"is_primary":    issue.IsPrimary,
				"space_savings": issue.SpaceSavings,
			},
		})
	}

	return suggestions, nil
}

// formatBytes formats bytes into a human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Ensure UnusedIndexRule implements Rule interface.
var _ suggester.Rule = (*UnusedIndexRule)(nil)
