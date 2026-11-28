// Package verify checks whether a resolved suggestion actually made its queries
// faster, by comparing their query history before and after the resolution.
//
// Only suggestions tied to specific queries can be verified: slow_query (one
// query) and index_recommendation (the queries the index was proposed for).
package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// Verdicts, for a query and for a suggestion as a whole.
const (
	// VerdictImproved means the mean time fell by at least ImprovedRatio.
	VerdictImproved = "improved"
	// VerdictRegressed means the mean time rose by at least RegressedRatio.
	VerdictRegressed = "regressed"
	// VerdictUnchanged means the mean time moved less than either ratio.
	VerdictUnchanged = "unchanged"
	// VerdictStopped means the query did not run at all after the resolution,
	// over a full window.
	VerdictStopped = "stopped"
	// VerdictPending means there is not enough history after the resolution yet.
	VerdictPending = "pending"
	// VerdictNoData means there is no usable history before the resolution,
	// e.g. because it has aged out of query retention.
	VerdictNoData = "no_data"
)

const (
	// ImprovedRatio and RegressedRatio bound "unchanged": run-to-run noise in
	// a mean over hours is well within 20%.
	ImprovedRatio  = 0.8
	RegressedRatio = 1.2
	// MinAfterSpan and MinCalls are the least evidence after the resolution
	// before a verdict is given.
	MinAfterSpan = time.Hour
	MinCalls     = 10
	// maxSamples bounds each page read per period; maxPeriodSamples caps total
	// samples read per period even for multi-week windows.
	maxSamples       = 10000
	maxPeriodSamples = 50000
	// baselineLeadIn is how far before a period its baseline sample is looked
	// for; several collection intervals.
	baselineLeadIn = time.Hour
)

// Storage is the query history the verifier reads.
type Storage interface {
	GetQueryHistory(ctx context.Context, instanceID, queryID int64, from, to time.Time, limit, offset int) ([]models.QueryHistorySample, error)
}

// Verifier compares query history around a suggestion's resolution.
type Verifier struct {
	storage Storage
	window  time.Duration
}

// New creates a Verifier. window is the slow-query window: how much history
// each side of the comparison covers.
func New(storage Storage, window time.Duration) *Verifier {
	if window <= 0 {
		window = 24 * time.Hour
	}
	return &Verifier{storage: storage, window: window}
}

// Period is a query's execution over part of the comparison.
type Period struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Calls  int64     `json:"calls"`
	MeanMs float64   `json:"mean_ms"`
}

// QueryOutcome is one query's before/after comparison.
type QueryOutcome struct {
	QueryID int64   `json:"queryid"`
	Query   string  `json:"query,omitempty"`
	Before  *Period `json:"before,omitempty"`
	After   *Period `json:"after,omitempty"`
	Verdict string  `json:"verdict"`
	// Ratio is the after mean over the before mean; 0 when either is missing.
	Ratio float64 `json:"ratio,omitempty"`
}

// Result is the verification of one resolved suggestion.
type Result struct {
	ResolvedAt time.Time      `json:"resolved_at"`
	Verdict    string         `json:"verdict"`
	Summary    string         `json:"summary"`
	Queries    []QueryOutcome `json:"queries"`
}

// Verify compares the history of the suggestion's queries before and after it
// resolved. It returns nil for suggestions that are not resolved or not tied
// to queries.
func (v *Verifier) Verify(ctx context.Context, sug models.Suggestion, now time.Time) (*Result, error) {
	if sug.Status != models.StatusResolved || sug.ResolvedAt == nil {
		return nil, nil
	}
	ids, lagging := queryIDs(sug)
	if len(ids) == 0 {
		return nil, nil
	}
	resolved := *sug.ResolvedAt
	var gap time.Duration
	if lagging {
		gap = v.window
	}

	// An index recommendation retires within one planning cycle of the index
	// being built, so the fix is at the resolution. A slow query resolves
	// once its trailing-window mean drops below the threshold, which can be up
	// to a window after the fix, so its "before" ends a window earlier.
	before := [2]time.Time{resolved.Add(-gap - v.window), resolved.Add(-gap)}
	after := [2]time.Time{resolved, minTime(resolved.Add(v.window), now)}

	res := &Result{ResolvedAt: resolved}
	for _, id := range ids {
		// Read before and after windows separately with pagination so large
		// windows never drop older "before" samples behind a DESC LIMIT.
		beforeSamples, err := v.readHistory(ctx, sug.InstanceID, id, before[0].Add(-baselineLeadIn), before[1])
		if err != nil {
			return nil, fmt.Errorf("reading before history of query %d: %w", id, err)
		}
		afterSamples, err := v.readHistory(ctx, sug.InstanceID, id, after[0].Add(-baselineLeadIn), after[1])
		if err != nil {
			return nil, fmt.Errorf("reading after history of query %d: %w", id, err)
		}
		res.Queries = append(res.Queries, comparePeriods(id, beforeSamples, afterSamples, before, after, now.Sub(resolved) >= v.window))
	}
	res.Verdict = overall(res.Queries)
	res.Summary = summarize(res)
	return res, nil
}

func (v *Verifier) readHistory(ctx context.Context, instanceID, queryID int64, from, to time.Time) ([]models.QueryHistorySample, error) {
	var all []models.QueryHistorySample
	for offset := 0; len(all) < maxPeriodSamples; {
		limit := min(maxSamples, maxPeriodSamples-len(all))
		batch, err := v.storage.GetQueryHistory(ctx, instanceID, queryID, from, to, limit, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < limit {
			break
		}
		offset += len(batch)
	}
	return all, nil
}

// queryIDs returns the queries a suggestion is about, and whether its
// resolution lags the fix by up to a window.
func queryIDs(sug models.Suggestion) ([]int64, bool) {
	var meta struct {
		QueryID  json.Number   `json:"queryid"`
		QueryIDs []json.Number `json:"query_ids"`
	}
	// UseNumber keeps 64-bit query ids exact.
	dec := json.NewDecoder(bytes.NewReader([]byte(sug.Metadata)))
	dec.UseNumber()
	if sug.Metadata == "" || dec.Decode(&meta) != nil {
		return nil, false
	}
	switch sug.RuleID {
	case "slow_query":
		if id, err := meta.QueryID.Int64(); err == nil {
			return []int64{id}, true
		}
	case "index_recommendation":
		var ids []int64
		for _, n := range meta.QueryIDs {
			if id, err := n.Int64(); err == nil {
				ids = append(ids, id)
			}
		}
		return ids, false
	}
	return nil, false
}

// comparePeriods computes one query's before/after periods and verdict.
// afterComplete reports whether a full window has passed since the resolution.
func comparePeriods(id int64, beforeSamples, afterSamples []models.QueryHistorySample, before, after [2]time.Time, afterComplete bool) QueryOutcome {
	// Storage returns newest first.
	sort.Slice(beforeSamples, func(i, j int) bool { return beforeSamples[i].SampledAt.Before(beforeSamples[j].SampledAt) })
	sort.Slice(afterSamples, func(i, j int) bool { return afterSamples[i].SampledAt.Before(afterSamples[j].SampledAt) })
	out := QueryOutcome{QueryID: id}
	if len(afterSamples) > 0 {
		out.Query = afterSamples[len(afterSamples)-1].Query
	} else if len(beforeSamples) > 0 {
		out.Query = beforeSamples[len(beforeSamples)-1].Query
	}
	out.Before = period(beforeSamples, before[0], before[1])
	out.After = period(afterSamples, after[0], after[1])

	switch {
	case out.Before == nil || out.Before.Calls == 0 || out.Before.MeanMs <= 0:
		out.Verdict = VerdictNoData
	case afterComplete && (out.After == nil || out.After.Calls == 0):
		out.Verdict = VerdictStopped
	case out.After == nil || out.After.To.Sub(out.After.From) < MinAfterSpan || out.After.Calls < MinCalls:
		if afterComplete {
			// The post-resolution window is closed, so waiting will never add calls.
			out.Verdict = VerdictNoData
		} else {
			out.Verdict = VerdictPending
		}
	default:
		out.Ratio = out.After.MeanMs / out.Before.MeanMs
		switch {
		case out.Ratio <= ImprovedRatio:
			out.Verdict = VerdictImproved
		case out.Ratio >= RegressedRatio:
			out.Verdict = VerdictRegressed
		default:
			out.Verdict = VerdictUnchanged
		}
	}
	return out
}

// period sums execution between from and to. Samples hold cumulative
// pg_stat_statements counters, so the period is the sum of the deltas between
// consecutive samples. Each delta covers the interval ending at its sample, so
// a sample at or before from (within baselineLeadIn) is only the baseline. A
// counter that went down was reset, and counts from zero.
func period(samples []models.QueryHistorySample, from, to time.Time) *Period {
	var prev *models.QueryHistorySample
	var p *Period
	for i := range samples {
		s := &samples[i]
		if s.SampledAt.After(to) {
			break
		}
		if !s.SampledAt.After(from) {
			// Ignore stale baselines older than baselineLeadIn so a collection
			// outage before from does not blend pre-fix execution into the period.
			if from.Sub(s.SampledAt) <= baselineLeadIn {
				prev = s
			}
			continue
		}
		if p == nil {
			p = &Period{From: s.SampledAt}
			if prev != nil {
				p.From = prev.SampledAt
			}
		}
		if prev != nil {
			calls, total := s.Calls-prev.Calls, s.TotalExecTime-prev.TotalExecTime
			if calls < 0 || total < 0 {
				calls, total = s.Calls, s.TotalExecTime
			}
			p.Calls += calls
			p.MeanMs += total // summed here, divided below
		}
		p.To = s.SampledAt
		prev = s
	}
	if p == nil {
		return nil
	}
	if p.Calls > 0 {
		p.MeanMs /= float64(p.Calls)
	} else {
		p.MeanMs = 0
	}
	return p
}

// overall combines per-query verdicts: any regression wins, then any
// improvement, and pending only while every query is still pending.
func overall(qs []QueryOutcome) string {
	counts := map[string]int{}
	for _, q := range qs {
		counts[q.Verdict]++
	}
	for _, v := range []string{VerdictRegressed, VerdictImproved, VerdictUnchanged, VerdictStopped, VerdictPending} {
		if counts[v] > 0 {
			return v
		}
	}
	return VerdictNoData
}

// summarize renders the result in one line, e.g. "3.2s → 40ms (99% faster)".
func summarize(r *Result) string {
	if len(r.Queries) == 1 {
		return describe(r.Queries[0])
	}
	counts := map[string]int{}
	for _, q := range r.Queries {
		counts[q.Verdict]++
	}
	var parts []string
	for _, v := range []struct{ verdict, label string }{
		{VerdictImproved, "faster"}, {VerdictRegressed, "slower"}, {VerdictUnchanged, "unchanged"},
		{VerdictStopped, "no longer running"}, {VerdictPending, "waiting for data"}, {VerdictNoData, "no earlier history"},
	} {
		if n := counts[v.verdict]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, v.label))
		}
	}
	return fmt.Sprintf("%d queries: %s", len(r.Queries), strings.Join(parts, ", "))
}

// describe renders one query's outcome.
func describe(q QueryOutcome) string {
	switch q.Verdict {
	case VerdictNoData:
		if q.Before != nil && q.Before.Calls > 0 && q.Before.MeanMs > 0 && q.After != nil && q.After.Calls > 0 {
			return "too few calls after the fix to compare"
		}
		return "no query history from before the fix"
	case VerdictPending:
		return "waiting for more calls after the fix"
	case VerdictStopped:
		return fmt.Sprintf("was %s on average; has not run since", FormatMs(q.Before.MeanMs))
	}
	change := fmt.Sprintf("%.0f%% faster", 100*(1-q.Ratio))
	switch q.Verdict {
	case VerdictRegressed:
		change = fmt.Sprintf("%.0f%% slower", 100*(q.Ratio-1))
	case VerdictUnchanged:
		change = "no significant change"
	}
	return fmt.Sprintf("%s → %s (%s)", FormatMs(q.Before.MeanMs), FormatMs(q.After.MeanMs), change)
}

// FormatMs renders a duration in milliseconds, e.g. "40ms" or "3.2s".
func FormatMs(ms float64) string {
	switch {
	case ms >= 10_000:
		return fmt.Sprintf("%.0fs", ms/1000)
	case ms >= 1000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms >= 10:
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fms", ms)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
