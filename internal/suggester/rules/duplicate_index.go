package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

const duplicateIndexDetectionMethod = "name_and_size_heuristic"

// DuplicateIndexRule generates suggestions for potentially redundant indexes.
type DuplicateIndexRule struct{}

// NewDuplicateIndexRule creates a new DuplicateIndexRule.
func NewDuplicateIndexRule(config *suggester.Config) *DuplicateIndexRule {
	_ = config
	return &DuplicateIndexRule{}
}

// ID returns the rule identifier.
func (r *DuplicateIndexRule) ID() string {
	return "duplicate_index"
}

// Name returns the human-readable rule name.
func (r *DuplicateIndexRule) Name() string {
	return "Duplicate Index Detection"
}

// RequiredDomains returns the analysis domains this rule reads.
// Duplicate detection compares indexes within one index-stats collection.
func (r *DuplicateIndexRule) RequiredDomains() []string {
	return []string{analyzer.DomainIndexStats}
}

// Evaluate analyzes index issues and generates suggestions for potential duplicate indexes.
func (r *DuplicateIndexRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	_ = ctx

	if analysis == nil || len(analysis.IndexIssues) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, issue := range analysis.IndexIssues {
		if issue.IssueType != analyzer.IndexIssueDuplicate {
			continue
		}

		title := fmt.Sprintf("Potential duplicate index: %s", issue.IndexName)

		var desc strings.Builder
		fmt.Fprintf(&desc, "Index `%s.%s.%s` may be redundant with `%s`.\n\n",
			issue.SchemaName, issue.TableName, issue.IndexName, issue.DuplicateOf)

		desc.WriteString("**Duplicate Index Details:**\n")
		fmt.Fprintf(&desc, "- Table: %s.%s\n", issue.SchemaName, issue.TableName)
		fmt.Fprintf(&desc, "- Candidate index scans: %d\n", issue.IdxScan)
		fmt.Fprintf(&desc, "- Retained index scans: %d\n", issue.DuplicateOfIdxScan)
		fmt.Fprintf(&desc, "- Index size: %s\n", formatBytes(issue.IndexSize))
		fmt.Fprintf(&desc, "- Estimated space savings: %s\n", formatBytes(issue.SpaceSavings))
		desc.WriteString("- Detection method: name and size heuristic\n\n")

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("This check is heuristic-based. Verify both index definitions, predicates, and operator classes before dropping anything.\n\n")
		fmt.Fprintf(&desc, "```sql\nDROP INDEX %s.%s;\n```\n\n", issue.SchemaName, issue.IndexName)
		desc.WriteString("**Before dropping:**\n")
		desc.WriteString("- Compare index definitions in PostgreSQL\n")
		desc.WriteString("- Confirm the retained index supports the same query patterns\n")
		desc.WriteString("- Review recent execution plans during representative traffic\n")

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     suggester.SeverityInfo,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("%s.%s.%s", issue.SchemaName, issue.TableName, issue.IndexName),
			Metadata: map[string]any{
				"schema_name":           issue.SchemaName,
				"table_name":            issue.TableName,
				"index_name":            issue.IndexName,
				"duplicate_of":          issue.DuplicateOf,
				"idx_scan":              issue.IdxScan,
				"duplicate_of_idx_scan": issue.DuplicateOfIdxScan,
				"index_size":            issue.IndexSize,
				"space_savings":         issue.SpaceSavings,
				"detection_method":      duplicateIndexDetectionMethod,
			},
		})
	}

	return suggestions, nil
}

// Ensure DuplicateIndexRule implements Rule interface.
var _ suggester.Rule = (*DuplicateIndexRule)(nil)
