package rules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// wraparoundLimit is where transaction and multixact IDs wrap. PostgreSQL stops
// assigning new IDs a few million short of it, which stops all writes.
const wraparoundLimit = 1 << 31

// WraparoundRule warns when a database's oldest unfrozen transaction ID (or
// multixact ID) gets close to wraparound.
type WraparoundRule struct {
	multixact      bool
	warningAge     int64
	criticalAge    int64
	freezeMaxRatio float64
}

// NewXIDWraparoundRule creates the transaction ID wraparound rule.
func NewXIDWraparoundRule(config *suggester.Config) *WraparoundRule {
	return newWraparoundRule(config, false)
}

// NewMultixactWraparoundRule creates the multixact ID wraparound rule.
func NewMultixactWraparoundRule(config *suggester.Config) *WraparoundRule {
	return newWraparoundRule(config, true)
}

func newWraparoundRule(config *suggester.Config, multixact bool) *WraparoundRule {
	return &WraparoundRule{
		multixact:      multixact,
		warningAge:     config.XIDAgeWarning,
		criticalAge:    config.XIDAgeCritical,
		freezeMaxRatio: config.FreezeMaxAgeFactor,
	}
}

// ID returns the rule identifier.
func (r *WraparoundRule) ID() string {
	if r.multixact {
		return "multixact_wraparound"
	}
	return "xid_wraparound"
}

// Name returns the human-readable rule name.
func (r *WraparoundRule) Name() string {
	if r.multixact {
		return "Multixact ID Wraparound"
	}
	return "Transaction ID Wraparound"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *WraparoundRule) RequiredDomains() []string {
	return []string{analyzer.DomainOutageRisk}
}

// warnAt is the age that triggers a warning: the configured age, or earlier when
// the database is already well past the point where autovacuum forces freezing.
func (r *WraparoundRule) warnAt(risk *models.OutageRisk) int64 {
	freezeMax := risk.FreezeMaxAge
	if r.multixact {
		freezeMax = risk.MultixactFreezeMaxAge
	}
	warn := r.warningAge
	if freezeMax > 0 && r.freezeMaxRatio > 0 {
		warn = min(warn, int64(float64(freezeMax)*r.freezeMaxRatio))
	}
	return warn
}

// Evaluate reports each database whose freeze horizon is too old.
func (r *WraparoundRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Risk == nil || analysis.Risk.OutageRisk == nil {
		return nil, nil
	}
	risk := analysis.Risk.OutageRisk
	warnAt := r.warnAt(risk)

	var suggestions []suggester.Suggestion
	for _, db := range risk.Databases {
		age := db.XIDAge
		if r.multixact {
			age = db.MXIDAge
		}
		if age < warnAt {
			continue
		}
		severity := suggester.SeverityWarning
		if age >= r.criticalAge {
			severity = suggester.SeverityCritical
		}

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        fmt.Sprintf("%s wraparound approaching in database %s (%.0f%% used)", r.idKind(), db.Name, 100*float64(age)/wraparoundLimit),
			Description:  r.describe(risk, db.Name, age, warnAt),
			TargetObject: "database:" + db.Name,
			Metadata: map[string]any{
				"database":    db.Name,
				"age":         age,
				"warn_at":     warnAt,
				"limit":       int64(wraparoundLimit),
				"used_ratio":  float64(age) / wraparoundLimit,
				"in_recovery": risk.InRecovery,
			},
		})
	}
	return suggestions, nil
}

func (r *WraparoundRule) idKind() string {
	if r.multixact {
		return "Multixact ID"
	}
	return "Transaction ID"
}

func (r *WraparoundRule) describe(risk *models.OutageRisk, dbName string, age, warnAt int64) string {
	var desc strings.Builder
	fmt.Fprintf(&desc, "The oldest unfrozen %s in database `%s` is %s IDs old, %.0f%% of the way to the ~2.1 billion limit.\n\n",
		strings.ToLower(r.idKind()), dbName, formatCount(age), 100*float64(age)/wraparoundLimit)

	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("PostgreSQL must freeze old rows before IDs wrap around. If it cannot, it stops accepting writes ")
	desc.WriteString("for the whole cluster until a manual VACUUM completes, which can take hours on large tables.\n\n")
	setting := "autovacuum_freeze_max_age"
	if r.multixact {
		setting = "autovacuum_multixact_freeze_max_age"
	}
	fmt.Fprintf(&desc, "Autovacuum normally keeps this age near `%s`; this is flagged from %s. ", setting, formatCount(warnAt))
	desc.WriteString("Being far past it usually means something prevents freezing, or autovacuum cannot keep up.\n\n")

	if risk.InRecovery {
		desc.WriteString("This server is a standby: freezing happens on the primary, so act there.\n\n")
	}

	if holders := xminHolders(risk); len(holders) > 0 && !r.multixact {
		desc.WriteString("**What may be blocking freezing (oldest first):**\n")
		for _, h := range holders {
			fmt.Fprintf(&desc, "- %s\n", h)
		}
		desc.WriteString("\nVACUUM cannot freeze rows newer than the oldest of these, so fix them first.\n\n")
	}

	desc.WriteString("**What to do:**\n")
	if dbName == risk.Database && len(risk.OldestTables) > 0 {
		desc.WriteString("Freeze the oldest tables, largest risk first. `VACUUM` takes no blocking locks but uses I/O:\n```sql\n")
		for i, t := range risk.OldestTables {
			if i == 3 {
				break
			}
			tableAge := t.XIDAge
			if r.multixact {
				tableAge = t.MXIDAge
			}
			fmt.Fprintf(&desc, "VACUUM (FREEZE, VERBOSE) %s;  -- age %s, %s\n",
				quoteQualified(t.SchemaName, t.RelName), formatCount(tableAge), formatBytes(t.TotalBytes))
		}
		desc.WriteString("```\n")
	} else {
		fmt.Fprintf(&desc, "Connect to `%s` and find its oldest tables:\n```sql\n", dbName)
		desc.WriteString("SELECT c.oid::regclass, age(c.relfrozenxid), mxid_age(c.relminmxid)\n")
		desc.WriteString("FROM pg_class c WHERE c.relkind IN ('r','m','t')\nORDER BY 2 DESC LIMIT 10;\n```\n")
		desc.WriteString("Then run `VACUUM (FREEZE, VERBOSE)` on each.\n")
	}
	desc.WriteString("- Check `pg_stat_progress_vacuum` for an anti-wraparound autovacuum already running; do not cancel it.\n")
	desc.WriteString("- If autovacuum is too slow, raise `autovacuum_vacuum_cost_limit` or `autovacuum_max_workers`.\n")
	return desc.String()
}

// xminHolders describes sessions, slots and prepared transactions holding back
// the xmin horizon, oldest first.
func xminHolders(risk *models.OutageRisk) []string {
	type holder struct {
		age  int64
		text string
	}
	var hs []holder
	for _, b := range risk.XminBackends {
		hs = append(hs, holder{b.XminAge, fmt.Sprintf("Session PID %d (%s@%s, %s), snapshot age %s: `%s`",
			b.PID, b.Username, b.Database, b.State, formatCount(b.XminAge), truncateQuery(b.Query, 80))})
	}
	for _, s := range risk.Slots {
		age := max(derefInt64(s.XminAge), derefInt64(s.CatalogXminAge))
		if age > 0 {
			hs = append(hs, holder{age, fmt.Sprintf("Replication slot `%s` (%s, active=%t), xmin age %s", s.Name, s.Type, s.Active, formatCount(age))})
		}
	}
	for _, p := range risk.PreparedXacts {
		hs = append(hs, holder{p.XIDAge, fmt.Sprintf("Prepared transaction `%s` in %s, age %s", p.GID, p.Database, formatCount(p.XIDAge))})
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i].age > hs[j].age })
	if len(hs) > 5 {
		hs = hs[:5]
	}
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.text
	}
	return out
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// formatCount renders large counts as e.g. "1.2B", "350M" or "12k".
func formatCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.0fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// Ensure WraparoundRule implements Rule interface.
var _ suggester.Rule = (*WraparoundRule)(nil)
