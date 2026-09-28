package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// ReplicationSlotRule flags replication slots that retain WAL until the disk
// fills or hold back freezing, and standbys that fall far behind.
type ReplicationSlotRule struct {
	retainedWarning  int64
	retainedCritical int64
	xminCritical     int64
	lagWarning       float64
}

// NewReplicationSlotRule creates a new ReplicationSlotRule.
func NewReplicationSlotRule(config *suggester.Config) *ReplicationSlotRule {
	return &ReplicationSlotRule{
		retainedWarning:  config.SlotRetainedWarningBytes,
		retainedCritical: config.SlotRetainedCriticalBytes,
		xminCritical:     config.XIDAgeWarning,
		lagWarning:       config.ReplicaLagWarningSeconds,
	}
}

// ID returns the rule identifier.
func (r *ReplicationSlotRule) ID() string {
	return "replication_slot"
}

// Name returns the human-readable rule name.
func (r *ReplicationSlotRule) Name() string {
	return "Replication Slots and Lag"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *ReplicationSlotRule) RequiredDomains() []string {
	return []string{analyzer.DomainOutageRisk}
}

// Evaluate reports risky slots and lagging standbys.
func (r *ReplicationSlotRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Risk == nil || analysis.Risk.OutageRisk == nil {
		return nil, nil
	}
	risk := analysis.Risk.OutageRisk

	var suggestions []suggester.Suggestion
	for _, slot := range risk.Slots {
		if s := r.evaluateSlot(slot); s != nil {
			suggestions = append(suggestions, *s)
		}
	}
	for _, replica := range risk.Replicas {
		if s := r.evaluateReplica(replica); s != nil {
			suggestions = append(suggestions, *s)
		}
	}
	return suggestions, nil
}

func (r *ReplicationSlotRule) evaluateSlot(slot models.ReplicationSlot) *suggester.Suggestion {
	retained := derefInt64(slot.RetainedBytes)
	xminAge := max(derefInt64(slot.XminAge), derefInt64(slot.CatalogXminAge))

	var severity string
	var reasons []string
	switch {
	case slot.WALStatus == "unreserved":
		severity = suggester.SeverityCritical
		reasons = append(reasons, "WAL it needs is past `max_slot_wal_keep_size` and will be removed at the next checkpoint (`wal_status = unreserved`)")
	case slot.WALStatus == "lost":
		severity = suggester.SeverityWarning
		reasons = append(reasons, "the WAL it needs has already been removed (`wal_status = lost`), so its consumer can no longer catch up")
	}
	if retained >= r.retainedCritical {
		severity = suggester.SeverityCritical
		reasons = append(reasons, fmt.Sprintf("it retains %s of WAL", formatBytes(retained)))
	} else if !slot.Active && retained >= r.retainedWarning {
		severity = maxSeverity(severity, suggester.SeverityWarning)
		reasons = append(reasons, fmt.Sprintf("it is inactive and retains %s of WAL", formatBytes(retained)))
	}
	if xminAge >= r.xminCritical {
		severity = suggester.SeverityCritical
		reasons = append(reasons, fmt.Sprintf("it holds back freezing: xmin age %s", formatCount(xminAge)))
	}
	if severity == "" {
		return nil
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "Replication slot `%s` (%s, %s) needs attention: %s.\n\n",
		slot.Name, slot.Type, activeWord(slot.Active), strings.Join(reasons, "; "))

	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("A slot keeps every WAL segment its consumer has not confirmed, however long that takes. ")
	desc.WriteString("A consumer that is gone or stuck makes WAL grow until the disk fills and PostgreSQL shuts down. ")
	desc.WriteString("Slots also keep an xmin, which stops VACUUM from removing dead rows or freezing old ones.\n\n")
	if slot.SafeWALSize != nil {
		fmt.Fprintf(&desc, "WAL that can still be written before this slot is invalidated: %s.\n\n", formatBytes(*slot.SafeWALSize))
	}

	desc.WriteString("**What to do:**\n")
	desc.WriteString("- Find the consumer (a standby, Debezium, a logical replication subscription) and check whether it is still needed and running.\n")
	desc.WriteString("- If it is gone for good, drop the slot. The consumer loses its position and must be re-seeded:\n")
	fmt.Fprintf(&desc, "```sql\nSELECT pg_drop_replication_slot(%s);\n```\n", quoteLiteral(slot.Name))
	desc.WriteString("- Set `max_slot_wal_keep_size` so a stuck slot is invalidated before the disk fills.\n")

	return &suggester.Suggestion{
		RuleID:       r.ID(),
		Severity:     severity,
		Title:        fmt.Sprintf("Replication slot %s: %s", slot.Name, reasons[0]),
		Description:  desc.String(),
		TargetObject: "slot:" + slot.Name,
		Metadata: map[string]any{
			"slot_name":      slot.Name,
			"slot_type":      slot.Type,
			"active":         slot.Active,
			"wal_status":     slot.WALStatus,
			"retained_bytes": retained,
			"xmin_age":       xminAge,
		},
	}
}

func (r *ReplicationSlotRule) evaluateReplica(replica models.ReplicaLag) *suggester.Suggestion {
	if replica.ReplayLagSecs == nil || *replica.ReplayLagSecs < r.lagWarning {
		return nil
	}
	lag := *replica.ReplayLagSecs
	name := replica.ApplicationName
	if name == "" {
		name = replica.ClientAddr
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "Standby `%s` (%s, state %s) is %.0f seconds behind in replaying WAL.\n\n", name, replica.ClientAddr, replica.State, lag)
	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("Reads from this standby return stale data, a failover to it loses or delays recent writes, ")
	desc.WriteString("and the primary keeps WAL for it (with a slot) or it may fall off entirely (without one).\n\n")
	desc.WriteString("**What to do:**\n")
	desc.WriteString("- Check the standby for long-running queries that pause replay (`max_standby_streaming_delay`).\n")
	desc.WriteString("- Check its disk and network throughput against the primary's WAL rate.\n")

	return &suggester.Suggestion{
		RuleID:       r.ID(),
		Severity:     suggester.SeverityWarning,
		Title:        fmt.Sprintf("Standby %s is %.0fs behind", name, lag),
		Description:  desc.String(),
		TargetObject: "replica:" + name,
		Metadata: map[string]any{
			"application_name":   replica.ApplicationName,
			"client_addr":        replica.ClientAddr,
			"state":              replica.State,
			"replay_lag_seconds": lag,
		},
	}
}

func activeWord(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}

// maxSeverity returns the more severe of two severities; "" is the least severe.
func maxSeverity(a, b string) string {
	rank := map[string]int{"": 0, suggester.SeverityInfo: 1, suggester.SeverityWarning: 2, suggester.SeverityCritical: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// quoteLiteral quotes a string as a SQL literal.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// UnobservedTargets reports slots and replicas whose check failed.
func (r *ReplicationSlotRule) UnobservedTargets(analysis *analyzer.AnalysisResult) []string {
	return unobservedTargets(riskUnavailable(analysis), map[string]string{
		"replication_slots": "slot:",
		"replication":       "replica:",
	})
}

// Ensure ReplicationSlotRule implements Rule interface.
var _ suggester.Rule = (*ReplicationSlotRule)(nil)
