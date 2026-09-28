// Package plans collects generic plans of the busiest queries and turns their
// sequential scans into concrete index proposals.
package plans

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/plans"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

const (
	// CollectorName is the unique name for this collector.
	CollectorName = "query_plans"
	// DefaultInterval is the default collection interval.
	DefaultInterval = time.Hour
	// DefaultMaxQueries is how many of the busiest queries are planned.
	DefaultMaxQueries = 20
	// DefaultMinTableRows skips proposals on tables small enough to scan cheaply.
	DefaultMinTableRows = 10000

	// selectionWindow is the period queries are ranked by execution time over.
	selectionWindow = 24 * time.Hour
	// replanAfter is how long a plan is reused before the query is planned again.
	replanAfter = 24 * time.Hour
	// maxSelectivity is the share of a table's rows a filter may match before
	// an index stops paying off and the planner prefers a sequential scan.
	maxSelectivity = 0.2
	// btreeTupleOverhead approximates per-entry btree overhead in bytes.
	btreeTupleOverhead = 16
)

// Planner is the part of the PostgreSQL client the collector needs.
type Planner interface {
	ExplainGeneric(ctx context.Context, query string) (*models.ExplainPlan, error)
	HypoPGAvailable(ctx context.Context) (bool, error)
	ValidateIndex(ctx context.Context, query, createIndex string) (*models.IndexValidation, error)
	GetTableIndexInfo(ctx context.Context, schema, table string, columns []string) (*models.TableIndexInfo, error)
}

// Collector plans the busiest queries and records index proposals.
type Collector struct {
	collector.BaseCollector
	planner      Planner
	maxQueries   int
	minTableRows int64
	now          func() time.Time
}

// Config holds configuration for Collector.
type Config struct {
	PGClient     postgres.Client
	Planner      Planner
	Storage      sqlite.Storage
	InstanceID   int64
	Interval     time.Duration
	MaxQueries   int
	MinTableRows int64
	Logger       *log.Logger
}

// NewCollector creates a new query plan Collector.
func NewCollector(cfg Config) *Collector {
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.MaxQueries <= 0 {
		cfg.MaxQueries = DefaultMaxQueries
	}
	if cfg.MinTableRows <= 0 {
		cfg.MinTableRows = DefaultMinTableRows
	}
	return &Collector{
		BaseCollector: collector.NewBaseCollector(collector.BaseCollectorConfig{
			Name:       CollectorName,
			Interval:   cfg.Interval,
			PGClient:   cfg.PGClient,
			Storage:    cfg.Storage,
			InstanceID: cfg.InstanceID,
			Logger:     cfg.Logger,
		}),
		planner:      cfg.Planner,
		maxQueries:   cfg.MaxQueries,
		minTableRows: cfg.MinTableRows,
		now:          time.Now,
	}
}

// rankedQuery is a query with its execution time over the selection window.
type rankedQuery struct {
	id     int64
	text   string
	timeMs float64
}

// Collect plans the busiest queries and stores the report with the snapshot.
// A query that cannot be planned is recorded with its error; only a failure to
// read the query list or to store the report fails the collector.
func (c *Collector) Collect(ctx context.Context, snapshotID int64) error {
	ranked, total, err := c.rankQueries(ctx)
	if err != nil {
		return err
	}
	previous, err := c.previousFindings(ctx)
	if err != nil {
		c.Logf("warning: reading previous plans: %v", err)
	}

	report := &models.QueryPlanReport{}
	if ok, err := c.planner.HypoPGAvailable(ctx); err != nil {
		c.Logf("warning: checking for hypopg: %v", err)
	} else {
		report.HypoPG = ok
	}

	j := &judge{c: c, hypo: report.HypoPG, tables: make(map[string]*models.TableIndexInfo)}
	now := c.now()
	for _, q := range ranked {
		finding := models.QueryPlanFinding{
			QueryID:     q.id,
			Query:       q.text,
			TotalTimeMs: q.timeMs,
		}
		if total > 0 {
			finding.TimeShare = q.timeMs / total
		}

		if prev, ok := previous[q.id]; ok && prev.Query == q.text && now.Sub(prev.ExplainedAt) < replanAfter && prev.Error == "" {
			finding.ExplainedAt = prev.ExplainedAt
			finding.TotalCost = prev.TotalCost
			finding.SeqScans = prev.SeqScans
			// The plan is reused, but indexes may have been added since.
			for i := range finding.SeqScans {
				j.recheck(ctx, &finding.SeqScans[i])
			}
		} else {
			c.plan(ctx, j, &finding, now)
		}
		report.Queries = append(report.Queries, finding)
	}

	return c.Storage().SaveQueryPlans(ctx, snapshotID, report)
}

// plan explains one query and judges each of its sequential scans.
func (c *Collector) plan(ctx context.Context, j *judge, f *models.QueryPlanFinding, now time.Time) {
	f.ExplainedAt = now
	plan, err := c.planner.ExplainGeneric(ctx, f.Query)
	if err != nil {
		f.Error = err.Error()
		return
	}
	root, err := plans.Parse(plan.PlanJSON)
	if err != nil {
		f.Error = err.Error()
		return
	}
	f.TotalCost = root.TotalCost

	// Keep the plan for the query page, which explains it in plain language.
	plan.QueryID = f.QueryID
	plan.CapturedAt = now
	if _, err := c.Storage().SaveExplainPlan(ctx, plan); err != nil {
		c.Logf("warning: saving plan for query %d: %v", f.QueryID, err)
	}

	for _, s := range root.SeqScans() {
		scan := models.SeqScanFinding{
			Schema: s.Schema, Table: s.Table, Filter: s.Filter,
			PlanRows: s.PlanRows, Cost: s.Cost, Columns: s.Columns,
		}
		j.judge(ctx, f.Query, &scan)
		f.SeqScans = append(f.SeqScans, scan)
	}
}

// rankQueries returns the busiest explainable queries by execution time over
// the selection window, and the total execution time of all queries.
func (c *Collector) rankQueries(ctx context.Context) ([]rankedQuery, float64, error) {
	store := c.Storage()
	latest, err := store.GetLatestSnapshotWithCollector(ctx, c.InstanceID(), "query_stats", time.Time{})
	if err != nil {
		return nil, 0, fmt.Errorf("finding query statistics: %w", err)
	}
	if latest == nil {
		// At startup this collector runs alongside the first query collection.
		// Failing makes the coordinator retry on the next cycle instead of
		// waiting a full interval with nothing planned.
		return nil, 0, fmt.Errorf("no query statistics collected yet; will retry")
	}

	var all []rankedQuery
	baseline, err := store.GetLatestSnapshotWithCollector(ctx, c.InstanceID(), "query_stats", c.now().Add(-selectionWindow))
	if err != nil {
		return nil, 0, fmt.Errorf("finding baseline query statistics: %w", err)
	}
	if baseline != nil && baseline.ID != latest.ID {
		deltas, err := store.GetQueryStatsDelta(ctx, baseline.ID, latest.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("reading query statistics: %w", err)
		}
		for _, d := range deltas {
			all = append(all, rankedQuery{d.QueryID, d.Query, d.DeltaTotalTime})
		}
	} else {
		// Less than a window of history: fall back to totals since reset.
		stats, err := store.GetQueryStats(ctx, latest.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("reading query statistics: %w", err)
		}
		for _, s := range stats {
			all = append(all, rankedQuery{s.QueryID, s.Query, s.TotalExecTime})
		}
	}

	var total float64
	var candidates []rankedQuery
	for _, q := range all {
		total += max(q.timeMs, 0)
		if q.timeMs > 0 && postgres.Explainable(q.text) && !strings.Contains(q.text, "pganalyzer_plan_") {
			candidates = append(candidates, q)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].timeMs > candidates[j].timeMs })
	if len(candidates) > c.maxQueries {
		candidates = candidates[:c.maxQueries]
	}
	return candidates, total, nil
}

// previousFindings returns the findings of the latest stored report by query id.
func (c *Collector) previousFindings(ctx context.Context) (map[int64]models.QueryPlanFinding, error) {
	snap, err := c.Storage().GetLatestSnapshotWithCollector(ctx, c.InstanceID(), CollectorName, time.Time{})
	if err != nil || snap == nil {
		return nil, err
	}
	report, err := c.Storage().GetQueryPlans(ctx, snap.ID)
	if err != nil || report == nil {
		return nil, err
	}
	out := make(map[int64]models.QueryPlanFinding, len(report.Queries))
	for _, f := range report.Queries {
		out[f.QueryID] = f
	}
	return out, nil
}

// judge decides whether a sequential scan warrants an index. It caches table
// information for one collection run.
type judge struct {
	c      *Collector
	hypo   bool
	tables map[string]*models.TableIndexInfo
}

func (j *judge) tableInfo(ctx context.Context, schema, table string, columns []string) (*models.TableIndexInfo, error) {
	key := schema + "." + table + ":" + strings.Join(columns, ",")
	if info, ok := j.tables[key]; ok {
		return info, nil
	}
	info, err := j.c.planner.GetTableIndexInfo(ctx, schema, table, columns)
	if err != nil {
		return nil, err
	}
	j.tables[key] = info
	return info, nil
}

// judge fills in the proposal for a freshly planned scan: it drops scans on
// small tables, unselective filters and columns an index already covers, and
// asks hypopg when available.
func (j *judge) judge(ctx context.Context, query string, s *models.SeqScanFinding) {
	if len(s.Columns) == 0 {
		if s.Filter == "" {
			s.SkipReason = "reads the whole table without a filter"
		} else {
			s.SkipReason = "the filter cannot use a plain btree index (OR, functions or pattern matching)"
		}
		return
	}
	if !j.checkTable(ctx, s) {
		return
	}
	if !j.hypo {
		return
	}
	v, err := j.c.planner.ValidateIndex(ctx, query, plans.CreateIndexSQL(s.Schema, s.Table, s.Columns, "", false))
	if err != nil {
		j.c.Logf("warning: validating index on %s.%s: %v", s.Schema, s.Table, err)
		return
	}
	s.Validation = v
	if !v.UsesIndex {
		s.SkipReason = "the planner would not use this index (checked with hypopg)"
	}
}

// recheck re-applies the table checks to a reused scan, so a proposal
// disappears once the index has been created.
func (j *judge) recheck(ctx context.Context, s *models.SeqScanFinding) {
	if len(s.Columns) == 0 || s.SkipReason != "" {
		return
	}
	j.checkTable(ctx, s)
}

// checkTable applies the size, selectivity and existing-index checks. It
// reports whether the proposal survives.
func (j *judge) checkTable(ctx context.Context, s *models.SeqScanFinding) bool {
	info, err := j.tableInfo(ctx, s.Schema, s.Table, s.Columns)
	if err != nil {
		s.SkipReason = "could not read the table's indexes: " + err.Error()
		return false
	}
	s.EstimatedRows = info.EstimatedRows
	switch {
	case info.EstimatedRows < j.c.minTableRows:
		s.SkipReason = fmt.Sprintf("the table is small (~%d rows), so reading it whole is cheap", info.EstimatedRows)
		return false
	case s.PlanRows/float64(info.EstimatedRows) > maxSelectivity:
		s.SkipReason = fmt.Sprintf("the filter matches ~%.0f%% of the table, too much for an index to help", 100*s.PlanRows/float64(info.EstimatedRows))
		return false
	}
	for _, idx := range info.Indexes {
		if plans.HasPrefix(idx.Columns, s.Columns) {
			s.SkipReason = fmt.Sprintf("index %s already covers these columns but the planner prefers a sequential scan; its statistics may be stale (run ANALYZE)", idx.Name)
			return false
		}
	}
	s.EstimatedBytes = info.EstimatedRows * int64(info.AvgKeyWidth+btreeTupleOverhead)
	return true
}

// Ensure Collector implements collector.Collector.
var _ collector.Collector = (*Collector)(nil)
