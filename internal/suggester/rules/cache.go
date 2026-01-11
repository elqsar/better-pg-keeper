package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// CacheRule generates suggestions for low cache hit ratios.
type CacheRule struct {
	warningThreshold  float64
	criticalThreshold float64
}

// NewCacheRule creates a new CacheRule with the given thresholds.
func NewCacheRule(config *suggester.Config) *CacheRule {
	return &CacheRule{
		warningThreshold:  config.CacheHitRatioWarning,
		criticalThreshold: config.CacheHitRatioCritical,
	}
}

// ID returns the rule identifier.
func (r *CacheRule) ID() string {
	return "low_cache_hit"
}

// Name returns the human-readable rule name.
func (r *CacheRule) Name() string {
	return "Low Cache Hit Ratio Detection"
}

// Evaluate analyzes cache statistics and generates suggestions.
func (r *CacheRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.CacheStats == nil {
		return nil, nil
	}

	cache := analysis.CacheStats

	// Only generate suggestion if below threshold
	if !cache.BelowThreshold {
		return nil, nil
	}

	var suggestions []suggester.Suggestion

	// Convert ratio from percentage (0-100) to decimal (0-1) for comparison
	hitRatio := cache.OverallHitRatio / 100

	// Determine severity based on cache hit ratio
	severity := suggester.SeverityWarning
	if hitRatio < r.criticalThreshold {
		severity = suggester.SeverityCritical
	}

	title := "Low cache hit ratio"

	// Build description
	var desc strings.Builder
	fmt.Fprintf(&desc, "Database cache hit ratio is %.1f%% (threshold: %.0f%%).\n\n",
		cache.OverallHitRatio, r.warningThreshold*100)

	desc.WriteString("**What this means:**\n")
	desc.WriteString("A low cache hit ratio indicates that PostgreSQL frequently reads data from disk instead of memory.\n")
	desc.WriteString("This significantly impacts query performance.\n\n")

	desc.WriteString("**Recommendations:**\n")
	if hitRatio < r.criticalThreshold {
		desc.WriteString("1. **Critical**: Consider increasing `shared_buffers` immediately\n")
	} else {
		desc.WriteString("1. Consider increasing `shared_buffers`\n")
	}
	desc.WriteString("2. Review queries that read large amounts of data\n")
	desc.WriteString("3. Add indexes to reduce the amount of data scanned\n")
	desc.WriteString("4. Consider using connection pooling to reduce memory pressure\n\n")

	if len(cache.PoorCacheQueries) > 0 {
		desc.WriteString("**Queries with poor cache performance:**\n")
		for i, q := range cache.PoorCacheQueries {
			if i >= 5 {
				fmt.Fprintf(&desc, "... and %d more queries\n", len(cache.PoorCacheQueries)-5)
				break
			}
			queryPreview := truncateQuery(q.Query, 60)
			fmt.Fprintf(&desc, "- `%s` (%.1f%% cache hit)\n", queryPreview, q.CacheHitRatio*100)
		}
		desc.WriteString("\n")
	}

	desc.WriteString("**Memory configuration tips:**\n")
	desc.WriteString("```\n")
	desc.WriteString("# Typical shared_buffers setting: 25% of system RAM\n")
	desc.WriteString("# For 16GB RAM:\n")
	desc.WriteString("shared_buffers = 4GB\n")
	desc.WriteString("effective_cache_size = 12GB\n")
	desc.WriteString("```\n")

	suggestions = append(suggestions, suggester.Suggestion{
		RuleID:       r.ID(),
		Severity:     severity,
		Title:        title,
		Description:  desc.String(),
		TargetObject: "database",
		Metadata: map[string]any{
			"hit_ratio":        cache.OverallHitRatio,
			"threshold":        r.warningThreshold * 100,
			"poor_query_count": len(cache.PoorCacheQueries),
		},
	})

	return suggestions, nil
}

// Ensure CacheRule implements Rule interface.
var _ suggester.Rule = (*CacheRule)(nil)
