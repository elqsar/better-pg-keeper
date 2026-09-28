package models

import "time"

// Setup check statuses, from best to worst. Pending and info never mean
// something is broken; warn and fail do.
const (
	SetupOK      = "ok"
	SetupInfo    = "info"
	SetupPending = "pending"
	SetupWarn    = "warn"
	SetupFail    = "fail"
)

// SetupCheck is one item of the setup checklist: whether PGAnalyzer can see
// what it needs, and how to fix it when it cannot.
type SetupCheck struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// Fix is SQL or configuration that resolves the check, when there is one.
	Fix string `json:"fix,omitempty"`
}

// SetupReport is the setup checklist at one point in time.
type SetupReport struct {
	CheckedAt time.Time    `json:"checked_at"`
	Checks    []SetupCheck `json:"checks"`
	// StatsSince is when PostgreSQL started counting index scans in the
	// connected database: the last statistics reset, or server start.
	StatsSince *time.Time `json:"stats_since,omitempty"`
}

// Count returns how many checks have the given status.
func (r *SetupReport) Count(status string) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, c := range r.Checks {
		if c.Status == status {
			n++
		}
	}
	return n
}

// NeedsAttention reports whether any check is warn or fail.
func (r *SetupReport) NeedsAttention() bool {
	return r.Count(SetupWarn)+r.Count(SetupFail) > 0
}
