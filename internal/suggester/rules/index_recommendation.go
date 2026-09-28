package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/plans"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// IndexRecommendationRule turns index proposals derived from query plans into
// ready-to-run DDL.
type IndexRecommendationRule struct {
	warningShare float64
}

// warningTimeShare is the share of execution time above which the queries an
// index would serve make it worth a warning rather than info.
const warningTimeShare = 0.05

// NewIndexRecommendationRule creates a new IndexRecommendationRule.
func NewIndexRecommendationRule(config *suggester.Config) *IndexRecommendationRule {
	return &IndexRecommendationRule{warningShare: warningTimeShare}
}

// ID returns the rule identifier.
func (r *IndexRecommendationRule) ID() string {
	return "index_recommendation"
}

// Name returns the human-readable rule name.
func (r *IndexRecommendationRule) Name() string {
	return "Index Recommendation"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *IndexRecommendationRule) RequiredDomains() []string {
	return []string{analyzer.DomainQueryPlans}
}

// Evaluate emits one suggestion per recommended index.
func (r *IndexRecommendationRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.QueryPlans == nil {
		return nil, nil
	}
	hypo := analysis.QueryPlans.Report != nil && analysis.QueryPlans.Report.HypoPG

	var suggestions []suggester.Suggestion
	for _, rec := range analysis.QueryPlans.Recommendations {
		severity := suggester.SeverityInfo
		if rec.TimeShare >= r.warningShare {
			severity = suggester.SeverityWarning
		}
		cols := strings.Join(rec.Columns, ", ")
		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        fmt.Sprintf("Add index on %s.%s (%s)", rec.Schema, rec.Table, cols),
			Description:  describeRecommendation(rec, hypo),
			TargetObject: fmt.Sprintf("index:%s.%s(%s)", rec.Schema, rec.Table, strings.Join(rec.Columns, ",")),
			Metadata: map[string]any{
				"schema":          rec.Schema,
				"table":           rec.Table,
				"columns":         rec.Columns,
				"time_share":      rec.TimeShare,
				"query_ids":       queryIDs(rec),
				"estimated_bytes": rec.EstimatedBytes,
				"validated":       rec.Validated,
			},
		})
	}
	return suggestions, nil
}

func describeRecommendation(rec analyzer.IndexRecommendation, hypo bool) string {
	name := plans.IndexName(rec.Table, rec.Columns)
	qualified := quoteQualified(rec.Schema, name)

	var desc strings.Builder
	fmt.Fprintf(&desc, "%s read every row of `%s.%s` (~%s rows) to find a few. ",
		pluralQueries(len(rec.Queries)), rec.Schema, rec.Table, formatCount(rec.EstimatedRows))
	fmt.Fprintf(&desc, "Together they account for %.1f%% of database execution time. An index on (%s) lets PostgreSQL go straight to the matching rows.\n\n",
		100*rec.TimeShare, strings.Join(rec.Columns, ", "))

	desc.WriteString("**Queries that would use it:**\n")
	for _, q := range rec.Queries {
		fmt.Fprintf(&desc, "- [Query %d](/queries/%d) (%.1f%% of DB time) filters on `%s`\n", q.QueryID, q.QueryID, 100*q.TimeShare, q.Filter)
		if v := q.Validation; v != nil && v.UsesIndex && v.CostAfter > 0 {
			fmt.Fprintf(&desc, "  - Planner cost with the index: %s → %s (%.0fx cheaper)\n",
				formatCount(int64(v.CostBefore)), formatCount(int64(v.CostAfter)), v.CostBefore/v.CostAfter)
		}
	}
	desc.WriteString("\n")

	switch {
	case rec.Validated:
		desc.WriteString("Checked with hypopg: the planner uses this index for the queries above.\n\n")
	case hypo:
		desc.WriteString("hypopg could not confirm the planner would use this index for every query; review the plans before creating it.\n\n")
	default:
		desc.WriteString("Not validated. With the [hypopg](https://github.com/HypoPG/hypopg) extension installed, PGAnalyzer checks each proposal against the planner without building anything.\n\n")
	}

	desc.WriteString("**Create it:**\n```sql\n")
	desc.WriteString(plans.CreateIndexSQL(rec.Schema, rec.Table, rec.Columns, name, true))
	desc.WriteString(";\n```\n")
	if rec.EstimatedBytes > 0 {
		fmt.Fprintf(&desc, "- Estimated size: about %s.\n", formatBytes(rec.EstimatedBytes))
	}
	desc.WriteString("- `CONCURRENTLY` keeps reads and writes going during the build, but takes longer and cannot run inside a transaction block.\n")
	desc.WriteString("- If the build fails or is cancelled it leaves an INVALID index behind. Check with:\n")
	fmt.Fprintf(&desc, "  `SELECT indisvalid FROM pg_index WHERE indexrelid = %s::regclass;`\n", quoteLiteral(qualified))
	desc.WriteString("- Every index slows down writes to the table a little; this one pays off if the queries above run often.\n\n")

	desc.WriteString("**Roll back:**\n```sql\n")
	fmt.Fprintf(&desc, "DROP INDEX CONCURRENTLY %s;\n```\n", qualified)
	return desc.String()
}

func pluralQueries(n int) string {
	if n == 1 {
		return "A busy query has to"
	}
	return fmt.Sprintf("%d busy queries have to", n)
}

func queryIDs(rec analyzer.IndexRecommendation) []int64 {
	ids := make([]int64, len(rec.Queries))
	for i, q := range rec.Queries {
		ids[i] = q.QueryID
	}
	return ids
}

// Ensure IndexRecommendationRule implements Rule interface.
var _ suggester.Rule = (*IndexRecommendationRule)(nil)
