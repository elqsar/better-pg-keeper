package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// HighTempUsageRule generates suggestions for excessive temp file usage.
type HighTempUsageRule struct {
	warningBytes  int64
	criticalBytes int64
}

// NewHighTempUsageRule creates a new HighTempUsageRule.
func NewHighTempUsageRule(config *suggester.Config) *HighTempUsageRule {
	return &HighTempUsageRule{
		warningBytes:  config.TempBytesWarning,
		criticalBytes: config.TempBytesCritical,
	}
}

// ID returns the rule identifier.
func (r *HighTempUsageRule) ID() string {
	return "high_temp_usage"
}

// Name returns the human-readable rule name.
func (r *HighTempUsageRule) Name() string {
	return "High Temp File Usage Detection"
}

// Evaluate analyzes temp file usage and generates suggestions.
func (r *HighTempUsageRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.TransactionStats == nil {
		return nil, nil
	}

	stats := analysis.TransactionStats
	if stats.TempBytes < r.warningBytes {
		return nil, nil
	}

	// Determine severity
	severity := suggester.SeverityWarning
	if stats.TempBytes >= r.criticalBytes {
		severity = suggester.SeverityCritical
	}

	title := fmt.Sprintf("High temp file usage: %s", formatTempBytes(stats.TempBytes))

	var desc strings.Builder
	fmt.Fprintf(&desc, "Database has used %s of temporary files.\n\n", formatTempBytes(stats.TempBytes))
	desc.WriteString("**Statistics:**\n")
	fmt.Fprintf(&desc, "- Temp files created: %d\n", stats.TempFiles)
	fmt.Fprintf(&desc, "- Temp bytes written: %s\n\n", formatTempBytes(stats.TempBytes))

	desc.WriteString("**Impact:**\n")
	desc.WriteString("- Temp files indicate disk I/O instead of in-memory operations\n")
	desc.WriteString("- Can significantly slow down queries\n")
	desc.WriteString("- May indicate undersized work_mem setting\n\n")

	desc.WriteString("**Recommendations:**\n")
	desc.WriteString("- Consider increasing `work_mem` for sorting and hash operations\n")
	desc.WriteString("- Review queries doing large sorts, hash joins, or aggregations\n")
	desc.WriteString("- Add appropriate indexes to reduce sorting needs\n")
	desc.WriteString("- Consider partitioning large tables\n")

	return []suggester.Suggestion{
		{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        title,
			Description:  desc.String(),
			TargetObject: "database:temp_usage",
			Metadata: map[string]any{
				"temp_files": stats.TempFiles,
				"temp_bytes": stats.TempBytes,
			},
		},
	}, nil
}

// formatTempBytes converts bytes to human-readable format.
func formatTempBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// Ensure HighTempUsageRule implements Rule interface.
var _ suggester.Rule = (*HighTempUsageRule)(nil)
