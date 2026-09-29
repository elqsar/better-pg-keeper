package handlers

import (
	"context"
	"sort"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
)

// wraparoundLimit is the transaction ID age at which data would wrap around
// (2^31). PostgreSQL stops accepting writes a few million short of it.
const wraparoundLimit = 1 << 31

// OutageRiskStorage is what the dashboard's outage-risk panel reads.
type OutageRiskStorage interface {
	GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error)
	GetOutageRisk(ctx context.Context, snapshotID int64) (*models.OutageRisk, error)
	GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error)
}

// OutageRiskConfig enables the dashboard's outage-risk panel.
type OutageRiskConfig struct {
	Storage OutageRiskStorage
	// DiskCapacityBytes enables the "full in N days" forecast; 0 means unknown.
	DiskCapacityBytes int64
}

// RiskPanel is the dashboard's summary of the outage-risk signals. Severity
// is left to the rules: each row shows how many active suggestions it has.
type RiskPanel struct {
	CollectedAt time.Time
	InRecovery  bool

	// Wraparound, as a fraction of the point where writes stop.
	XIDDatabase  string
	XIDFraction  float64
	MXIDDatabase string
	MXIDFraction float64
	XIDIssues    int

	Slots            int
	InactiveSlots    int
	RetainedWALBytes int64
	MaxReplayLagSecs float64
	SlotIssues       int

	TopSequence         *models.SequenceUsage
	SequencesOverHalf   int
	SequencesUnreadable int
	SequenceIssues      int

	PreparedXacts  int
	PreparedIssues int
	ClusterBytes   int64
	CapacityBytes  int64
	GrowthPerDay   float64 // 0 without a trend
	GrowthBytes    int64   // |GrowthPerDay|, for display
	Shrinking      bool
	HasTrend       bool
	DaysToFull     float64 // -1 without a capacity or with shrinking size
	DiskIssues     int
	Unavailable    []string
}

// buildRiskPanel reads the latest outage-risk snapshot. It returns nil before
// the first one.
func buildRiskPanel(ctx context.Context, cfg *OutageRiskConfig, instanceID int64, active []models.Suggestion) (*RiskPanel, error) {
	if cfg == nil || cfg.Storage == nil {
		return nil, nil
	}
	snap, err := cfg.Storage.GetLatestSnapshotWithCollector(ctx, instanceID, analyzer.DomainOutageRisk, time.Now())
	if err != nil || snap == nil {
		return nil, err
	}
	risk, err := cfg.Storage.GetOutageRisk(ctx, snap.ID)
	if err != nil || risk == nil {
		return nil, err
	}

	p := &RiskPanel{CollectedAt: snap.CapturedAt, InRecovery: risk.InRecovery, CapacityBytes: cfg.DiskCapacityBytes, DaysToFull: -1}
	for _, db := range risk.Databases {
		if f := float64(db.XIDAge) / wraparoundLimit; f > p.XIDFraction || p.XIDDatabase == "" {
			p.XIDDatabase, p.XIDFraction = db.Name, f
		}
		if f := float64(db.MXIDAge) / wraparoundLimit; f > p.MXIDFraction || p.MXIDDatabase == "" {
			p.MXIDDatabase, p.MXIDFraction = db.Name, f
		}
	}

	p.Slots = len(risk.Slots)
	for _, s := range risk.Slots {
		if !s.Active {
			p.InactiveSlots++
		}
		if s.RetainedBytes != nil {
			p.RetainedWALBytes += *s.RetainedBytes
		}
	}
	for _, r := range risk.Replicas {
		if r.ReplayLagSecs != nil && *r.ReplayLagSecs > p.MaxReplayLagSecs {
			p.MaxReplayLagSecs = *r.ReplayLagSecs
		}
	}

	seqs := append([]models.SequenceUsage(nil), risk.Sequences...)
	sort.Slice(seqs, func(i, j int) bool { return seqs[i].UsedFraction > seqs[j].UsedFraction })
	if len(seqs) > 0 {
		p.TopSequence = &seqs[0]
	}
	for _, s := range seqs {
		if s.UsedFraction >= 0.5 {
			p.SequencesOverHalf++
		}
	}
	p.SequencesUnreadable = risk.SequencesUnreadable
	p.PreparedXacts = len(risk.PreparedXacts)
	p.ClusterBytes = risk.ClusterSize

	history, err := cfg.Storage.GetSizeHistory(ctx, instanceID, snap.CapturedAt.Add(-analyzer.SizeTrendWindow))
	if err != nil {
		return nil, err
	}
	if trend := analyzer.FitSizeTrend(history); trend != nil {
		p.HasTrend, p.GrowthPerDay = true, trend.GrowthBytesPerDay
		p.GrowthBytes, p.Shrinking = int64(trend.GrowthBytesPerDay), trend.GrowthBytesPerDay < 0
		if p.Shrinking {
			p.GrowthBytes = -p.GrowthBytes
		}
		// Same forecast as the disk_growth rule.
		if p.CapacityBytes > 0 && trend.GrowthBytesPerDay > 0 {
			free := p.CapacityBytes - trend.CurrentBytes
			p.DaysToFull = max(float64(free)/trend.GrowthBytesPerDay, 0)
		}
	}

	for check := range risk.Unavailable {
		p.Unavailable = append(p.Unavailable, check)
	}
	sort.Strings(p.Unavailable)

	for _, s := range active {
		switch s.RuleID {
		case "xid_wraparound", "multixact_wraparound":
			p.XIDIssues++
		case "replication_slot":
			p.SlotIssues++
		case "sequence_exhaustion":
			p.SequenceIssues++
		case "prepared_transaction":
			p.PreparedIssues++
		case "disk_growth":
			p.DiskIssues++
		}
	}
	return p, nil
}
