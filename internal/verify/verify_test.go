package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// fakeHistory serves cumulative samples per query, newest first like storage,
// honoring limit and offset.
type fakeHistory map[int64][]models.QueryHistorySample

func (f fakeHistory) GetQueryHistory(ctx context.Context, instanceID, queryID int64, from, to time.Time, limit, offset int) ([]models.QueryHistorySample, error) {
	var out []models.QueryHistorySample
	for _, s := range f[queryID] {
		if !s.SampledAt.Before(from) && !s.SampledAt.After(to) {
			out = append([]models.QueryHistorySample{s}, out...)
		}
	}
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// history builds 5-minute cumulative samples from start to end, where
// meanAt(t) gives the mean time of the 10 calls made in each interval.
func history(id int64, start, end time.Time, meanAt func(time.Time) float64) []models.QueryHistorySample {
	var out []models.QueryHistorySample
	var calls int64
	var total float64
	for t := start; !t.After(end); t = t.Add(5 * time.Minute) {
		if m := meanAt(t); m >= 0 {
			calls += 10
			total += 10 * m
		}
		s := models.QueryHistorySample{SampledAt: t}
		s.QueryID, s.Query, s.Calls, s.TotalExecTime = id, "SELECT * FROM orders WHERE status = $1", calls, total
		out = append(out, s)
	}
	return out
}

func resolvedSuggestion(rule, metadata string, at time.Time) models.Suggestion {
	return models.Suggestion{ID: 1, InstanceID: 1, RuleID: rule, Status: models.StatusResolved, ResolvedAt: &at, Metadata: metadata}
}

func TestVerifyIndexRecommendation(t *testing.T) {
	resolved := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	now := resolved.Add(48 * time.Hour)
	start := resolved.Add(-3 * 24 * time.Hour)
	fixAt := func(before, after float64) func(time.Time) float64 {
		return func(t time.Time) float64 {
			if t.After(resolved) {
				return after
			}
			return before
		}
	}
	store := fakeHistory{
		1: history(1, start, now, fixAt(3200, 40)),
		2: history(2, start, now, fixAt(100, 105)),
		// 64-bit ids survive the metadata round trip.
		-8870412367183748000: history(-8870412367183748000, start, now, fixAt(50, 50)),
	}
	v := New(store, 24*time.Hour)

	res, err := v.Verify(context.Background(), resolvedSuggestion("index_recommendation", `{"query_ids":[1]}`, resolved), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictImproved || res.Queries[0].Before.MeanMs != 3200 || res.Queries[0].After.MeanMs != 40 {
		t.Fatalf("result = %+v", res.Queries[0])
	}
	if res.Summary != "3.2s → 40ms (99% faster)" {
		t.Errorf("summary = %q", res.Summary)
	}

	res, err = v.Verify(context.Background(), resolvedSuggestion("index_recommendation", `{"query_ids":[1,2,-8870412367183748000]}`, resolved), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictImproved || res.Summary != "3 queries: 1 faster, 2 unchanged" {
		t.Errorf("multi = %s / %q", res.Verdict, res.Summary)
	}
	if res.Queries[2].QueryID != -8870412367183748000 || res.Queries[2].Before == nil {
		t.Errorf("64-bit query id lost: %+v", res.Queries[2])
	}
}

func TestVerifySlowQueryUsesEarlierBaseline(t *testing.T) {
	// The fix landed 10h before the slow query resolved: the trailing 24h mean
	// only dropped below the threshold later. The 24h right before resolution
	// is mostly fixed already and must not be the baseline.
	resolved := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	fix := resolved.Add(-10 * time.Hour)
	now := resolved.Add(30 * time.Hour)
	store := fakeHistory{7: history(7, resolved.Add(-4*24*time.Hour), now, func(t time.Time) float64 {
		if t.After(fix) {
			return 20
		}
		return 2000
	})}
	res, err := New(store, 24*time.Hour).Verify(context.Background(), resolvedSuggestion("slow_query", `{"queryid":7}`, resolved), now)
	if err != nil {
		t.Fatal(err)
	}
	if q := res.Queries[0]; q.Verdict != VerdictImproved || q.Before.MeanMs != 2000 || q.After.MeanMs != 20 {
		t.Errorf("outcome = %+v before %+v", q, q.Before)
	}
}

func TestVerifyVerdicts(t *testing.T) {
	resolved := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	start := resolved.Add(-2 * 24 * time.Hour)
	cases := []struct {
		name   string
		now    time.Time
		meanAt func(time.Time) float64
		want   string
	}{
		{"regressed", resolved.Add(24 * time.Hour), func(t time.Time) float64 {
			if t.After(resolved) {
				return 300
			}
			return 100
		}, VerdictRegressed},
		{"pending right after", resolved.Add(20 * time.Minute), func(time.Time) float64 { return 100 }, VerdictPending},
		{"stopped", resolved.Add(25 * time.Hour), func(t time.Time) float64 {
			if t.After(resolved) {
				return -1
			}
			return 100
		}, VerdictStopped},
		{"not stopped before a full window", resolved.Add(5 * time.Hour), func(t time.Time) float64 {
			if t.After(resolved) {
				return -1
			}
			return 100
		}, VerdictPending},
		{"no earlier history", resolved.Add(24 * time.Hour), func(t time.Time) float64 {
			if t.After(resolved) {
				return 100
			}
			return -1
		}, VerdictNoData},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := fakeHistory{1: history(1, start, tc.now, tc.meanAt)}
			res, err := New(store, 24*time.Hour).Verify(context.Background(), resolvedSuggestion("index_recommendation", `{"query_ids":[1]}`, resolved), tc.now)
			if err != nil {
				t.Fatal(err)
			}
			if res.Verdict != tc.want {
				t.Errorf("verdict = %s (%q), want %s", res.Verdict, res.Summary, tc.want)
			}
		})
	}
}

func TestPeriodHandlesReset(t *testing.T) {
	at := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	mk := func(min int, calls int64, total float64) models.QueryHistorySample {
		s := models.QueryHistorySample{SampledAt: at.Add(time.Duration(min) * time.Minute)}
		s.Calls, s.TotalExecTime = calls, total
		return s
	}
	// 100 calls at 10ms, then pg_stat_statements is reset and 5 calls at 2ms.
	samples := []models.QueryHistorySample{mk(0, 1000, 10000), mk(5, 1100, 11000), mk(10, 5, 10)}
	p := period(samples, at.Add(time.Minute), at.Add(time.Hour))
	if p == nil || p.Calls != 105 || fmt.Sprintf("%.3f", p.MeanMs) != fmt.Sprintf("%.3f", 1010.0/105) {
		t.Errorf("period = %+v", p)
	}
}

func TestVerifySkipsUnverifiable(t *testing.T) {
	at := time.Now()
	v := New(fakeHistory{}, 24*time.Hour)
	for _, sug := range []models.Suggestion{
		resolvedSuggestion("unused_index", `{"index":"x"}`, at),
		{RuleID: "slow_query", Status: models.StatusActive, Metadata: `{"queryid":1}`},
		resolvedSuggestion("slow_query", `not json`, at),
	} {
		if res, err := v.Verify(context.Background(), sug, at); res != nil || err != nil {
			t.Errorf("%s/%s: got %+v, %v", sug.RuleID, sug.Status, res, err)
		}
	}
	if s := FormatMs(0.42); !strings.HasSuffix(s, "ms") {
		t.Errorf("FormatMs = %q", s)
	}
}

func TestVerifyEdgeCases(t *testing.T) {
	resolved := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	t.Run("Before.MeanMs zero returns VerdictNoData and finite JSON", func(t *testing.T) {
		store := fakeHistory{
			1: history(1, resolved.Add(-2*24*time.Hour), resolved.Add(24*time.Hour), func(t time.Time) float64 {
				if t.After(resolved) {
					return 50
				}
				return 0
			}),
		}
		res, err := New(store, 24*time.Hour).Verify(context.Background(), resolvedSuggestion("index_recommendation", `{"query_ids":[1]}`, resolved), resolved.Add(24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict != VerdictNoData || res.Queries[0].Ratio != 0 {
			t.Fatalf("res = %+v, want VerdictNoData with finite zero ratio", res)
		}
		if _, err := json.Marshal(res); err != nil {
			t.Fatalf("json.Marshal failed (NaN/Inf?): %v", err)
		}
	})

	t.Run("stale baseline older than baselineLeadIn is ignored", func(t *testing.T) {
		mk := func(at time.Time, calls int64, total float64) models.QueryHistorySample {
			s := models.QueryHistorySample{SampledAt: at}
			s.QueryID, s.Calls, s.TotalExecTime = 1, calls, total
			return s
		}
		// Sample 5h before 'from' must NOT become prev for 'After'.
		samples := []models.QueryHistorySample{
			mk(resolved.Add(-5*time.Hour), 100, 100000),
			mk(resolved.Add(10*time.Minute), 120, 100400),
		}
		p := period(samples, resolved, resolved.Add(time.Hour))
		if p == nil || p.Calls != 0 || !p.From.Equal(resolved.Add(10*time.Minute)) {
			t.Fatalf("stale pre-window sample bled into period: %+v", p)
		}
	})

	t.Run("completed window with fewer than MinCalls does not stay pending forever", func(t *testing.T) {
		mk := func(at time.Time, calls int64, total float64) models.QueryHistorySample {
			s := models.QueryHistorySample{SampledAt: at}
			s.QueryID, s.Calls, s.TotalExecTime = 1, calls, total
			return s
		}
		samples := history(1, resolved.Add(-25*time.Hour), resolved, func(time.Time) float64 { return 500 })
		last := samples[len(samples)-1]
		// Only 3 calls over the entire 24h post-resolution window.
		samples = append(samples,
			mk(resolved.Add(2*time.Hour), last.Calls+1, last.TotalExecTime+50),
			mk(resolved.Add(12*time.Hour), last.Calls+3, last.TotalExecTime+150),
		)
		res, err := New(fakeHistory{1: samples}, 24*time.Hour).Verify(
			context.Background(),
			resolvedSuggestion("index_recommendation", `{"query_ids":[1]}`, resolved),
			resolved.Add(25*time.Hour),
		)
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict != VerdictNoData || !strings.Contains(res.Summary, "too few calls") {
			t.Fatalf("verdict = %s (%q), want no_data / too few calls", res.Verdict, res.Summary)
		}
	})

	t.Run("large window over 10k samples preserves before period", func(t *testing.T) {
		// 7-day window at 1-minute sampling -> >10,000 samples per side (>20,000 total).
		win := 8 * 24 * time.Hour
		var samples []models.QueryHistorySample
		var calls int64
		var total float64
		start := resolved.Add(-win - 10*time.Minute)
		end := resolved.Add(win)
		for at := start; !at.After(end); at = at.Add(time.Minute) {
			calls += 1
			if at.After(resolved) {
				total += 10
			} else {
				total += 1000
			}
			s := models.QueryHistorySample{SampledAt: at}
			s.QueryID, s.Calls, s.TotalExecTime = 1, calls, total
			samples = append(samples, s)
		}
		res, err := New(fakeHistory{1: samples}, win).Verify(
			context.Background(),
			resolvedSuggestion("index_recommendation", `{"query_ids":[1]}`, resolved),
			end,
		)
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict != VerdictImproved || res.Queries[0].Before == nil || res.Queries[0].Before.Calls == 0 {
			t.Fatalf("large window lost before samples: %+v", res.Queries[0])
		}
	})
}
