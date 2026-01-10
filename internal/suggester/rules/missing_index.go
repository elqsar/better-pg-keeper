package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/suggester"
)

// MissingIndexRule generates suggestions for tables with high sequential scan ratios.
type MissingIndexRule struct {
	warningRatio     float64
	criticalRatio    float64
	minTableSize     int64
}

// NewMissingIndexRule creates a new MissingIndexRule with the given thresholds.
func NewMissingIndexRule(config *suggester.Config) *MissingIndexRule {
	return &MissingIndexRule{
		warningRatio:  config.SeqScanRatioWarning,
		criticalRatio: config.SeqScanRatioCritical,
		minTableSize:  config.MinTableSizeForIndex,
	}
}

// ID returns the rule identifier.
func (r *MissingIndexRule) ID() string {
	return "missing_index"
}

// Name returns the human-readable rule name.
func (r *MissingIndexRule) Name() string {
	return "Missing Index Detection"
}

// Evaluate analyzes table issues and generates suggestions for missing indexes.
func (r *MissingIndexRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || len(analysis.TableIssues) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, issue := range analysis.TableIssues {
		// Only handle missing index issues
		if issue.IssueType != analyzer.TableIssueMissingIndex {
			continue
		}

		// Skip small tables
		if issue.TableSize < r.minTableSize {
			continue
		}

		// Determine severity based on sequential scan ratio
		severity := suggester.SeverityInfo
		if issue.SeqScanRatio >= r.criticalRatio {
			severity = suggester.SeverityWarning
		} else if issue.SeqScanRatio >= r.warningRatio {
			severity = suggester.SeverityInfo
		}

		title := fmt.Sprintf("Consider index on %s.%s", issue.SchemaName, issue.TableName)

		// Build description
		var desc strings.Builder
		fmt.Fprintf(&desc, "Table `%s.%s` has a high sequential scan ratio (%.1f%%).\n\n",
			issue.SchemaName, issue.TableName, issue.SeqScanRatio*100)

		desc.WriteString("**Table Statistics:**\n")
		fmt.Fprintf(&desc, "- Table size: %s\n", formatBytes(issue.TableSize))
		fmt.Fprintf(&desc, "- Sequential scan ratio: %.1f%%\n", issue.SeqScanRatio*100)
		fmt.Fprintf(&desc, "- Live tuples: %d\n\n", issue.NLiveTup)

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("High sequential scan ratio indicates the table is frequently scanned without using indexes.\n\n")
		desc.WriteString("**To identify missing indexes:**\n")
		desc.WriteString("1. Identify the most frequent queries on this table\n")
		desc.WriteString("2. Run EXPLAIN ANALYZE on those queries\n")
		desc.WriteString("3. Look for sequential scans in the query plan\n")
		desc.WriteString("4. Add indexes on columns used in WHERE, JOIN, and ORDER BY clauses\n\n")

		desc.WriteString("**Example index creation:**\n")
		fmt.Fprintf(&desc, "```sql\n-- Analyze query patterns first\nCREATE INDEX idx_%s_<column> ON %s.%s (<column>);\n```\n",
			issue.TableName, issue.SchemaName, issue.TableName)

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("%s.%s", issue.SchemaName, issue.TableName),
			Metadata: map[string]any{
				"schema_name":    issue.SchemaName,
				"table_name":     issue.TableName,
				"table_size":     issue.TableSize,
				"seq_scan_ratio": issue.SeqScanRatio,
				"n_live_tup":     issue.NLiveTup,
			},
		})
	}

	return suggestions, nil
}

// Ensure MissingIndexRule implements Rule interface.
var _ suggester.Rule = (*MissingIndexRule)(nil)
