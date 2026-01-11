package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// IdleInTransactionRule generates suggestions for connections idle in transaction.
type IdleInTransactionRule struct {
	warningThresholdSec  float64
	criticalThresholdSec float64
}

// NewIdleInTransactionRule creates a new IdleInTransactionRule.
func NewIdleInTransactionRule(config *suggester.Config) *IdleInTransactionRule {
	return &IdleInTransactionRule{
		warningThresholdSec:  config.IdleInTxSeconds,
		criticalThresholdSec: config.IdleInTxCriticalSeconds,
	}
}

// ID returns the rule identifier.
func (r *IdleInTransactionRule) ID() string {
	return "idle_in_transaction"
}

// Name returns the human-readable rule name.
func (r *IdleInTransactionRule) Name() string {
	return "Idle In Transaction Detection"
}

// Evaluate analyzes idle-in-transaction connections and generates suggestions.
func (r *IdleInTransactionRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.ActivityStats == nil {
		return nil, nil
	}

	if len(analysis.ActivityStats.IdleInTransaction) == 0 {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	for _, idle := range analysis.ActivityStats.IdleInTransaction {
		// Only flag connections above warning threshold
		if idle.DurationSeconds < r.warningThresholdSec {
			continue
		}

		// Determine severity based on duration
		severity := suggester.SeverityWarning
		if idle.DurationSeconds >= r.criticalThresholdSec {
			severity = suggester.SeverityCritical
		}

		title := fmt.Sprintf("Idle in transaction for %.0fs (PID %d)", idle.DurationSeconds, idle.PID)

		var desc strings.Builder
		fmt.Fprintf(&desc, "Connection has been idle in transaction for %.1f seconds.\n\n", idle.DurationSeconds)
		desc.WriteString("**Connection Details:**\n")
		fmt.Fprintf(&desc, "- PID: %d\n", idle.PID)
		fmt.Fprintf(&desc, "- User: %s\n", idle.Username)
		fmt.Fprintf(&desc, "- Database: %s\n", idle.DatabaseName)
		fmt.Fprintf(&desc, "- State: %s\n", idle.State)
		fmt.Fprintf(&desc, "- Transaction started: %s\n", idle.XactStart.Format("2006-01-02 15:04:05"))
		if idle.Query != "" {
			queryPreview := truncateQuery(idle.Query, 100)
			fmt.Fprintf(&desc, "- Last query: %s\n\n", queryPreview)
		}

		desc.WriteString("**Impact:**\n")
		desc.WriteString("- Holds locks that may block other transactions\n")
		desc.WriteString("- Prevents VACUUM from reclaiming dead tuples\n")
		desc.WriteString("- Consumes a connection slot\n\n")

		desc.WriteString("**Recommendations:**\n")
		desc.WriteString("- Review application code for uncommitted transactions\n")
		desc.WriteString("- Consider setting idle_in_transaction_session_timeout\n")
		desc.WriteString("- Ensure proper connection pool configuration\n")
		fmt.Fprintf(&desc, "- If needed, terminate: `SELECT pg_terminate_backend(%d);`\n", idle.PID)

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: fmt.Sprintf("pid:%d", idle.PID),
			Metadata: map[string]any{
				"pid":              idle.PID,
				"username":         idle.Username,
				"database":         idle.DatabaseName,
				"state":            idle.State,
				"duration_seconds": idle.DurationSeconds,
				"last_query":       idle.Query,
			},
		})
	}

	return suggestions, nil
}

// Ensure IdleInTransactionRule implements Rule interface.
var _ suggester.Rule = (*IdleInTransactionRule)(nil)
