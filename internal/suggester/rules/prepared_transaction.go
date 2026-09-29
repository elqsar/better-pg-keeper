package rules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// PreparedTransactionRule flags two-phase transactions left prepared. They hold
// locks and block VACUUM indefinitely, and survive restarts.
type PreparedTransactionRule struct {
	warning  time.Duration
	critical time.Duration
}

// NewPreparedTransactionRule creates a new PreparedTransactionRule.
func NewPreparedTransactionRule(config *suggester.Config) *PreparedTransactionRule {
	return &PreparedTransactionRule{
		warning:  config.PreparedXactWarning,
		critical: config.PreparedXactCritical,
	}
}

// ID returns the rule identifier.
func (r *PreparedTransactionRule) ID() string {
	return "prepared_transaction"
}

// Name returns the human-readable rule name.
func (r *PreparedTransactionRule) Name() string {
	return "Orphaned Prepared Transaction"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *PreparedTransactionRule) RequiredDomains() []string {
	return []string{analyzer.DomainOutageRisk}
}

// Evaluate reports prepared transactions older than the thresholds.
func (r *PreparedTransactionRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Risk == nil || analysis.Risk.OutageRisk == nil {
		return nil, nil
	}
	now := analysis.DomainCoverage(analyzer.DomainOutageRisk).CapturedAt
	if now.IsZero() {
		now = analysis.AnalyzedAt
	}

	var suggestions []suggester.Suggestion
	for _, p := range analysis.Risk.PreparedXacts {
		age := now.Sub(p.Prepared)
		if age < r.warning {
			continue
		}
		severity := suggester.SeverityWarning
		if age >= r.critical {
			severity = suggester.SeverityCritical
		}

		var desc strings.Builder
		fmt.Fprintf(&desc, "Transaction `%s` was prepared %s ago (at %s) by `%s` in database `%s` and is still waiting to be committed or rolled back.\n\n",
			p.GID, formatAge(age), p.Prepared.Format("2006-01-02 15:04:05 MST"), p.Owner, p.Database)
		desc.WriteString("**Why it matters:**\n")
		desc.WriteString("A prepared transaction keeps its locks and its snapshot until someone finishes it, even across restarts. ")
		desc.WriteString("It blocks conflicting queries and DDL, and stops VACUUM from cleaning up or freezing rows anywhere in the cluster, ")
		fmt.Fprintf(&desc, "which eventually leads to bloat and transaction ID wraparound (its xmin age is now %s).\n\n", formatCount(p.XIDAge))
		desc.WriteString("**What to do:**\n")
		desc.WriteString("Find out which transaction manager or application prepared it and whether it was meant to commit. ")
		fmt.Fprintf(&desc, "Then, connected to `%s`, run one of:\n```sql\n", p.Database)
		fmt.Fprintf(&desc, "COMMIT PREPARED %s;\nROLLBACK PREPARED %s;\n```\n", quoteLiteral(p.GID), quoteLiteral(p.GID))
		desc.WriteString("- If nothing in your stack uses two-phase commit, set `max_prepared_transactions = 0` (requires a restart).\n")

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        fmt.Sprintf("Prepared transaction %s open for %s", p.GID, formatAge(age)),
			Description:  desc.String(),
			TargetObject: "prepared:" + p.GID,
			Metadata: map[string]any{
				"gid":         p.GID,
				"database":    p.Database,
				"owner":       p.Owner,
				"prepared":    p.Prepared,
				"age_seconds": age.Seconds(),
				"xid_age":     p.XIDAge,
			},
		})
	}
	return suggestions, nil
}

// formatAge renders a duration as e.g. "3 days", "5h" or "42m".
func formatAge(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// UnobservedTargets reports prepared transactions when their check failed.
func (r *PreparedTransactionRule) UnobservedTargets(analysis *analyzer.AnalysisResult) []string {
	return unobservedTargets(riskUnavailable(analysis), map[string]string{"prepared_xacts": "prepared:"})
}

// Ensure PreparedTransactionRule implements Rule interface.
var _ suggester.Rule = (*PreparedTransactionRule)(nil)
