package models

import (
	"strconv"
	"strings"
	"time"
)

// ServerSettings holds the server configuration that decides whether
// PostgreSQL maintains itself (autovacuum), protects itself (timeouts), and
// records enough to diagnose problems (logging, pg_stat_statements capacity).
//
// It is stored as one JSON document per snapshot, like OutageRisk.
type ServerSettings struct {
	// Settings maps a pg_settings name to its row. Only the reviewed settings
	// and any with a pending restart are collected.
	Settings map[string]Setting `json:"settings"`
	// StatStatements is nil when pg_stat_statements is not installed in the
	// connected database.
	StatStatements *StatStatementsUsage `json:"stat_statements,omitempty"`
	// AutovacuumDisabled lists tables in the connected database with
	// autovacuum_enabled=false, largest first.
	AutovacuumDisabled []TableSize `json:"autovacuum_disabled,omitempty"`

	// Unavailable maps a check that failed to its error. The other checks are
	// still valid.
	Unavailable map[string]string `json:"unavailable,omitempty"`
}

// Get returns a setting by name.
func (s *ServerSettings) Get(name string) (Setting, bool) {
	if s == nil {
		return Setting{}, false
	}
	v, ok := s.Settings[name]
	return v, ok
}

// Setting is one row of pg_settings.
type Setting struct {
	Name    string `json:"name"`
	Setting string `json:"setting"`
	Unit    string `json:"unit,omitempty"`
	// Source is where the value came from: default, configuration file,
	// command line, ...
	Source string `json:"source"`
	// Context says what applying a change needs: postmaster (restart),
	// sighup (reload), user/superuser (per session).
	Context        string `json:"context"`
	BootVal        string `json:"boot_val,omitempty"`
	PendingRestart bool   `json:"pending_restart,omitempty"`
}

// On reports whether a boolean setting is on.
func (s Setting) On() bool {
	return s.Setting == "on"
}

// Bytes returns a memory setting in bytes. ok is false for settings that are
// not a memory amount, and for -1 ("use another setting").
func (s Setting) Bytes() (bytes int64, ok bool) {
	mult, ok := byteUnits[s.Unit]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(s.Setting, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n * mult, true
}

// Duration returns a time setting. ok is false for settings that are not a
// time, and for negative values (usually "disabled").
func (s Setting) Duration() (d time.Duration, ok bool) {
	unit, ok := timeUnits[s.Unit]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(s.Setting, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n * float64(unit)), true
}

// Float returns a numeric setting.
func (s Setting) Float() (float64, bool) {
	n, err := strconv.ParseFloat(s.Setting, 64)
	return n, err == nil
}

// NeedsRestart reports whether changing the setting needs a server restart
// rather than a reload.
func (s Setting) NeedsRestart() bool {
	return s.Context == "postmaster"
}

// Display renders the value in its unit as PostgreSQL would, e.g. "128MB".
func (s Setting) Display() string {
	if b, ok := s.Bytes(); ok {
		return FormatSettingBytes(b)
	}
	if d, ok := s.Duration(); ok {
		if d == 0 {
			return "0 (disabled)"
		}
		return d.String()
	}
	if s.Setting == "-1" {
		return "-1 (disabled)"
	}
	if s.Unit != "" && !strings.ContainsAny(s.Unit, "0123456789") {
		return s.Setting + s.Unit
	}
	return s.Setting
}

// FormatSettingBytes renders bytes in the largest whole PostgreSQL unit.
func FormatSettingBytes(b int64) string {
	for _, u := range []struct {
		suffix string
		size   int64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"kB", 1 << 10}} {
		if b >= u.size && b%u.size == 0 {
			return strconv.FormatInt(b/u.size, 10) + u.suffix
		}
	}
	if b >= 1<<20 {
		return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64) + "MB"
	}
	return strconv.FormatInt(b, 10) + "B"
}

var byteUnits = map[string]int64{
	"B":    1,
	"kB":   1 << 10,
	"8kB":  8 << 10,
	"MB":   1 << 20,
	"16MB": 16 << 20,
	"GB":   1 << 30,
}

var timeUnits = map[string]time.Duration{
	"us":  time.Microsecond,
	"ms":  time.Millisecond,
	"s":   time.Second,
	"min": time.Minute,
	"h":   time.Hour,
	"d":   24 * time.Hour,
}

// StatStatementsUsage is how full pg_stat_statements is. Once it holds
// pg_stat_statements.max entries it evicts the least-used ones (counted in
// Dealloc), so their history is lost.
type StatStatementsUsage struct {
	Entries int64 `json:"entries"`
	// Max is pg_stat_statements.max.
	Max        int64      `json:"max"`
	Dealloc    int64      `json:"dealloc"`
	StatsReset *time.Time `json:"stats_reset,omitempty"`
}

// TableSize names a table with its total size (including indexes and TOAST).
type TableSize struct {
	SchemaName string `json:"schemaname"`
	RelName    string `json:"relname"`
	TotalBytes int64  `json:"total_bytes"`
}
