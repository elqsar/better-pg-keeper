package analyzer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// largeUnusedIndexBytes is the size from which an unused index is worth a
// warning rather than an informational note.
const largeUnusedIndexBytes = 100 * 1024 * 1024

// IndexAnalyzer detects issues with indexes such as unused or duplicate indexes.
type IndexAnalyzer struct {
	storage Storage
	config  *Config
}

// NewIndexAnalyzer creates a new IndexAnalyzer.
func NewIndexAnalyzer(storage Storage, cfg *Config) *IndexAnalyzer {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &IndexAnalyzer{
		storage: storage,
		config:  cfg,
	}
}

// Analyze detects index-related issues from the given snapshot.
func (a *IndexAnalyzer) Analyze(ctx context.Context, snapshotID int64) ([]IndexIssue, error) {
	indexStats, err := a.storage.GetIndexStats(ctx, snapshotID)
	if err != nil {
		return nil, err
	}

	if len(indexStats) == 0 {
		return nil, nil
	}

	snapshot, err := a.storage.GetSnapshotByID(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	asOf := time.Now()
	if snapshot != nil {
		asOf = snapshot.CapturedAt
	}

	var issues []IndexIssue

	// Detect unused indexes
	unusedIssues := a.detectUnusedIndexes(indexStats, asOf)
	issues = append(issues, unusedIssues...)

	// Detect duplicate indexes
	duplicateIssues := a.detectDuplicateIndexes(indexStats)
	issues = append(issues, duplicateIssues...)

	// Sort by potential space savings (largest first)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].SpaceSavings > issues[j].SpaceSavings
	})

	return issues, nil
}

// detectUnusedIndexes finds indexes with zero scans over a long enough window.
//
// Dropping an index is the most damaging thing this tool recommends, so an index
// is only reported when idx_scan has been counting for at least UnusedIndexDays.
// Right after a stats reset or restart every index reads zero, and an unknown
// window proves nothing. Indexes that enforce constraints or serve foreign-key
// lookups are skipped, since zero scans does not make them unnecessary.
func (a *IndexAnalyzer) detectUnusedIndexes(stats []models.IndexStat, asOf time.Time) []IndexIssue {
	var issues []IndexIssue
	minWindow := time.Duration(a.config.UnusedIndexDays) * 24 * time.Hour

	for _, stat := range stats {
		// Skip primary keys and unique indexes - they serve constraint purposes
		if stat.IsPrimary || stat.IsUnique {
			continue
		}

		// Skip if index has been used
		if stat.IdxScan > 0 {
			continue
		}

		// Referential checks on the referenced table look rows up through it,
		// and those lookups are what make deletes fast.
		if stat.BacksForeignKey {
			continue
		}

		// Skip tiny indexes (less than 8KB)
		if stat.IndexSize < 8192 {
			continue
		}

		if stat.StatsSince == nil {
			continue
		}
		window := asOf.Sub(*stat.StatsSince)
		if window < minWindow {
			continue
		}

		severity := models.SeverityInfo
		if stat.IndexSize >= largeUnusedIndexBytes {
			severity = models.SeverityWarning
		}

		issues = append(issues, IndexIssue{
			SchemaName: stat.SchemaName,
			TableName:  stat.RelName,
			IndexName:  stat.IndexRelName,
			IssueType:  IndexIssueUnused,
			Severity:   severity,
			Description: fmt.Sprintf(
				"Index has not been scanned in %d days. Consider dropping to save %s.",
				int(window.Hours()/24), formatBytes(stat.IndexSize),
			),
			IndexSize:    stat.IndexSize,
			IdxScan:      stat.IdxScan,
			IsUnique:     stat.IsUnique,
			IsPrimary:    stat.IsPrimary,
			SpaceSavings: stat.IndexSize,
			IndexDef:     stat.IndexDef,
			StatsWindow:  window,
		})
	}

	return issues
}

// detectDuplicateIndexes finds indexes made redundant by another index on the
// same table, comparing their definitions: access method, key columns with their
// opclass, collation and sort order, INCLUDE columns, expressions and predicate.
//
// An index is redundant when another index has the same shape and its key columns
// start with this index's key columns (a strict prefix only counts for btree,
// which is the only method that can use a leading subset of its keys). Unique and
// primary-key indexes are never reported, as they enforce constraints. Of two
// identical non-unique indexes, only the less-scanned one is reported.
func (a *IndexAnalyzer) detectDuplicateIndexes(stats []models.IndexStat) []IndexIssue {
	var issues []IndexIssue

	tableIndexes := make(map[string][]models.IndexStat)
	for _, stat := range stats {
		// Rows collected before definitions were recorded cannot be compared.
		if stat.KeyColumns == "" {
			continue
		}
		key := stat.SchemaName + "." + stat.RelName
		tableIndexes[key] = append(tableIndexes[key], stat)
	}

	for _, indexes := range tableIndexes {
		// Prefer the most-scanned index as the one to keep, so a candidate is
		// reported against the index queries actually use.
		sort.Slice(indexes, func(i, j int) bool {
			if indexes[i].IdxScan != indexes[j].IdxScan {
				return indexes[i].IdxScan > indexes[j].IdxScan
			}
			return indexes[i].IndexRelName < indexes[j].IndexRelName
		})

		// Point each redundant index at one that is itself kept, so the
		// suggestions never reference an index another suggestion drops.
		redundant := make(map[string]bool)
		for _, candidate := range indexes {
			for _, other := range indexes {
				if candidate.IndexRelName == other.IndexRelName {
					continue
				}
				if _, ok := redundantWith(candidate, other); ok {
					redundant[candidate.IndexRelName] = true
					break
				}
			}
		}

		for _, candidate := range indexes {
			if !redundant[candidate.IndexRelName] {
				continue
			}
			for _, retained := range indexes {
				if candidate.IndexRelName == retained.IndexRelName || redundant[retained.IndexRelName] {
					continue
				}
				exact, ok := redundantWith(candidate, retained)
				if !ok {
					continue
				}

				issues = append(issues, IndexIssue{
					SchemaName: candidate.SchemaName,
					TableName:  candidate.RelName,
					IndexName:  candidate.IndexRelName,
					IssueType:  IndexIssueDuplicate,
					Severity:   models.SeverityInfo,
					Description: fmt.Sprintf(
						"Index is covered by '%s'. Has %d scans vs %d. Consider dropping to save %s.",
						retained.IndexRelName, candidate.IdxScan, retained.IdxScan, formatBytes(candidate.IndexSize),
					),
					IndexSize:          candidate.IndexSize,
					IdxScan:            candidate.IdxScan,
					IsUnique:           candidate.IsUnique,
					IsPrimary:          candidate.IsPrimary,
					DuplicateOf:        retained.IndexRelName,
					DuplicateOfIdxScan: retained.IdxScan,
					SpaceSavings:       candidate.IndexSize,
					IndexDef:           candidate.IndexDef,
					DuplicateOfDef:     retained.IndexDef,
					ExactDuplicate:     exact,
				})
				break
			}
		}
	}

	return issues
}

// redundantWith reports whether candidate can be dropped because retained serves
// every lookup it does, and whether the two are identical.
func redundantWith(candidate, retained models.IndexStat) (exact, ok bool) {
	if candidate.IsPrimary || candidate.IsUnique {
		return false, false
	}
	if candidate.AccessMethod != retained.AccessMethod ||
		candidate.Expressions != retained.Expressions ||
		candidate.Predicate != retained.Predicate {
		return false, false
	}

	candKeys := strings.Fields(candidate.KeyColumns)
	retKeys := strings.Fields(retained.KeyColumns)
	if len(candKeys) > len(retKeys) {
		return false, false
	}
	for i, k := range candKeys {
		if retKeys[i] != k {
			return false, false
		}
	}

	// Columns the retained index carries, so INCLUDE columns the candidate
	// provides for index-only scans are still available.
	retCols := make(map[string]bool)
	for _, k := range retKeys {
		retCols[strings.SplitN(k, ":", 2)[0]] = true
	}
	for _, c := range strings.Fields(retained.IncludeColumns) {
		retCols[c] = true
	}
	for _, c := range strings.Fields(candidate.IncludeColumns) {
		if !retCols[c] {
			return false, false
		}
	}

	sameKeys := len(candKeys) == len(retKeys)
	if !sameKeys && (candidate.AccessMethod != "btree" || candidate.Expressions != "") {
		return false, false
	}

	exact = sameKeys && sameColumnSet(candidate.IncludeColumns, retained.IncludeColumns)
	if exact && !retained.IsUnique && !retained.IsPrimary {
		// Two identical plain indexes: keep the more-scanned one, breaking ties
		// by name so exactly one of the pair is reported.
		if candidate.IdxScan > retained.IdxScan ||
			(candidate.IdxScan == retained.IdxScan && candidate.IndexRelName < retained.IndexRelName) {
			return false, false
		}
	}

	return exact, true
}

// sameColumnSet compares space-separated column lists ignoring order.
func sameColumnSet(a, b string) bool {
	as, bs := strings.Fields(a), strings.Fields(b)
	if len(as) != len(bs) {
		return false
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// formatBytes formats byte count as human-readable string.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
