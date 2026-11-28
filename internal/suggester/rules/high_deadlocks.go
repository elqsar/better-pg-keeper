package rules

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// RequiredDomains returns the analysis domains this rule reads.
// Deadlock counters come from pg_stat_database.
func (r *HighDeadlocksRule) RequiredDomains() []string {
	return []string{analyzer.DomainDatabaseStats}
}

// UnobservedTargets preserves active deadlock suggestions when no baseline
// snapshot is available to compute a recent window delta.
func (r *HighDeadlocksRule) UnobservedTargets(analysis *analyzer.AnalysisResult) []string {
	if analysis == nil || analysis.TransactionStats == nil || !analysis.TransactionStats.BaselineObserved {
		return []string{"database:deadlocks"}
	}
	return nil
}

// Evaluate analyzes deadlock counts over the recent window and generates suggestions.
func (r *HighDeadlocksRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.TransactionStats == nil || !analysis.TransactionStats.BaselineObserved {
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
	if stats.Window > 0 {
		fmt.Fprintf(&desc, "Database has experienced %d deadlock(s) over the last %s.\n\n", stats.Deadlocks, formatShortDuration(stats.Window))
	} else {
		fmt.Fprintf(&desc, "Database has experienced %d deadlock(s).\n\n", stats.Deadlocks)
	}

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

func formatShortDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
	}
	if d < time.Minute {
		return "<1m"
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
}

// Ensure HighDeadlocksRule implements Rule and PartiallyObserved interfaces.
var _ suggester.Rule = (*HighDeadlocksRule)(nil)
var _ suggester.PartiallyObserved = (*HighDeadlocksRule)(nil)
