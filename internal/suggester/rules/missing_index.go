package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// MissingIndexRule generates suggestions for tables with high sequential scan ratios.
type MissingIndexRule struct {
	criticalRatio float64
	minTableSize  int64
}

// NewMissingIndexRule creates a new MissingIndexRule with the given thresholds.
func NewMissingIndexRule(config *suggester.Config) *MissingIndexRule {
	return &MissingIndexRule{
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

// RequiredDomains returns the analysis domains this rule reads.
// Sequential-scan ratios come from table stats.
func (r *MissingIndexRule) RequiredDomains() []string {
	return []string{analyzer.DomainTableStats}
}

// Evaluate analyzes table issues and generates suggestions for missing indexes.
func (r *MissingIndexRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || len(analysis.TableIssues) == 0 {
		return nil, nil
	}

	// With usable query plans, tables that got a concrete index_recommendation
	// are left to that rule rather than reported twice.
	var planned *analyzer.QueryPlanAnalysis
	if analysis.DomainsUsable(analyzer.DomainQueryPlans) {
		planned = analysis.QueryPlans
	}

	var suggestions []suggester.Suggestion

	for _, issue := range analysis.TableIssues {
		// Only handle missing index issues
		if issue.IssueType != analyzer.TableIssueMissingIndex {
			continue
		}
		if len(planned.RecommendationsFor(issue.SchemaName, issue.TableName)) > 0 {
			continue
		}

		// Skip small tables
		if issue.TableSize < r.minTableSize {
			continue
		}

		// The analyzer only reports tables at or above the warning ratio, so
		// those are informational until the scan ratio reaches critical.
		severity := suggester.SeverityInfo
		if issue.SeqScanRatio >= r.criticalRatio {
			severity = suggester.SeverityWarning
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
		if scans := skippedScans(planned, issue.SchemaName, issue.TableName); len(scans) > 0 {
			desc.WriteString("**What the query plans show:**\n")
			desc.WriteString("Busy queries scan this table, but no index is proposed:\n")
			for _, line := range scans {
				fmt.Fprintf(&desc, "- %s\n", line)
			}
			desc.WriteString("\n")
		} else {
			desc.WriteString("None of the busiest queries PGAnalyzer plans each hour explain these scans; they may come from ")
			desc.WriteString("less frequent queries, batch jobs, or filters an index cannot serve.\n\n")
		}
		desc.WriteString("**To find the cause:**\n")
		desc.WriteString("1. Open the busiest queries that mention this table on the Queries page\n")
		desc.WriteString("2. Generate their plans and look for \"Reads every row of\" this table\n")
		desc.WriteString("3. Index the columns those queries filter on with equality first, then one range column\n")

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

// skippedScans describes planned queries that scan the table sequentially but
// got no index proposal, with the reason.
func skippedScans(planned *analyzer.QueryPlanAnalysis, schema, table string) []string {
	if planned == nil || planned.Report == nil {
		return nil
	}
	var lines []string
	for _, q := range planned.Report.Queries {
		for _, s := range q.SeqScans {
			if s.Schema != schema || s.Table != table || s.SkipReason == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("Query %d (%.0f%% of DB time): %s", q.QueryID, 100*q.TimeShare, s.SkipReason))
		}
	}
	return lines
}
