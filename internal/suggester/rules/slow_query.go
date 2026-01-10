// Package rules contains individual rule implementations for the suggester.
package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/suggester"
)

// SlowQueryRule generates suggestions for queries that exceed execution time thresholds.
type SlowQueryRule struct {
	warningThresholdMs  float64
	criticalThresholdMs float64
}

// NewSlowQueryRule creates a new SlowQueryRule with the given thresholds.
func NewSlowQueryRule(config *suggester.Config) *SlowQueryRule {
	return &SlowQueryRule{
		warningThresholdMs:  config.SlowQueryMs,
		criticalThresholdMs: config.SlowQueryCriticalMs,
	}
}

// ID returns the rule identifier.
func (r *SlowQueryRule) ID() string {
	return "slow_query"
}

// Name returns the human-readable rule name.
func (r *SlowQueryRule) Name() string {
	return "Slow Query Detection"
}

// Evaluate analyzes slow queries and generates suggestions.
func (r *SlowQueryRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || len(analysis.SlowQueries) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, sq := range analysis.SlowQueries {
		// Determine severity based on mean execution time
		severity := suggester.SeverityWarning
		if sq.MeanExecTime >= r.criticalThresholdMs {
			severity = suggester.SeverityCritical
		}

		// Create query preview for title
		queryPreview := truncateQuery(sq.Query, 50)
		title := fmt.Sprintf("Slow query detected: %s", queryPreview)

		// Build description with optimization hints
		var desc strings.Builder
		fmt.Fprintf(&desc, "Query has mean execution time of %.2f ms (threshold: %.0f ms).\n\n", sq.MeanExecTime, r.warningThresholdMs)
		desc.WriteString("**Execution Statistics:**\n")
		fmt.Fprintf(&desc, "- Mean execution time: %.2f ms\n", sq.MeanExecTime)
		fmt.Fprintf(&desc, "- Max execution time: %.2f ms\n", sq.MaxExecTime)
		fmt.Fprintf(&desc, "- Total execution time: %.2f ms\n", sq.TotalExecTime)
		fmt.Fprintf(&desc, "- Total calls: %d\n", sq.Calls)
		fmt.Fprintf(&desc, "- Average rows returned: %.1f\n", sq.AvgRows)
		fmt.Fprintf(&desc, "- Cache hit ratio: %.1f%%\n\n", sq.CacheHitRatio*100)

		desc.WriteString("**Optimization Hints:**\n")
		if sq.CacheHitRatio < 0.95 {
			desc.WriteString("- Low cache hit ratio suggests the query reads data not in cache. Consider adding appropriate indexes.\n")
		}
		if sq.AvgRows > 1000 {
			desc.WriteString("- High row count returned. Consider adding LIMIT or more selective WHERE clauses.\n")
		}
		desc.WriteString("- Run EXPLAIN ANALYZE to identify slow operations in the query plan.\n")
		desc.WriteString("- Check for missing indexes on columns used in WHERE, JOIN, and ORDER BY clauses.\n")

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("queryid:%d", sq.QueryID),
			Metadata: map[string]any{
				"queryid":         sq.QueryID,
				"query":           sq.Query,
				"mean_time_ms":    sq.MeanExecTime,
				"max_time_ms":     sq.MaxExecTime,
				"total_time_ms":   sq.TotalExecTime,
				"call_count":      sq.Calls,
				"cache_hit_ratio": sq.CacheHitRatio,
				"avg_rows":        sq.AvgRows,
			},
		})
	}

	return suggestions, nil
}

// truncateQuery returns a truncated version of the query for display.
func truncateQuery(query string, maxLen int) string {
	// Remove extra whitespace
	query = strings.Join(strings.Fields(query), " ")
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen-3] + "..."
}

// Ensure SlowQueryRule implements Rule interface.
var _ suggester.Rule = (*SlowQueryRule)(nil)
