package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// DiskGrowthRule forecasts when the data volume fills up from the database size
// trend. SQL cannot see free disk space, so the forecast needs the volume size
// from configuration; without it, only fast growth is reported, as info.
type DiskGrowthRule struct {
	capacityBytes int64
	warningDays   float64
	criticalDays  float64
}

// doublingDaysInfo is the growth rate reported when capacity is unknown: the
// databases would double in size within this many days.
const doublingDaysInfo = 90

// NewDiskGrowthRule creates a new DiskGrowthRule.
func NewDiskGrowthRule(config *suggester.Config) *DiskGrowthRule {
	return &DiskGrowthRule{
		capacityBytes: config.DiskCapacityBytes,
		warningDays:   config.DiskFullWarningDays,
		criticalDays:  config.DiskFullCriticalDays,
	}
}

// ID returns the rule identifier.
func (r *DiskGrowthRule) ID() string {
	return "disk_growth"
}

// Name returns the human-readable rule name.
func (r *DiskGrowthRule) Name() string {
	return "Disk Growth Forecast"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *DiskGrowthRule) RequiredDomains() []string {
	return []string{analyzer.DomainOutageRisk}
}

// Evaluate forecasts disk exhaustion from the size trend.
func (r *DiskGrowthRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Risk == nil || analysis.Risk.SizeTrend == nil {
		return nil, nil
	}
	trend := analysis.Risk.SizeTrend
	if trend.GrowthBytesPerDay <= 0 {
		return nil, nil
	}

	var severity, title string
	daysToFull := -1.0
	if r.capacityBytes > 0 {
		free := r.capacityBytes - trend.CurrentBytes
		daysToFull = max(float64(free)/trend.GrowthBytesPerDay, 0)
		switch {
		case daysToFull < r.criticalDays:
			severity = suggester.SeverityCritical
		case daysToFull < r.warningDays:
			severity = suggester.SeverityWarning
		default:
			return nil, nil
		}
		title = fmt.Sprintf("Disk forecast to fill in %.0f days", daysToFull)
	} else {
		if trend.GrowthBytesPerDay*doublingDaysInfo < float64(trend.CurrentBytes) {
			return nil, nil
		}
		severity = suggester.SeverityInfo
		title = fmt.Sprintf("Databases growing fast: %s/day", formatBytes(int64(trend.GrowthBytesPerDay)))
	}

	var desc strings.Builder
	fmt.Fprintf(&desc, "Databases total %s and grew by about %s per day over the last %s (%d samples).\n\n",
		formatBytes(trend.CurrentBytes), formatBytes(int64(trend.GrowthBytesPerDay)), formatWindow(trend.Span), trend.Samples)
	if daysToFull >= 0 {
		fmt.Fprintf(&desc, "At that rate the configured %s volume is full in about **%.0f days**. ", formatBytes(r.capacityBytes), daysToFull)
		desc.WriteString("When the data volume fills, PostgreSQL stops accepting writes and can shut down.\n\n")
		desc.WriteString("The forecast counts database files only. WAL, logs and temporary files share the volume on most setups, so the real margin is smaller.\n\n")
	} else {
		fmt.Fprintf(&desc, "That doubles the size in under %d days. Set `thresholds.disk_capacity_gb` to get a \"disk full in N days\" forecast and alerts.\n\n", doublingDaysInfo)
	}

	if len(trend.GrowingTables) > 0 {
		desc.WriteString("**Fastest-growing tables (with indexes):**\n")
		for _, t := range trend.GrowingTables {
			fmt.Fprintf(&desc, "- %s: %s, +%s/day\n", quoteQualified(t.SchemaName, t.RelName), formatBytes(t.CurrentBytes), formatBytes(int64(t.GrowthPerDay)))
		}
		desc.WriteString("\n")
	}

	desc.WriteString("**What to do:**\n")
	desc.WriteString("- Grow the volume before the forecast date; on managed services, enable storage autoscaling.\n")
	desc.WriteString("- Check whether the growth is expected: bloat (see `table_bloat` suggestions), audit or log tables without retention, or duplicated data.\n")
	desc.WriteString("- For append-only history, partition by time so old data can be dropped instantly.\n")

	return []suggester.Suggestion{{
		RuleID:       r.ID(),
		Severity:     severity,
		Title:        title,
		Description:  desc.String(),
		TargetObject: "instance:disk",
		Metadata: map[string]any{
			"current_bytes":        trend.CurrentBytes,
			"growth_bytes_per_day": trend.GrowthBytesPerDay,
			"capacity_bytes":       r.capacityBytes,
			"days_to_full":         daysToFull,
			"span_seconds":         trend.Span.Seconds(),
		},
	}}, nil
}

// UnobservedTargets reports the disk forecast when database sizes could not be read.
func (r *DiskGrowthRule) UnobservedTargets(analysis *analyzer.AnalysisResult) []string {
	return unobservedTargets(riskUnavailable(analysis), map[string]string{"size": "instance:disk"})
}

// Ensure DiskGrowthRule implements Rule interface.
var _ suggester.Rule = (*DiskGrowthRule)(nil)
