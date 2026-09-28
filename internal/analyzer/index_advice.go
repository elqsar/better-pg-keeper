package analyzer

import (
	"context"
	"fmt"
	"sort"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/plans"
)

// IndexRecommendation is an index proposed from the plans of one or more
// queries that scan a table sequentially.
type IndexRecommendation struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
	// Queries are the queries that would use the index, busiest first.
	Queries []RecommendedFor `json:"queries"`
	// TimeShare is the combined share of execution time of those queries (0-1).
	TimeShare      float64 `json:"time_share"`
	EstimatedRows  int64   `json:"estimated_rows"`
	EstimatedBytes int64   `json:"estimated_bytes"`
	// Validated is set when hypopg confirmed the planner would use the index.
	Validated bool `json:"validated"`
}

// RecommendedFor is one query an index recommendation serves.
type RecommendedFor struct {
	QueryID    int64                   `json:"queryid"`
	Query      string                  `json:"query"`
	TimeShare  float64                 `json:"time_share"`
	Filter     string                  `json:"filter"`
	Validation *models.IndexValidation `json:"validation,omitempty"`
}

// QueryPlanAnalysis is the query_plans domain: the stored report and the
// recommendations derived from it.
type QueryPlanAnalysis struct {
	Report          *models.QueryPlanReport `json:"report"`
	Recommendations []IndexRecommendation   `json:"recommendations"`
}

// RecommendationsFor returns the recommendations on one table.
func (a *QueryPlanAnalysis) RecommendationsFor(schema, table string) []IndexRecommendation {
	if a == nil {
		return nil
	}
	var out []IndexRecommendation
	for _, r := range a.Recommendations {
		if r.Schema == schema && r.Table == table {
			out = append(out, r)
		}
	}
	return out
}

func (a *MainAnalyzer) analyzeQueryPlans(ctx context.Context, snapshotID int64) (*QueryPlanAnalysis, error) {
	report, err := a.storage.GetQueryPlans(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("getting query plans: %w", err)
	}
	if report == nil {
		return nil, nil
	}
	return &QueryPlanAnalysis{Report: report, Recommendations: recommendIndexes(report)}, nil
}

// recommendIndexes groups the surviving proposals by table. When one proposal
// is a leading prefix of another on the same table, the longer index serves
// both queries, so they are merged into it.
func recommendIndexes(report *models.QueryPlanReport) []IndexRecommendation {
	type proposal struct {
		scan  models.SeqScanFinding
		query models.QueryPlanFinding
	}
	byTable := make(map[string][]proposal)
	var tables []string
	for _, q := range report.Queries {
		for _, s := range q.SeqScans {
			if len(s.Columns) == 0 || s.SkipReason != "" {
				continue
			}
			key := s.Schema + "." + s.Table
			if _, ok := byTable[key]; !ok {
				tables = append(tables, key)
			}
			byTable[key] = append(byTable[key], proposal{s, q})
		}
	}

	var recs []IndexRecommendation
	for _, key := range tables {
		props := byTable[key]
		// Longest first, so shorter proposals find the index that covers them.
		sort.SliceStable(props, func(i, j int) bool { return len(props[i].scan.Columns) > len(props[j].scan.Columns) })

		var tableRecs []IndexRecommendation
		for _, p := range props {
			target := -1
			for i := range tableRecs {
				if plans.HasPrefix(tableRecs[i].Columns, p.scan.Columns) {
					target = i
					break
				}
			}
			if target < 0 {
				tableRecs = append(tableRecs, IndexRecommendation{
					Schema: p.scan.Schema, Table: p.scan.Table, Columns: p.scan.Columns,
					EstimatedRows: p.scan.EstimatedRows, EstimatedBytes: p.scan.EstimatedBytes,
				})
				target = len(tableRecs) - 1
			}
			r := &tableRecs[target]
			if hasQuery(r.Queries, p.query.QueryID) {
				continue
			}
			r.Queries = append(r.Queries, RecommendedFor{
				QueryID: p.query.QueryID, Query: p.query.Query, TimeShare: p.query.TimeShare,
				Filter: p.scan.Filter, Validation: p.scan.Validation,
			})
			r.TimeShare += p.query.TimeShare
			if p.scan.Validation != nil && p.scan.Validation.UsesIndex {
				r.Validated = true
			}
		}
		for i := range tableRecs {
			sort.Slice(tableRecs[i].Queries, func(a, b int) bool {
				return tableRecs[i].Queries[a].TimeShare > tableRecs[i].Queries[b].TimeShare
			})
		}
		recs = append(recs, tableRecs...)
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].TimeShare > recs[j].TimeShare })
	return recs
}

func hasQuery(qs []RecommendedFor, id int64) bool {
	for _, q := range qs {
		if q.QueryID == id {
			return true
		}
	}
	return false
}
