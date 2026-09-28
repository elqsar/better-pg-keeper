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

// RequiredDomains returns the analysis domains this rule reads.
// Scan counts come from index stats.
func (r *UnusedIndexRule) RequiredDomains() []string {
	return []string{analyzer.DomainIndexStats}
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

		days := int(issue.StatsWindow.Hours() / 24)

		// Build description
		var desc strings.Builder
		fmt.Fprintf(&desc, "Index `%s.%s.%s` has had 0 scans in the %d days PostgreSQL has been counting them.\n\n",
			issue.SchemaName, issue.TableName, issue.IndexName, days)

		desc.WriteString("**Index Details:**\n")
		fmt.Fprintf(&desc, "- Table: %s.%s\n", issue.SchemaName, issue.TableName)
		if issue.IndexDef != "" {
			fmt.Fprintf(&desc, "- Definition: `%s`\n", issue.IndexDef)
		}
		fmt.Fprintf(&desc, "- Index size: %s\n", formatBytes(issue.IndexSize))
		fmt.Fprintf(&desc, "- Index scans: %d in %d days\n\n", issue.IdxScan, days)

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("Every write to the table also updates this index, and it takes disk space and cache. ")
		desc.WriteString("If nothing needs it, dropping it speeds up writes.\n\n")
		desc.WriteString("**Before dropping:**\n")
		desc.WriteString("- Scans on read replicas are not visible here. If replicas serve reads, check `pg_stat_user_indexes` on each of them.\n")
		desc.WriteString("- Consider rare jobs that run less often than the window above (quarterly reports, yearly archiving).\n")
		desc.WriteString("- Save the definition above so the index can be recreated if something slows down.\n\n")
		desc.WriteString("`DROP INDEX CONCURRENTLY` does not block reads or writes, but cannot run inside a transaction block:\n")
		fmt.Fprintf(&desc, "```sql\nDROP INDEX CONCURRENTLY %s;\n```\n", quoteQualified(issue.SchemaName, issue.IndexName))

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     issue.Severity,
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
				"stats_days":    days,
				"index_def":     issue.IndexDef,
			},
		})
	}

	return suggestions, nil
}

// quoteQualified renders schema.name with each part quoted as an identifier, so
// generated DDL is safe to paste for mixed-case or unusual names.
func quoteQualified(schema, name string) string {
	return quoteIdent(schema) + "." + quoteIdent(name)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
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
