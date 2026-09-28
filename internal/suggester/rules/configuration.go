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

const (
	// initdbSharedBuffers is the shared_buffers initdb writes into
	// postgresql.conf. Still having it on a large database means nobody tuned it.
	initdbSharedBuffers = 128 << 20
	// untunedSharedBuffersClusterBytes is the database size above which the
	// initdb shared_buffers is too small to hold the working set.
	untunedSharedBuffersClusterBytes = 2 << 30
	// largeAutovacuumDisabledTable is the size at which a table with
	// autovacuum disabled is critical: bloat there is expensive to undo.
	largeAutovacuumDisabledTable = 1 << 30
	// Typical shared_buffers range as a fraction of RAM.
	sharedBuffersMinFraction = 0.15
	sharedBuffersMaxFraction = 0.40
)

// ConfigurationRule reviews server settings that stop PostgreSQL from
// maintaining or protecting itself, or hide the evidence needed to diagnose it.
// Each finding has its own target so it resolves independently.
type ConfigurationRule struct {
	memoryBytes int64
}

// NewConfigurationRule creates a new ConfigurationRule.
func NewConfigurationRule(config *suggester.Config) *ConfigurationRule {
	return &ConfigurationRule{memoryBytes: config.ServerMemoryBytes}
}

// ID returns the rule identifier.
func (r *ConfigurationRule) ID() string {
	return "configuration"
}

// Name returns the human-readable rule name.
func (r *ConfigurationRule) Name() string {
	return "Server Configuration Review"
}

// RequiredDomains returns the analysis domains this rule reads. Activity and
// outage-risk data refine some findings when present but are not required.
func (r *ConfigurationRule) RequiredDomains() []string {
	return []string{analyzer.DomainSettings}
}

// Evaluate reviews the collected settings.
func (r *ConfigurationRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Settings == nil || analysis.Settings.ServerSettings == nil {
		return nil, nil
	}
	settings := analysis.Settings.ServerSettings
	// Without these checks a missing finding means "not looked at", which must
	// not resolve the existing ones.
	for _, check := range []string{"settings", "autovacuum_disabled"} {
		if msg, failed := settings.Unavailable[check]; failed {
			return nil, fmt.Errorf("settings check %s unavailable: %s", check, msg)
		}
	}

	var out []suggester.Suggestion
	add := func(s *suggester.Suggestion) {
		if s != nil {
			s.RuleID = r.ID()
			out = append(out, *s)
		}
	}
	add(r.autovacuum(settings))
	for _, t := range settings.AutovacuumDisabled {
		add(r.autovacuumDisabledTable(t))
	}
	add(r.idleInTransactionTimeout(settings, analysis))
	add(r.statementTimeout(settings))
	add(r.observability(settings))
	add(r.sharedBuffers(settings, analysis))
	add(r.workMem(settings))
	add(r.randomPageCost(settings))
	add(r.pendingRestart(settings))
	return out, nil
}

func (r *ConfigurationRule) autovacuum(s *models.ServerSettings) *suggester.Suggestion {
	av, okAV := s.Get("autovacuum")
	tc, okTC := s.Get("track_counts")
	var off []models.Setting
	if okAV && !av.On() {
		off = append(off, av)
	}
	if okTC && !tc.On() {
		off = append(off, tc)
	}
	if len(off) == 0 {
		return nil
	}

	var desc strings.Builder
	if okTC && !tc.On() {
		desc.WriteString("`track_counts` is off, so PostgreSQL does not count dead rows and autovacuum cannot tell which tables need vacuuming.")
		if okAV && !av.On() {
			desc.WriteString(" `autovacuum` is off as well.")
		}
		desc.WriteString("\n\n")
	} else {
		desc.WriteString("`autovacuum` is off for the whole server.\n\n")
	}
	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("- Dead rows from UPDATE and DELETE are never cleaned up, so tables and indexes grow without bound and queries slow down.\n")
	desc.WriteString("- Planner statistics are never refreshed, so plans get worse as data changes.\n")
	desc.WriteString("- PostgreSQL still forces anti-wraparound vacuums, but only when it has to, often on the largest tables at the busiest time.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("Turn it back on. If it was turned off because it caused load, tune it (`autovacuum_vacuum_cost_limit`, per-table thresholds) instead:\n")
	for _, st := range off {
		desc.WriteString(applySetting(st, "on"))
	}
	desc.WriteString("\nAfterwards, check the bloat and vacuum suggestions: tables that have gone without vacuum for a long time may need a manual `VACUUM (ANALYZE)`.\n")

	names := make([]string, len(off))
	for i, st := range off {
		names[i] = st.Name
	}
	return &suggester.Suggestion{
		Severity:     suggester.SeverityCritical,
		Title:        "Autovacuum is disabled",
		Description:  desc.String(),
		TargetObject: "setting:autovacuum",
		Metadata:     map[string]any{"settings_off": names},
	}
}

func (r *ConfigurationRule) autovacuumDisabledTable(t models.TableSize) *suggester.Suggestion {
	severity := suggester.SeverityWarning
	if t.TotalBytes >= largeAutovacuumDisabledTable {
		severity = suggester.SeverityCritical
	}
	name := quoteQualified(t.SchemaName, t.RelName)

	var desc strings.Builder
	fmt.Fprintf(&desc, "Table %s (%s) has `autovacuum_enabled = false`, so autovacuum never vacuums or analyzes it unless wraparound forces it.\n\n", name, formatBytes(t.TotalBytes))
	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("Dead rows accumulate and statistics go stale, so the table and its indexes bloat and queries on it get slower plans. ")
	desc.WriteString("This is often left behind by a bulk load or a migration that meant to turn it back on.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("If nothing vacuums this table on a schedule, turn autovacuum back on and catch up:\n")
	fmt.Fprintf(&desc, "```sql\nALTER TABLE %s RESET (autovacuum_enabled);\nVACUUM (ANALYZE, VERBOSE) %s;\n```\n", name, name)
	desc.WriteString("If autovacuum was disabled because it was too aggressive, tune it for this table instead, e.g. `ALTER TABLE ... SET (autovacuum_vacuum_cost_delay = 10)`.\n")

	return &suggester.Suggestion{
		Severity:     severity,
		Title:        fmt.Sprintf("Autovacuum disabled on %s.%s", t.SchemaName, t.RelName),
		Description:  desc.String(),
		TargetObject: fmt.Sprintf("autovacuum_disabled:%s.%s", t.SchemaName, t.RelName),
		Metadata: map[string]any{
			"schema":      t.SchemaName,
			"table":       t.RelName,
			"total_bytes": t.TotalBytes,
		},
	}
}

func (r *ConfigurationRule) idleInTransactionTimeout(s *models.ServerSettings, analysis *analyzer.AnalysisResult) *suggester.Suggestion {
	st, ok := s.Get("idle_in_transaction_session_timeout")
	if d, isTime := st.Duration(); !ok || !isTime || d > 0 {
		return nil
	}

	severity := suggester.SeverityInfo
	var seen int
	if analysis.ActivityStats != nil && analysis.DomainsUsable(analyzer.DomainActivity) {
		seen = len(analysis.ActivityStats.IdleInTransaction)
	}
	if seen > 0 {
		severity = suggester.SeverityWarning
	}

	var desc strings.Builder
	desc.WriteString("`idle_in_transaction_session_timeout` is 0, so a session that opens a transaction and stops sending queries keeps it open forever.")
	if seen > 0 {
		fmt.Fprintf(&desc, " %d such session(s) are open right now (see the idle-in-transaction suggestion).", seen)
	}
	desc.WriteString("\n\n**Why it matters:**\n")
	desc.WriteString("An idle transaction holds its locks, blocking DDL and conflicting writes, and holds back the xmin horizon so VACUUM cannot clean up anywhere. ")
	desc.WriteString("The usual cause is an application error path that skips COMMIT/ROLLBACK, or a person with an open `BEGIN` in a SQL console.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("Set a timeout that is longer than any legitimate pause inside a transaction. The server then ends such sessions:\n")
	desc.WriteString(applySetting(st, "5min"))
	desc.WriteString("To limit it to the application, set it per role instead: `ALTER ROLE app_user SET idle_in_transaction_session_timeout = '5min';`\n")

	return &suggester.Suggestion{
		Severity:     severity,
		Title:        "No timeout for idle transactions",
		Description:  desc.String(),
		TargetObject: "setting:" + st.Name,
		Metadata:     map[string]any{"setting": st.Name, "value": st.Setting, "idle_sessions": seen},
	}
}

func (r *ConfigurationRule) statementTimeout(s *models.ServerSettings) *suggester.Suggestion {
	st, ok := s.Get("statement_timeout")
	if d, isTime := st.Duration(); !ok || !isTime || d > 0 {
		return nil
	}

	var desc strings.Builder
	desc.WriteString("`statement_timeout` is 0, so a runaway query can run for hours, holding a connection, locks and the xmin horizon.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("Set a limit for the application role, a little above its slowest legitimate query:\n")
	desc.WriteString("```sql\nALTER ROLE app_user SET statement_timeout = '30s';\n```\n")
	desc.WriteString("Avoid a server-wide value: it also cancels migrations, `CREATE INDEX` and manual maintenance. ")
	desc.WriteString("New sessions of the role pick the value up; existing connections keep the old one until they reconnect.\n")

	return &suggester.Suggestion{
		Severity:     suggester.SeverityInfo,
		Title:        "No statement timeout",
		Description:  desc.String(),
		TargetObject: "setting:" + st.Name,
		Metadata:     map[string]any{"setting": st.Name, "value": st.Setting},
	}
}

// observability groups the settings that decide whether there is evidence to
// look at after an incident. They are cheap and usually all missing together.
func (r *ConfigurationRule) observability(s *models.ServerSettings) *suggester.Suggestion {
	type fix struct {
		st      models.Setting
		value   string
		purpose string
	}
	var fixes []fix
	if st, ok := s.Get("log_min_duration_statement"); ok && st.Setting == "-1" {
		fixes = append(fixes, fix{st, "1s", "logs each statement slower than 1s with its parameters, so a slow request can be traced after the fact"})
	}
	if st, ok := s.Get("log_lock_waits"); ok && !st.On() {
		fixes = append(fixes, fix{st, "on", "logs who blocked whom when a lock wait exceeds `deadlock_timeout` (1s by default)"})
	}
	if st, ok := s.Get("log_temp_files"); ok && st.Setting == "-1" {
		fixes = append(fixes, fix{st, "10MB", "logs queries that spill sorts or hashes to disk, which points at `work_mem` problems"})
	}
	if st, ok := s.Get("track_io_timing"); ok && !st.On() {
		fixes = append(fixes, fix{st, "on", "records time spent reading from disk per query in pg_stat_statements, separating slow I/O from slow CPU"})
	}
	if len(fixes) == 0 {
		return nil
	}

	var desc strings.Builder
	desc.WriteString("Some cheap diagnostics are turned off, so the evidence for the next incident will be missing:\n\n")
	names := make([]string, len(fixes))
	for i, f := range fixes {
		names[i] = f.st.Name
		fmt.Fprintf(&desc, "- `%s` is `%s`. Setting it to `%s` %s.\n", f.st.Name, f.st.Display(), f.value, f.purpose)
	}
	desc.WriteString("\n**What to do:**\n")
	for _, f := range fixes {
		desc.WriteString(applySetting(f.st, f.value))
	}
	desc.WriteString("`track_io_timing` has a small overhead on some virtual machines; `pg_test_timing` measures it.\n")

	return &suggester.Suggestion{
		Severity:     suggester.SeverityInfo,
		Title:        fmt.Sprintf("Slow-query and lock-wait diagnostics are off (%d setting(s))", len(fixes)),
		Description:  desc.String(),
		TargetObject: "setting:observability",
		Metadata:     map[string]any{"settings": names},
	}
}

func (r *ConfigurationRule) sharedBuffers(s *models.ServerSettings, analysis *analyzer.AnalysisResult) *suggester.Suggestion {
	st, ok := s.Get("shared_buffers")
	if !ok {
		return nil
	}
	sb, ok := st.Bytes()
	if !ok {
		return nil
	}

	var desc strings.Builder
	var title string
	meta := map[string]any{"setting": st.Name, "bytes": sb}
	if r.memoryBytes > 0 {
		frac := float64(sb) / float64(r.memoryBytes)
		meta["memory_bytes"] = r.memoryBytes
		meta["fraction"] = frac
		if frac >= sharedBuffersMinFraction && frac <= sharedBuffersMaxFraction {
			return nil
		}
		target := models.FormatSettingBytes(roundToMB(r.memoryBytes / 4))
		if frac < sharedBuffersMinFraction {
			title = fmt.Sprintf("shared_buffers is only %.0f%% of RAM", 100*frac)
			fmt.Fprintf(&desc, "`shared_buffers` is %s, %.0f%% of the %s of RAM configured in `thresholds.server_memory_gb`.\n\n", st.Display(), 100*frac, formatBytes(r.memoryBytes))
			desc.WriteString("**Why it matters:**\nPostgreSQL's own cache is small, so it keeps reading pages back from the OS cache or disk. 25% of RAM is the usual starting point.\n\n")
		} else {
			title = fmt.Sprintf("shared_buffers is %.0f%% of RAM", 100*frac)
			fmt.Fprintf(&desc, "`shared_buffers` is %s, %.0f%% of the %s of RAM configured in `thresholds.server_memory_gb`.\n\n", st.Display(), 100*frac, formatBytes(r.memoryBytes))
			desc.WriteString("**Why it matters:**\nThe OS page cache, per-query `work_mem` and connections need the rest. Above about 40% the server is more likely to swap or be killed by the OOM killer, and data is cached twice.\n\n")
		}
		desc.WriteString("**What to do:**\n")
		desc.WriteString(applySetting(st, target))
	} else {
		var cluster int64
		if analysis.Risk != nil && analysis.Risk.OutageRisk != nil {
			cluster = analysis.Risk.ClusterSize
		}
		if sb != initdbSharedBuffers || cluster < untunedSharedBuffersClusterBytes {
			return nil
		}
		meta["cluster_bytes"] = cluster
		title = "shared_buffers is still at the 128MB default"
		fmt.Fprintf(&desc, "`shared_buffers` is 128MB, the value `initdb` writes, while the databases hold %s.\n\n", formatBytes(cluster))
		desc.WriteString("**Why it matters:**\nPostgreSQL's own cache holds a small fraction of the data, so most reads go through the OS cache or to disk.\n\n")
		desc.WriteString("**What to do:**\n")
		desc.WriteString("Set it to about 25% of the server's RAM; for a 16GB server:\n")
		desc.WriteString(applySetting(st, "4GB"))
		desc.WriteString("Set `thresholds.server_memory_gb` in the PGAnalyzer config to have this checked against the actual RAM.\n")
	}
	desc.WriteString("Managed services (RDS, Cloud SQL) usually size it from the instance class already; check the parameter group before changing it.\n")

	return &suggester.Suggestion{
		Severity:     suggester.SeverityWarning,
		Title:        title,
		Description:  desc.String(),
		TargetObject: "setting:shared_buffers",
		Metadata:     meta,
	}
}

// workMem warns when connections using work_mem at once could exhaust RAM. A
// single query can use several multiples of work_mem, so this is a floor on the
// worst case, not a ceiling.
func (r *ConfigurationRule) workMem(s *models.ServerSettings) *suggester.Suggestion {
	if r.memoryBytes <= 0 {
		return nil
	}
	wm, okWM := s.Get("work_mem")
	mc, okMC := s.Get("max_connections")
	sb, okSB := s.Get("shared_buffers")
	if !okWM || !okMC || !okSB {
		return nil
	}
	wmBytes, ok1 := wm.Bytes()
	sbBytes, ok2 := sb.Bytes()
	conns, ok3 := mc.Float()
	if !ok1 || !ok2 || !ok3 {
		return nil
	}
	worst := int64(conns)*wmBytes + sbBytes
	if worst <= r.memoryBytes {
		return nil
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "`work_mem` is %s and `max_connections` is %d. With `shared_buffers` (%s), connections sorting at the same time could use %s, more than the %s of RAM.\n\n",
		wm.Display(), int64(conns), sb.Display(), formatBytes(worst), formatBytes(r.memoryBytes))
	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("`work_mem` is per sort or hash step, not per connection, so a single complex query can use it several times. ")
	desc.WriteString("Under a burst the server swaps, or the OOM killer ends a backend and PostgreSQL restarts every connection.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("- Lower the server-wide `work_mem`, and raise it only where needed: `ALTER ROLE reporting SET work_mem = '256MB';` or `SET work_mem` in the session.\n")
	desc.WriteString("- Or reduce `max_connections` and put a pooler (PgBouncer) in front, so fewer backends run at once.\n")

	return &suggester.Suggestion{
		Severity:     suggester.SeverityWarning,
		Title:        "work_mem x max_connections can exceed RAM",
		Description:  desc.String(),
		TargetObject: "setting:work_mem",
		Metadata: map[string]any{
			"work_mem_bytes":  wmBytes,
			"max_connections": int64(conns),
			"worst_bytes":     worst,
			"memory_bytes":    r.memoryBytes,
		},
	}
}

func (r *ConfigurationRule) randomPageCost(s *models.ServerSettings) *suggester.Suggestion {
	st, ok := s.Get("random_page_cost")
	if !ok {
		return nil
	}
	v, ok := st.Float()
	if !ok || v < 4 {
		return nil
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "`random_page_cost` is %s, a value tuned for spinning disks, where a random read costs four times a sequential one.\n\n", st.Setting)
	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("On SSD and network storage (including RDS, Cloud SQL and most cloud volumes) random reads are nearly as cheap as sequential ones. ")
	desc.WriteString("The planner then overestimates index scans and picks sequential scans on large tables it should not.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("If the data is on SSD or network storage:\n")
	desc.WriteString(applySetting(st, "1.1"))
	desc.WriteString("Compare the plans of the busiest queries before and after; `SET random_page_cost = 1.1` in a session tries it without changing the server.\n")

	return &suggester.Suggestion{
		Severity:     suggester.SeverityInfo,
		Title:        "random_page_cost is set for spinning disks",
		Description:  desc.String(),
		TargetObject: "setting:random_page_cost",
		Metadata:     map[string]any{"setting": st.Name, "value": v},
	}
}

func (r *ConfigurationRule) pendingRestart(s *models.ServerSettings) *suggester.Suggestion {
	var pending []string
	for name, st := range s.Settings {
		if st.PendingRestart {
			pending = append(pending, name)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Strings(pending)

	var desc strings.Builder
	desc.WriteString("These settings were changed, but the new values only take effect after a server restart:\n\n")
	for _, name := range pending {
		fmt.Fprintf(&desc, "- `%s` (running with `%s`)\n", name, s.Settings[name].Display())
	}
	desc.WriteString("\n**Why it matters:**\n")
	desc.WriteString("The server is not running with the configuration people think it has, and the change will take effect at an unplanned restart, such as a crash or failover.\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("Restart in a maintenance window, or revert the change in the configuration if it was a mistake. ")
	desc.WriteString("`SELECT name, setting, pending_restart FROM pg_settings WHERE pending_restart;` shows the current state.\n")

	return &suggester.Suggestion{
		Severity:     suggester.SeverityInfo,
		Title:        fmt.Sprintf("%d setting(s) waiting for a restart", len(pending)),
		Description:  desc.String(),
		TargetObject: "setting:pending_restart",
		Metadata:     map[string]any{"settings": pending},
	}
}

// applySetting renders how to apply a setting change and what that needs.
func applySetting(st models.Setting, value string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "```sql\nALTER SYSTEM SET %s = %s;\n", st.Name, quoteLiteral(value))
	if st.NeedsRestart() {
		b.WriteString("-- takes effect after a server restart\n```\n")
	} else {
		b.WriteString("SELECT pg_reload_conf();\n```\n")
	}
	b.WriteString("On managed Postgres, where `ALTER SYSTEM` is not allowed, change it in the parameter group or database flags instead.\n")
	return b.String()
}

func roundToMB(b int64) int64 {
	return b / (1 << 20) * (1 << 20)
}

// Ensure ConfigurationRule implements Rule interface.
var _ suggester.Rule = (*ConfigurationRule)(nil)
