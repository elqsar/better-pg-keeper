package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// LockContentionRule generates suggestions for blocked queries and lock contention.
type LockContentionRule struct {
	warningThresholdSec  float64
	criticalThresholdSec float64
}

// NewLockContentionRule creates a new LockContentionRule.
func NewLockContentionRule(config *suggester.Config) *LockContentionRule {
	return &LockContentionRule{
		warningThresholdSec:  config.BlockedQuerySeconds,
		criticalThresholdSec: config.BlockedQueryCriticalSeconds,
	}
}

// ID returns the rule identifier.
func (r *LockContentionRule) ID() string {
	return "lock_contention"
}

// Name returns the human-readable rule name.
func (r *LockContentionRule) Name() string {
	return "Lock Contention Detection"
}

// Evaluate analyzes blocked queries and generates suggestions.
func (r *LockContentionRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.LockStats == nil {
		return nil, nil
	}

	if len(analysis.LockStats.BlockedQueries) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, blocked := range analysis.LockStats.BlockedQueries {
		// Only flag queries blocked above warning threshold
		if blocked.WaitDuration < r.warningThresholdSec {
			continue
		}

		// Determine severity based on wait duration
		severity := suggester.SeverityWarning
		if blocked.WaitDuration >= r.criticalThresholdSec {
			severity = suggester.SeverityCritical
		}

		title := fmt.Sprintf("Query blocked for %.0fs waiting for %s lock", blocked.WaitDuration, blocked.LockMode)

		var desc strings.Builder
		fmt.Fprintf(&desc, "Query has been blocked for %.1f seconds waiting to acquire a %s lock.\n\n", blocked.WaitDuration, blocked.LockMode)

		desc.WriteString("**Blocked Query:**\n")
		fmt.Fprintf(&desc, "- PID: %d\n", blocked.BlockedPID)
		fmt.Fprintf(&desc, "- User: %s\n", blocked.BlockedUser)
		blockedQueryPreview := truncateQuery(blocked.BlockedQuery, 100)
		fmt.Fprintf(&desc, "- Query: %s\n\n", blockedQueryPreview)

		desc.WriteString("**Blocking Query:**\n")
		fmt.Fprintf(&desc, "- PID: %d\n", blocked.BlockingPID)
		fmt.Fprintf(&desc, "- User: %s\n", blocked.BlockingUser)
		blockingQueryPreview := truncateQuery(blocked.BlockingQuery, 100)
		fmt.Fprintf(&desc, "- Query: %s\n\n", blockingQueryPreview)

		desc.WriteString("**Lock Information:**\n")
		fmt.Fprintf(&desc, "- Lock Type: %s\n", blocked.LockType)
		fmt.Fprintf(&desc, "- Lock Mode: %s\n", blocked.LockMode)
		if blocked.Relation != nil {
			fmt.Fprintf(&desc, "- Table: %s\n\n", *blocked.Relation)
		}

		desc.WriteString("**Recommendations:**\n")
		desc.WriteString("- Review the blocking transaction to understand the contention\n")
		desc.WriteString("- Consider adding indexes on foreign key columns\n")
		desc.WriteString("- Review transaction isolation levels\n")
		desc.WriteString("- Reduce transaction scope/duration where possible\n")
		fmt.Fprintf(&desc, "- To unblock, consider terminating the blocking query: `SELECT pg_cancel_backend(%d);`\n", blocked.BlockingPID)

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("blocked_pid:%d", blocked.BlockedPID),
			Metadata: map[string]any{
				"blocked_pid":    blocked.BlockedPID,
				"blocked_user":   blocked.BlockedUser,
				"blocked_query":  blocked.BlockedQuery,
				"blocking_pid":   blocked.BlockingPID,
				"blocking_user":  blocked.BlockingUser,
				"blocking_query": blocked.BlockingQuery,
				"wait_duration":  blocked.WaitDuration,
				"lock_type":      blocked.LockType,
				"lock_mode":      blocked.LockMode,
				"relation":       blocked.Relation,
			},
		})
	}

	return suggestions, nil
}

// Ensure LockContentionRule implements Rule interface.
var _ suggester.Rule = (*LockContentionRule)(nil)
