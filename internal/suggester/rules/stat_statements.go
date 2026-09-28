package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// StatStatementsCapacityRule warns when pg_stat_statements is too small or not
// tracking. Evicted queries lose their history, so slow-query and
// index-advisor findings are incomplete: PGAnalyzer checks its own evidence.
type StatStatementsCapacityRule struct {
	fullFraction float64
}

// NewStatStatementsCapacityRule creates a new StatStatementsCapacityRule.
func NewStatStatementsCapacityRule(config *suggester.Config) *StatStatementsCapacityRule {
	return &StatStatementsCapacityRule{fullFraction: config.StatStatementsFullFraction}
}

// ID returns the rule identifier.
func (r *StatStatementsCapacityRule) ID() string {
	return "stat_statements_capacity"
}

// Name returns the human-readable rule name.
func (r *StatStatementsCapacityRule) Name() string {
	return "pg_stat_statements Capacity"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *StatStatementsCapacityRule) RequiredDomains() []string {
	return []string{analyzer.DomainSettings}
}

// Evaluate checks how full pg_stat_statements is and whether it tracks anything.
func (r *StatStatementsCapacityRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Settings == nil || analysis.Settings.ServerSettings == nil {
		return nil, nil
	}
	// Failed checks leave their parts empty; UnobservedTargets keeps those
	// findings from resolving.
	settings := analysis.Settings

	var out []suggester.Suggestion
	if st, ok := settings.Get("pg_stat_statements.track"); ok && st.Setting == "none" {
		var desc strings.Builder
		desc.WriteString("`pg_stat_statements.track` is `none`, so no query statistics are recorded. ")
		desc.WriteString("Slow-query detection, the index advisor and the digest's top queries have nothing to work from.\n\n")
		desc.WriteString("**What to do:**\n")
		desc.WriteString(applySetting(st, "top"))
		out = append(out, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     suggester.SeverityWarning,
			Title:        "pg_stat_statements is not tracking queries",
			Description:  desc.String(),
			TargetObject: "setting:pg_stat_statements.track",
			Metadata:     map[string]any{"setting": st.Name, "value": st.Setting},
		})
	}

	u := settings.StatStatements
	if u == nil || u.Max <= 0 {
		return out, nil
	}
	fill := float64(u.Entries) / float64(u.Max)
	var evicted int64
	if settings.Evictions != nil {
		evicted = *settings.Evictions
	}
	if fill < r.fullFraction && evicted == 0 {
		return out, nil
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "pg_stat_statements holds %d of at most %d statements (%.0f%%).", u.Entries, u.Max, 100*fill)
	if settings.Evictions != nil {
		fmt.Fprintf(&desc, " It evicted statements %d time(s) in the last %s.", evicted, formatWindow(settings.EvictionWindow))
	} else if u.Dealloc > 0 {
		fmt.Fprintf(&desc, " It has evicted statements %d time(s) since its statistics were reset.", u.Dealloc)
	}
	desc.WriteString("\n\n**Why it matters:**\n")
	desc.WriteString("When it is full, PostgreSQL discards the least-used 5% of statements to make room. Their history is lost, so a query that runs rarely but slowly ")
	desc.WriteString("(a nightly job, a report) can disappear before PGAnalyzer flags it, and the index advisor never sees it. ")
	desc.WriteString("Frequent evictions usually mean queries with literals instead of parameters, each counted as a separate statement.\n\n")
	desc.WriteString("**What to do:**\n")
	target := max(10000, 2*u.Max)
	fmt.Fprintf(&desc, "- Raise the limit. Each entry takes a few kB of shared memory:\n")
	if st, ok := settings.Get("pg_stat_statements.max"); ok {
		desc.WriteString(applySetting(st, fmt.Sprint(target)))
	} else {
		fmt.Fprintf(&desc, "```sql\nALTER SYSTEM SET pg_stat_statements.max = %d;\n-- takes effect after a server restart\n```\n", target)
	}
	desc.WriteString("- If the application builds SQL with literal values, switch it to bind parameters; that also helps the plan cache.\n")

	meta := map[string]any{
		"entries":       u.Entries,
		"max":           u.Max,
		"fill_fraction": fill,
		"dealloc":       u.Dealloc,
	}
	if settings.Evictions != nil {
		meta["evictions"] = evicted
		meta["eviction_window_seconds"] = settings.EvictionWindow.Seconds()
	}
	title := fmt.Sprintf("pg_stat_statements is full (%d of %d statements)", u.Entries, u.Max)
	if fill < r.fullFraction {
		title = fmt.Sprintf("pg_stat_statements evicted statements %d time(s)", evicted)
	}
	out = append(out, suggester.Suggestion{
		RuleID:       r.ID(),
		Severity:     suggester.SeverityWarning,
		Title:        title,
		Description:  desc.String(),
		TargetObject: "setting:pg_stat_statements.max",
		Metadata:     meta,
	})
	return out, nil
}

// UnobservedTargets reports findings whose settings check failed.
func (r *StatStatementsCapacityRule) UnobservedTargets(analysis *analyzer.AnalysisResult) []string {
	return unobservedTargets(settingsUnavailable(analysis), map[string]string{
		"settings":        "setting:",
		"stat_statements": "setting:pg_stat_statements.max",
	})
}

// Ensure StatStatementsCapacityRule implements Rule interface.
var _ suggester.Rule = (*StatStatementsCapacityRule)(nil)
