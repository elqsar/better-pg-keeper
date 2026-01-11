package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// HighDeadlocksRule generates suggestions when deadlocks are detected.
type HighDeadlocksRule struct {
	warningThreshold int64
}

// NewHighDeadlocksRule creates a new HighDeadlocksRule.
func NewHighDeadlocksRule(config *suggester.Config) *HighDeadlocksRule {
	return &HighDeadlocksRule{
		warningThreshold: config.DeadlocksWarning,
	}
}

// ID returns the rule identifier.
func (r *HighDeadlocksRule) ID() string {
	return "high_deadlocks"
}

// Name returns the human-readable rule name.
func (r *HighDeadlocksRule) Name() string {
	return "Deadlock Detection"
}

// Evaluate analyzes deadlock counts and generates suggestions.
func (r *HighDeadlocksRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.TransactionStats == nil {
		return nil, nil
	}

	stats := analysis.TransactionStats
	if stats.Deadlocks < r.warningThreshold {
		return nil, nil
	}

	// Deadlocks are always at least a warning, critical if many
	severity := suggester.SeverityWarning
	if stats.Deadlocks >= 5 {
		severity = suggester.SeverityCritical
	}

	title := fmt.Sprintf("Deadlocks detected: %d", stats.Deadlocks)

	var desc strings.Builder
	fmt.Fprintf(&desc, "Database has experienced %d deadlock(s).\n\n", stats.Deadlocks)

	desc.WriteString("**What is a deadlock?**\n")
	desc.WriteString("A deadlock occurs when two or more transactions wait for each other to release locks, ")
	desc.WriteString("creating a circular dependency. PostgreSQL automatically detects and terminates one transaction to resolve it.\n\n")

	desc.WriteString("**Impact:**\n")
	desc.WriteString("- One transaction is rolled back and must be retried\n")
	desc.WriteString("- Indicates contention for the same resources\n")
	desc.WriteString("- Can cause application errors if not handled properly\n\n")

	desc.WriteString("**Recommendations:**\n")
	desc.WriteString("- Ensure all transactions acquire locks in the same order\n")
	desc.WriteString("- Keep transactions short and focused\n")
	desc.WriteString("- Use appropriate isolation levels (avoid SERIALIZABLE if not needed)\n")
	desc.WriteString("- Review application logic for lock ordering issues\n")
	desc.WriteString("- Consider using advisory locks for coordinating access\n")
	desc.WriteString("- Check the PostgreSQL log for detailed deadlock information\n")

	return []suggester.Suggestion{
		{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: "database:deadlocks",
			Metadata: map[string]any{
				"deadlock_count": stats.Deadlocks,
			},
		},
	}, nil
}

// Ensure HighDeadlocksRule implements Rule interface.
var _ suggester.Rule = (*HighDeadlocksRule)(nil)
