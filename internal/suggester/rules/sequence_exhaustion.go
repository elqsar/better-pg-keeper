package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/suggester"
)

// SequenceExhaustionRule warns when a sequence approaches the largest value it
// can hand out, after which every INSERT that needs a new ID fails.
type SequenceExhaustionRule struct {
	warning  float64
	critical float64
}

// NewSequenceExhaustionRule creates a new SequenceExhaustionRule.
func NewSequenceExhaustionRule(config *suggester.Config) *SequenceExhaustionRule {
	return &SequenceExhaustionRule{
		warning:  config.SequenceUsageWarning,
		critical: config.SequenceUsageCritical,
	}
}

// ID returns the rule identifier.
func (r *SequenceExhaustionRule) ID() string {
	return "sequence_exhaustion"
}

// Name returns the human-readable rule name.
func (r *SequenceExhaustionRule) Name() string {
	return "Sequence Exhaustion"
}

// RequiredDomains returns the analysis domains this rule reads.
func (r *SequenceExhaustionRule) RequiredDomains() []string {
	return []string{analyzer.DomainOutageRisk}
}

// Evaluate reports sequences past the usage thresholds.
func (r *SequenceExhaustionRule) Evaluate(ctx context.Context, analysis *analyzer.AnalysisResult) ([]suggester.Suggestion, error) {
	if analysis == nil || analysis.Risk == nil || analysis.Risk.OutageRisk == nil {
		return nil, nil
	}

	var suggestions []suggester.Suggestion
	for _, seq := range analysis.Risk.Sequences {
		if seq.UsedFraction < r.warning {
			continue
		}
		severity := suggester.SeverityWarning
		if seq.UsedFraction >= r.critical {
			severity = suggester.SeverityCritical
		}

		name := quoteQualified(seq.SchemaName, seq.SequenceName)
		subject := name
		if seq.TableName != "" {
			subject = fmt.Sprintf("%s.%s", seq.TableName, seq.ColumnName)
		}

		suggestions = append(suggestions, suggester.Suggestion{
			RuleID:       r.ID(),
			Severity:     severity,
			Title:        fmt.Sprintf("%s has used %.0f%% of its ID range", subject, 100*seq.UsedFraction),
			Description:  describeSequence(seq, name),
			TargetObject: "sequence:" + seq.SchemaName + "." + seq.SequenceName,
			Metadata: map[string]any{
				"sequence":      seq.SchemaName + "." + seq.SequenceName,
				"table":         seq.TableName,
				"column":        seq.ColumnName,
				"column_type":   seq.ColumnType,
				"last_value":    seq.LastValue,
				"max_value":     seq.MaxValue,
				"used_fraction": seq.UsedFraction,
			},
		})
	}
	return suggestions, nil
}

func describeSequence(seq models.SequenceUsage, name string) string {
	var desc strings.Builder
	fmt.Fprintf(&desc, "Sequence `%s` is at %s of a maximum %s (%.1f%% used, %s left).\n\n",
		name, formatCount(seq.LastValue), formatCount(seq.MaxValue), 100*seq.UsedFraction, formatCount(seq.MaxValue-seq.LastValue))
	if seq.TableName != "" {
		fmt.Fprintf(&desc, "It feeds column `%s` of table `%s` (type `%s`).\n\n", seq.ColumnName, seq.TableName, seq.ColumnType)
	}

	desc.WriteString("**Why it matters:**\n")
	desc.WriteString("When the sequence reaches its maximum, `nextval()` fails and every INSERT that needs a new ID errors ")
	desc.WriteString("until the column type is changed, which is slow to do in an emergency on a large table.\n\n")

	desc.WriteString("**What to do:**\n")
	intColumn := seq.ColumnType == "integer" || seq.ColumnType == "smallint"
	if seq.TableName != "" && intColumn {
		table := quoteQualified(seq.SchemaName, seq.TableName)
		desc.WriteString("Widen the column to `bigint`, and the sequence too if it was created `AS integer` (serial columns are):\n```sql\n")
		fmt.Fprintf(&desc, "ALTER TABLE %s ALTER COLUMN %s TYPE bigint;\n", table, quoteIdent(seq.ColumnName))
		fmt.Fprintf(&desc, "ALTER SEQUENCE %s AS bigint;\n```\n", name)
		desc.WriteString("- `ALTER COLUMN ... TYPE` rewrites the table and its indexes under an ACCESS EXCLUSIVE lock, ")
		desc.WriteString("blocking reads and writes for the duration. Schedule it, or on a large table add a new `bigint` column, ")
		desc.WriteString("backfill it in batches, and swap the columns in one short transaction.\n")
		desc.WriteString("- Columns in other tables that reference this one through foreign keys need the same change.\n")
	} else {
		desc.WriteString("The sequence's own `MAXVALUE` is the limit. Raise it if the consumer can store larger values:\n```sql\n")
		fmt.Fprintf(&desc, "ALTER SEQUENCE %s AS bigint MAXVALUE 9223372036854775807;\n```\n", name)
	}
	desc.WriteString("- Application code, ORMs and APIs that hold IDs as 32-bit integers must be checked as well.\n")
	return desc.String()
}

// Ensure SequenceExhaustionRule implements Rule interface.
var _ suggester.Rule = (*SequenceExhaustionRule)(nil)
