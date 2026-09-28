package rules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// ConnectionSaturationRule warns when connections approach max_connections, after
// which new connections are refused and the application errors.
type ConnectionSaturationRule struct {
	warning  float64
	critical float64
}

// NewConnectionSaturationRule creates a new ConnectionSaturationRule.
func NewConnectionSaturationRule(config *suggester.Config) *ConnectionSaturationRule {
	return &ConnectionSaturationRule{
		warning:  config.ConnectionUtilizationWarning,
		critical: config.ConnectionUtilizationCritical,
	}
}

// ID returns the rule identifier.
func (r *ConnectionSaturationRule) ID() string {
	return "connection_saturation"
}

// Name returns the human-readable rule name.
func (r *ConnectionSaturationRule) Name() string {
	return "Connection Saturation"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *ConnectionSaturationRule) RequiredDomains() []string {
	return []string{analyzer.DomainActivity}
}

// Evaluate checks the peak (or, without history, current) connection utilization.
func (r *ConnectionSaturationRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.ActivityStats == nil {
		return nil, nil
	}
	activity := analysis.ActivityStats

	total, idle, maxConns := activity.TotalConnections, activity.IdleCount, activity.MaxConnections
	period := "now"
	var window time.Duration
	if p := activity.Peak; p != nil && p.MaxConnections > 0 &&
		float64(p.TotalConnections)/float64(p.MaxConnections) > float64(total)/float64(max(maxConns, 1)) {
		total, idle, maxConns = p.TotalConnections, p.IdleCount, p.MaxConnections
		window = p.Window
		period = fmt.Sprintf("at peak in the last %s (%s)", formatWindow(p.Window), p.CapturedAt.Format("2006-01-02 15:04"))
	}
	if maxConns <= 0 {
		return nil, nil
	}
	utilization := float64(total) / float64(maxConns)
	if utilization < r.warning {
		return nil, nil
	}
	severity := suggester.SeverityWarning
	if utilization >= r.critical {
		severity = suggester.SeverityCritical
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "%d of %d connections were in use %s (%.0f%%); %d of them idle.\n\n", total, maxConns, period, 100*utilization, idle)
	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("Once `max_connections` is reached, new connections fail with \"too many clients\" and the application ")
	desc.WriteString("starts erroring. Superuser-reserved slots are all that is left for you to log in and fix it.\n\n")
	desc.WriteString("**What to do:**\n")
	if total > 0 && float64(idle)/float64(total) >= 0.5 {
		desc.WriteString("- Most connections are idle, which is the signature of per-process application pools. ")
		desc.WriteString("Put a pooler such as PgBouncer (transaction mode) in front, so a few server connections serve many clients.\n")
	} else {
		desc.WriteString("- Most connections are busy: look for slow queries holding connections and for lock waits (see those suggestions).\n")
		desc.WriteString("- A pooler such as PgBouncer still helps by queueing work instead of refusing it.\n")
	}
	desc.WriteString("- Lower application pool sizes so their sum stays below `max_connections`.\n")
	desc.WriteString("- Raising `max_connections` costs memory per connection and needs a restart; prefer a pooler.\n")

	return []suggester.Suggestion{{
		RuleID:       r.ID(),
		Severity:     severity,
		Title:        fmt.Sprintf("Connections at %.0f%% of max_connections", 100*utilization),
		Description:  desc.String(),
		TargetObject: "instance:connections",
		Metadata: map[string]any{
			"total_connections": total,
			"idle_connections":  idle,
			"max_connections":   maxConns,
			"utilization":       utilization,
			"window_seconds":    window.Seconds(),
		},
	}}, nil
}

// Ensure ConnectionSaturationRule implements Rule interface.
var _ suggester.Rule = (*ConnectionSaturationRule)(nil)
