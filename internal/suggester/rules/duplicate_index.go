package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

const duplicateIndexDetectionMethod = "index_definition"

// DuplicateIndexRule generates suggestions for indexes made redundant by another index.
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

		// An identical index is pure overhead. A prefix index can still be
		// marginally faster for queries it serves, being smaller.
		severity := suggester.SeverityInfo
		title := fmt.Sprintf("Redundant index: %s is covered by %s", issue.IndexName, issue.DuplicateOf)
		if issue.ExactDuplicate {
			severity = suggester.SeverityWarning
			title = fmt.Sprintf("Duplicate index: %s is identical to %s", issue.IndexName, issue.DuplicateOf)
		}

		var desc strings.Builder
		if issue.ExactDuplicate {
			fmt.Fprintf(&desc, "Index `%s.%s.%s` has the same definition as `%s`. PostgreSQL maintains both on every write but only needs one.\n\n",
				issue.SchemaName, issue.TableName, issue.IndexName, issue.DuplicateOf)
		} else {
			fmt.Fprintf(&desc, "The columns of index `%s.%s.%s` are the leading columns of `%s`, which can serve the same lookups.\n\n",
				issue.SchemaName, issue.TableName, issue.IndexName, issue.DuplicateOf)
		}

		desc.WriteString("**Definitions:**\n")
		fmt.Fprintf(&desc, "- Candidate: `%s`\n", issue.IndexDef)
		fmt.Fprintf(&desc, "- Retained: `%s`\n\n", issue.DuplicateOfDef)

		desc.WriteString("**Usage:**\n")
		fmt.Fprintf(&desc, "- Candidate index scans: %d\n", issue.IdxScan)
		fmt.Fprintf(&desc, "- Retained index scans: %d\n", issue.DuplicateOfIdxScan)
		fmt.Fprintf(&desc, "- Space saved by dropping: %s\n\n", formatBytes(issue.SpaceSavings))

		desc.WriteString("**Recommendation:**\n")
		if issue.ExactDuplicate {
			desc.WriteString("Drop the candidate. Queries that used it will use the retained index, which is identical.\n\n")
		} else {
			desc.WriteString("Dropping the candidate is usually safe: queries that used it will use the retained index instead. ")
			desc.WriteString("That index is wider, so those queries may read slightly more pages. Check the most frequent queries on this table afterwards.\n\n")
		}
		desc.WriteString("`DROP INDEX CONCURRENTLY` does not block reads or writes, but cannot run inside a transaction block:\n")
		fmt.Fprintf(&desc, "```sql\nDROP INDEX CONCURRENTLY %s;\n```\n", quoteQualified(issue.SchemaName, issue.IndexName))

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
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
				"exact_duplicate":       issue.ExactDuplicate,
				"index_def":             issue.IndexDef,
				"duplicate_of_def":      issue.DuplicateOfDef,
			},
		})
	}

	return suggestions, nil
}

// Ensure DuplicateIndexRule implements Rule interface.
var _ suggester.Rule = (*DuplicateIndexRule)(nil)
