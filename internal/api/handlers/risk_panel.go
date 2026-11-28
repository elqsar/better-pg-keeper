package handlers

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/labstack/echo/v4"

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
	CollectedAt time.Time `json:"collected_at"`
	InRecovery  bool      `json:"in_recovery"`

	// Wraparound, as a fraction of the point where writes stop.
	XIDDatabase  string  `json:"xid_database"`
	XIDFraction  float64 `json:"xid_fraction"`
	MXIDDatabase string  `json:"mxid_database"`
	MXIDFraction float64 `json:"mxid_fraction"`
	XIDIssues    int     `json:"xid_issues"`

	Slots            int     `json:"slots"`
	InactiveSlots    int     `json:"inactive_slots"`
	RetainedWALBytes int64   `json:"retained_wal_bytes"`
	MaxReplayLagSecs float64 `json:"max_replay_lag_seconds"`
	SlotIssues       int     `json:"slot_issues"`

	TopSequence         *models.SequenceUsage `json:"top_sequence,omitempty"`
	SequencesOverHalf   int                   `json:"sequences_over_half"`
	SequencesUnreadable int                   `json:"sequences_unreadable"`
	SequenceIssues      int                   `json:"sequence_issues"`

	PreparedXacts  int      `json:"prepared_xacts"`
	PreparedIssues int      `json:"prepared_issues"`
	ClusterBytes   int64    `json:"cluster_bytes"`
	CapacityBytes  int64    `json:"capacity_bytes"`
	GrowthPerDay   float64  `json:"growth_per_day"` // 0 without a trend
	GrowthBytes    int64    `json:"growth_bytes"`   // |GrowthPerDay|, for display
	Shrinking      bool     `json:"shrinking"`
	HasTrend       bool     `json:"has_trend"`
	DaysToFull     float64  `json:"days_to_full"` // -1 without a capacity or with shrinking size
	DiskIssues     int      `json:"disk_issues"`
	Unavailable    []string `json:"unavailable,omitempty"`
}

// RiskStorage defines the storage interface needed by the outage-risk API handler.
type RiskStorage interface {
	OutageRiskStorage
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
}

// RiskResponse represents the GET /api/v1/risk response payload.
type RiskResponse struct {
	CollectedAt *time.Time         `json:"collected_at,omitempty"`
	Summary     *RiskPanel         `json:"summary"`
	OutageRisk  *models.OutageRisk `json:"outage_risk"`
}

// RiskHandler handles outage-risk API requests.
type RiskHandler struct {
	storage    RiskStorage
	cfg        OutageRiskConfig
	instanceID int64
}

// NewRiskHandler creates a new RiskHandler.
func NewRiskHandler(storage RiskStorage, cfg OutageRiskConfig, instanceID int64) *RiskHandler {
	if cfg.Storage == nil {
		cfg.Storage = storage
	}
	return &RiskHandler{
		storage:    storage,
		cfg:        cfg,
		instanceID: instanceID,
	}
}

// GetRisk handles GET /api/v1/risk requests.
func (h *RiskHandler) GetRisk(c echo.Context) error {
	ctx := c.Request().Context()

	snap, err := h.storage.GetLatestSnapshotWithCollector(ctx, h.instanceID, analyzer.DomainOutageRisk, time.Now())
	if err != nil {
		c.Logger().Errorf("failed to get latest outage-risk snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get outage-risk snapshot",
			"code":  "DATABASE_ERROR",
		})
	}
	if snap == nil {
		return c.JSON(http.StatusOK, RiskResponse{})
	}

	risk, err := h.storage.GetOutageRisk(ctx, snap.ID)
	if err != nil {
		c.Logger().Errorf("failed to get outage-risk data: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get outage-risk data",
			"code":  "DATABASE_ERROR",
		})
	}
	if risk == nil {
		return c.JSON(http.StatusOK, RiskResponse{})
	}

	active, err := h.storage.GetSuggestionsByStatus(ctx, h.instanceID, models.StatusActive)
	if err != nil {
		c.Logger().Errorf("failed to get active suggestions for risk summary: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get active suggestions",
			"code":  "DATABASE_ERROR",
		})
	}

	panel, err := buildRiskPanelFromData(ctx, &h.cfg, h.instanceID, snap, risk, active)
	if err != nil {
		c.Logger().Errorf("failed to build outage-risk summary: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to build outage-risk summary",
			"code":  "DATABASE_ERROR",
		})
	}

	collectedAt := snap.CapturedAt.UTC()
	return c.JSON(http.StatusOK, RiskResponse{
		CollectedAt: &collectedAt,
		Summary:     panel,
		OutageRisk:  risk,
	})
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
	return buildRiskPanelFromData(ctx, cfg, instanceID, snap, risk, active)
}

func buildRiskPanelFromData(ctx context.Context, cfg *OutageRiskConfig, instanceID int64, snap *models.Snapshot, risk *models.OutageRisk, active []models.Suggestion) (*RiskPanel, error) {
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
