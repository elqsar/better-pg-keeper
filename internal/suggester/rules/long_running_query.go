package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// LongRunningQueryRule generates suggestions for queries running longer than threshold.
type LongRunningQueryRule struct {
	warningThresholdSec  float64
	criticalThresholdSec float64
}

// NewLongRunningQueryRule creates a new LongRunningQueryRule.
func NewLongRunningQueryRule(config *suggester.Config) *LongRunningQueryRule {
	return &LongRunningQueryRule{
		warningThresholdSec:  config.LongRunningQuerySeconds,
		criticalThresholdSec: config.LongRunningCriticalSeconds,
	}
}

// ID returns the rule identifier.
func (r *LongRunningQueryRule) ID() string {
	return "long_running_query"
}

// Name returns the human-readable rule name.
func (r *LongRunningQueryRule) Name() string {
	return "Long Running Query Detection"
}

// Evaluate analyzes long-running queries and generates suggestions.
func (r *LongRunningQueryRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.ActivityStats == nil {
		return nil, nil
	}

	if len(analysis.ActivityStats.LongRunningQueries) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, lrq := range analysis.ActivityStats.LongRunningQueries {
		// Only flag queries above warning threshold
		if lrq.DurationSeconds < r.warningThresholdSec {
			continue
		}

		// Determine severity based on duration
		severity := suggester.SeverityWarning
		if lrq.DurationSeconds >= r.criticalThresholdSec {
			severity = suggester.SeverityCritical
		}

		queryPreview := truncateQuery(lrq.Query, 50)
		title := fmt.Sprintf("Long-running query (%.0fs): %s", lrq.DurationSeconds, queryPreview)

		var desc strings.Builder
		fmt.Fprintf(&desc, "Query has been running for %.1f seconds (threshold: %.0fs).\n\n", lrq.DurationSeconds, r.warningThresholdSec)
		desc.WriteString("**Query Details:**\n")
		fmt.Fprintf(&desc, "- PID: %d\n", lrq.PID)
		fmt.Fprintf(&desc, "- User: %s\n", lrq.Username)
		fmt.Fprintf(&desc, "- Database: %s\n", lrq.DatabaseName)
		fmt.Fprintf(&desc, "- State: %s\n", lrq.State)
		if lrq.WaitEventType != nil {
			fmt.Fprintf(&desc, "- Wait Event: %s/%s\n", *lrq.WaitEventType, *lrq.WaitEvent)
		}
		fmt.Fprintf(&desc, "- Started: %s\n\n", lrq.QueryStart.Format("2006-01-02 15:04:05"))

		desc.WriteString("**Recommendations:**\n")
		desc.WriteString("- Check if the query is making progress or is stuck waiting\n")
		desc.WriteString("- Review the query plan with EXPLAIN ANALYZE (on a test database)\n")
		desc.WriteString("- Consider adding statement_timeout to prevent runaway queries\n")
		desc.WriteString("- If needed, terminate the query with: `SELECT pg_cancel_backend(%d);`\n")

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("pid:%d", lrq.PID),
			Metadata: map[string]any{
				"pid":              lrq.PID,
				"username":         lrq.Username,
				"database":         lrq.DatabaseName,
				"query":            lrq.Query,
				"duration_seconds": lrq.DurationSeconds,
				"state":            lrq.State,
			},
		})
	}

	return suggestions, nil
}

// Ensure LongRunningQueryRule implements Rule interface.
var _ suggester.Rule = (*LongRunningQueryRule)(nil)
