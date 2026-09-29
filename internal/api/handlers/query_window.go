package handlers

import (
	"context"
	"sort"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
)

// defaultSlowQueryMs is used when no threshold is configured.
const defaultSlowQueryMs = 1000

// QueryWindowConfig makes dashboard query figures cover the slow-query window,
// as the analyzer judges them, instead of the lifetime of pg_stat_statements.
type QueryWindowConfig struct {
	Storage     analyzer.QueryWindowStorage
	Window      time.Duration
	SlowQueryMs float64
}

// querySummary is what the dashboards show about queries.
type querySummary struct {
	Total int
	Slow  int
	Top   []DashboardQuery
	// Window is the span covered, 0 for lifetime statistics.
	Window time.Duration
}

// summarizeQueries uses the recent window when enough history exists and the
// lifetime statistics otherwise.
func summarizeQueries(ctx context.Context, cfg *QueryWindowConfig, instanceID int64, lifetime []models.QueryStat, topN int) (querySummary, error) {
	slowMs := float64(defaultSlowQueryMs)
	if cfg != nil && cfg.SlowQueryMs > 0 {
		slowMs = cfg.SlowQueryMs
	}

	var rows []DashboardQuery
	var summary querySummary
	var w *analyzer.QueryWindow
	var err error
	if cfg != nil && cfg.Storage != nil {
		w, err = analyzer.RecentQueryStats(ctx, cfg.Storage, instanceID, cfg.Window, time.Now())
	}
	if w != nil {
		summary.Window = w.Span()
		for _, q := range w.Queries {
			rows = append(rows, DashboardQuery{QueryID: q.QueryID, QueryPreview: truncateString(q.Query, 80),
				Calls: q.DeltaCalls, MeanExecTimeMs: q.MeanExecTime, TotalExecTimeMs: q.DeltaTotalTime})
		}
	} else {
		for _, q := range lifetime {
			rows = append(rows, DashboardQuery{QueryID: q.QueryID, QueryPreview: truncateString(q.Query, 80),
				Calls: q.Calls, MeanExecTimeMs: q.MeanExecTime, TotalExecTimeMs: q.TotalExecTime})
		}
	}

	summary.Total = len(rows)
	for _, r := range rows {
		if r.MeanExecTimeMs >= slowMs {
			summary.Slow++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].TotalExecTimeMs > rows[j].TotalExecTimeMs })
	if len(rows) > topN {
		rows = rows[:topN]
	}
	summary.Top = rows
	// A failed window read falls back to lifetime figures; the error is
	// returned for logging only.
	return summary, err
}
