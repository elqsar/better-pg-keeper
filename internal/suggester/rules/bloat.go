package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
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

// RequiredDomains returns the analysis domains this rule reads.
// Bloat percentages come from the bloat collector, keyed against table stats.
func (r *BloatRule) RequiredDomains() []string {
	return []string{analyzer.DomainTableStats, analyzer.DomainBloat}
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

		deadPercent := issue.CurrentValue
		basis := "estimated dead/live row ratio"
		if issue.MeasurementSource == "pgstattuple_approx" {
			basis = "measured dead heap percentage"
		}

		// Build description
		var desc strings.Builder
		fmt.Fprintf(&desc, "Table `%s.%s` has %.1f%% %s (threshold: %.0f%%).\n\n",
			issue.SchemaName, issue.TableName, deadPercent, basis, r.warningThreshold)

		desc.WriteString("**Table Statistics:**\n")
		fmt.Fprintf(&desc, "- Dead tuples: %d\n", issue.NDeadTup)
		fmt.Fprintf(&desc, "- Live tuples: %d\n", issue.NLiveTup)
		if issue.NLiveTup > 0 {
			fmt.Fprintf(&desc, "- Estimated dead/live row ratio: %.1f%%\n", 100*float64(issue.NDeadTup)/float64(issue.NLiveTup))
		}
		if issue.TableSize > 0 {
			fmt.Fprintf(&desc, "- Table size: %s\n", formatBytes(issue.TableSize))
		}
		if issue.HeapBytes > 0 {
			fmt.Fprintf(&desc, "- Heap size (excluding indexes and TOAST): %s\n", formatBytes(issue.HeapBytes))
		}
		if issue.DeadTupleBytes != nil {
			label := "rough estimate from row counts"
			if issue.MeasurementSource == "pgstattuple_approx" {
				label = "pgstattuple_approx measurement"
			}
			fmt.Fprintf(&desc, "- Dead tuple footprint: %s (%s)\n", formatBytes(*issue.DeadTupleBytes), label)
		}
		if issue.FreeBytes != nil {
			fmt.Fprintf(&desc, "- Already free within the heap: approximately %s\n", formatBytes(*issue.FreeBytes))
		}
		if issue.VacuumTrigger != nil {
			fmt.Fprintf(&desc, "- Effective autovacuum trigger: %.0f dead rows\n", *issue.VacuumTrigger)
		}
		desc.WriteString("\n")

		desc.WriteString("**Recommendation:**\n")
		desc.WriteString("Investigate why autovacuum has not kept up. If cleanup is needed now, run:\n")
		fmt.Fprintf(&desc, "```sql\nVACUUM (ANALYZE) %s;\n```\n\n", quoteQualified(issue.SchemaName, issue.TableName))
		desc.WriteString("Ordinary VACUUM usually makes space reusable inside this table; it normally does not return that space to the operating system. A table rewrite can shrink the file but needs extra disk space and strong locking, so review it separately.\n\n")

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
				"schema_name": issue.SchemaName,
				"table_name":  issue.TableName,
				"dead_tuples": issue.NDeadTup,
				"live_tuples": issue.NLiveTup,
				"bloat_ratio": deadPercent,
				"table_size":  issue.TableSize,
			},
		})
	}

	return suggestions, nil
}

// Ensure BloatRule implements Rule interface.
var _ suggester.Rule = (*BloatRule)(nil)
