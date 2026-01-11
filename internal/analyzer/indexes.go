package analyzer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/elqsar/pganalyzer/internal/models"
)

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

	var issues []IndexIssue

	// Detect unused indexes
	unusedIssues := a.detectUnusedIndexes(indexStats)
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

// detectUnusedIndexes finds indexes with zero scans.
// Excludes primary keys and unique indexes as they serve constraint purposes.
func (a *IndexAnalyzer) detectUnusedIndexes(stats []models.IndexStat) []IndexIssue {
	var issues []IndexIssue

	for _, stat := range stats {
		// Skip primary keys and unique indexes - they serve constraint purposes
		if stat.IsPrimary || stat.IsUnique {
			continue
		}

		// Skip if index has been used
		if stat.IdxScan > 0 {
			continue
		}

		// Skip tiny indexes (less than 8KB)
		if stat.IndexSize < 8192 {
			continue
		}

		severity := models.SeverityWarning
		if stat.IndexSize > 1024*1024 { // > 1MB
			severity = models.SeverityCritical
		}

		issues = append(issues, IndexIssue{
			SchemaName: stat.SchemaName,
			TableName:  stat.RelName,
			IndexName:  stat.IndexRelName,
			IssueType:  IndexIssueUnused,
			Severity:   severity,
			Description: fmt.Sprintf(
				"Index has never been used (0 scans). Consider dropping to save %s.",
				formatBytes(stat.IndexSize),
			),
			IndexSize:    stat.IndexSize,
			IdxScan:      stat.IdxScan,
			IsUnique:     stat.IsUnique,
			IsPrimary:    stat.IsPrimary,
			SpaceSavings: stat.IndexSize,
		})
	}

	return issues
}

// detectDuplicateIndexes finds indexes on the same table that might be redundant.
// This is a heuristic based on index names and sizes.
// Note: Full duplicate detection would require index definitions from PostgreSQL.
func (a *IndexAnalyzer) detectDuplicateIndexes(stats []models.IndexStat) []IndexIssue {
	var issues []IndexIssue

	// Group indexes by table
	tableIndexes := make(map[string][]models.IndexStat)
	for _, stat := range stats {
		key := stat.SchemaName + "." + stat.RelName
		tableIndexes[key] = append(tableIndexes[key], stat)
	}

	// Check each table for potential duplicates
	for _, indexes := range tableIndexes {
		if len(indexes) < 2 {
			continue
		}

		// Sort by index size descending
		sort.Slice(indexes, func(i, j int) bool {
			return indexes[i].IndexSize > indexes[j].IndexSize
		})

		// Check for indexes with similar sizes (potential duplicates)
		// This is a heuristic - same size doesn't guarantee duplicate
		// but very similar sizes on same table often indicates redundancy
		for i := range len(indexes) {
			for j := i + 1; j < len(indexes); j++ {
				idx1 := indexes[i]
				idx2 := indexes[j]

				// Skip if either is primary key
				if idx1.IsPrimary || idx2.IsPrimary {
					continue
				}

				// Check if sizes are within 10% of each other
				if idx1.IndexSize == 0 || idx2.IndexSize == 0 {
					continue
				}

				sizeDiff := float64(idx1.IndexSize-idx2.IndexSize) / float64(idx1.IndexSize)
				if sizeDiff < 0 {
					sizeDiff = -sizeDiff
				}

				// Check for naming patterns that suggest duplicates
				if a.arePotentialDuplicates(idx1.IndexRelName, idx2.IndexRelName) && sizeDiff < 0.1 {
					// The less-used one is the candidate for removal
					lessUsed := idx1
					moreUsed := idx2
					if idx1.IdxScan > idx2.IdxScan {
						lessUsed = idx2
						moreUsed = idx1
					}

					// Skip if less-used is a unique constraint
					if lessUsed.IsUnique {
						continue
					}

					issues = append(issues, IndexIssue{
						SchemaName: lessUsed.SchemaName,
						TableName:  lessUsed.RelName,
						IndexName:  lessUsed.IndexRelName,
						IssueType:  IndexIssueDuplicate,
						Severity:   models.SeverityInfo,
						Description: fmt.Sprintf(
							"Index may be redundant with '%s'. Has %d scans vs %d. Review and consider dropping to save %s.",
							moreUsed.IndexRelName, lessUsed.IdxScan, moreUsed.IdxScan, formatBytes(lessUsed.IndexSize),
						),
						IndexSize:    lessUsed.IndexSize,
						IdxScan:      lessUsed.IdxScan,
						IsUnique:     lessUsed.IsUnique,
						IsPrimary:    lessUsed.IsPrimary,
						DuplicateOf:  moreUsed.IndexRelName,
						SpaceSavings: lessUsed.IndexSize,
					})
				}
			}
		}
	}

	return issues
}

// arePotentialDuplicates checks if two index names suggest they might be duplicates.
// This is a heuristic based on common naming patterns.
func (a *IndexAnalyzer) arePotentialDuplicates(name1, name2 string) bool {
	// Normalize names for comparison
	n1 := strings.ToLower(name1)
	n2 := strings.ToLower(name2)

	// Check for common prefixes (after removing common suffixes)
	suffixes := []string{"_idx", "_index", "_ix", "_1", "_2", "_new", "_old", "_backup", "_v2"}

	clean1 := n1
	clean2 := n2
	for _, suffix := range suffixes {
		clean1 = strings.TrimSuffix(clean1, suffix)
		clean2 = strings.TrimSuffix(clean2, suffix)
	}

	// If cleaned names are identical, they're likely duplicates
	if clean1 == clean2 && clean1 != n1 && clean2 != n2 {
		return true
	}

	// Check if one contains the other (common with legacy indexes)
	if strings.Contains(n1, n2) || strings.Contains(n2, n1) {
		// Only if there's significant overlap
		if min(len(n1), len(n2)) > 5 {
			return true
		}
	}

	return false
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
