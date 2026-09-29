// Package setup builds the setup checklist: whether PGAnalyzer can see what it
// needs from PostgreSQL, and whether enough history exists yet for each kind
// of advice. A new install otherwise looks clean when it is only blind.
package setup

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
)

// DefaultTTL is how long a report is reused, so page views don't query
// PostgreSQL. Setup changes rarely.
const DefaultTTL = 10 * time.Minute

// SizeTrendSpan is how much size history the disk-growth forecast needs.
const SizeTrendSpan = analyzer.MinSizeTrendSpan

// PGChecker runs the PostgreSQL side of the checklist.
type PGChecker interface {
	CheckSetup(ctx context.Context) (*models.SetupReport, error)
}

// Storage is the collected history readiness is judged from.
type Storage interface {
	GetEarliestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notBefore time.Time) (*models.Snapshot, error)
	GetSizeHistory(ctx context.Context, instanceID int64, since time.Time) ([]models.SizeSample, error)
}

// Options configures a Checker.
type Options struct {
	PG PGChecker
	// Storage is optional; without it, history readiness is left out.
	Storage    Storage
	InstanceID int64
	// SlowQueryWindow and UnusedIndexDays are the thresholds whose history
	// readiness is reported.
	SlowQueryWindow time.Duration
	UnusedIndexDays int
	// IndexAdvisor reports whether the index advisor is enabled; the hypopg
	// check is left out when it is not.
	IndexAdvisor bool
	TTL          time.Duration
	Now          func() time.Time
}

// Checker builds setup reports and caches the latest one.
type Checker struct {
	opts Options

	mu     sync.Mutex
	cached *models.SetupReport
}

// New creates a Checker.
func New(opts Options) *Checker {
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Checker{opts: opts}
}

// Report returns the cached report, running the checks when it is older than
// the TTL.
func (c *Checker) Report(ctx context.Context) *models.SetupReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil && c.opts.Now().Sub(c.cached.CheckedAt) < c.opts.TTL {
		return c.cached
	}
	c.cached = c.Run(ctx)
	return c.cached
}

// Refresh runs the checks now and caches the result.
func (c *Checker) Refresh(ctx context.Context) *models.SetupReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cached = c.Run(ctx)
	return c.cached
}

// Run always runs the checks.
func (c *Checker) Run(ctx context.Context) *models.SetupReport {
	now := c.opts.Now()
	report, err := c.opts.PG.CheckSetup(ctx)
	if err != nil || report == nil {
		detail := "no result"
		if err != nil {
			detail = err.Error()
		}
		report = &models.SetupReport{Checks: []models.SetupCheck{{
			Name: "connection", Title: "PostgreSQL connection", Status: models.SetupFail,
			Detail: detail, Fix: "Check postgres.host, port, user and password in the config, and that the server accepts connections from here.",
		}}}
	}
	report.CheckedAt = now

	if !c.opts.IndexAdvisor {
		kept := report.Checks[:0]
		for _, check := range report.Checks {
			if check.Name != "hypopg" {
				kept = append(kept, check)
			}
		}
		report.Checks = kept
	}
	if c.opts.Storage != nil {
		report.Checks = append(report.Checks, c.readiness(ctx, report, now)...)
	}
	return report
}

// readiness reports how close each history-based kind of advice is to having
// enough evidence.
func (c *Checker) readiness(ctx context.Context, report *models.SetupReport, now time.Time) []models.SetupCheck {
	var out []models.SetupCheck

	window := c.opts.SlowQueryWindow
	if window <= 0 {
		window = 24 * time.Hour
	}
	slow := models.SetupCheck{Name: "query_history", Title: "Slow-query history"}
	snap, err := c.opts.Storage.GetEarliestSnapshotWithCollector(ctx, c.opts.InstanceID, analyzer.DomainQueryStats, time.Time{})
	switch {
	case err != nil:
		slow.Status, slow.Detail = models.SetupInfo, "Could not read collected history: "+err.Error()
	case snap == nil:
		slow.Status, slow.Detail = models.SetupPending, "No query statistics collected yet."
	case now.Sub(snap.CapturedAt) < window:
		slow.Status = models.SetupPending
		slow.Detail = fmt.Sprintf("Slow queries are judged on lifetime averages until %s of history exists; recent regressions show %s.",
			formatSpan(window), readyIn(window-now.Sub(snap.CapturedAt)))
	default:
		slow.Status, slow.Detail = models.SetupOK, fmt.Sprintf("Slow queries are judged on the last %s.", formatSpan(window))
	}
	out = append(out, slow)

	if c.opts.UnusedIndexDays > 0 && report.StatsSince != nil {
		need := time.Duration(c.opts.UnusedIndexDays) * 24 * time.Hour
		idx := models.SetupCheck{Name: "index_evidence", Title: "Unused-index evidence"}
		counted := now.Sub(*report.StatsSince)
		if counted < need {
			idx.Status = models.SetupPending
			idx.Detail = fmt.Sprintf("PostgreSQL has counted index scans since %s (statistics reset or server start). Unused indexes are reported after %d days, %s.",
				report.StatsSince.Format("2006-01-02 15:04"), c.opts.UnusedIndexDays, readyIn(need-counted))
		} else {
			idx.Status = models.SetupOK
			idx.Detail = fmt.Sprintf("Index scans counted since %s.", report.StatsSince.Format("2006-01-02"))
		}
		out = append(out, idx)
	}

	size := models.SetupCheck{Name: "size_history", Title: "Disk growth history"}
	samples, err := c.opts.Storage.GetSizeHistory(ctx, c.opts.InstanceID, now.Add(-7*24*time.Hour))
	switch {
	case err != nil:
		size.Status, size.Detail = models.SetupInfo, "Could not read size history: "+err.Error()
	case len(samples) < 2 || samples[len(samples)-1].CapturedAt.Sub(samples[0].CapturedAt) < SizeTrendSpan:
		var have time.Duration
		if len(samples) >= 2 {
			have = samples[len(samples)-1].CapturedAt.Sub(samples[0].CapturedAt)
		}
		size.Status = models.SetupPending
		size.Detail = fmt.Sprintf("The disk-growth forecast needs %s of size history, %s.", formatSpan(SizeTrendSpan), readyIn(SizeTrendSpan-have))
	default:
		size.Status, size.Detail = models.SetupOK, "Enough size history for a growth trend."
	}
	out = append(out, size)
	return out
}

// readyIn renders how long until something is ready, e.g. "ready in ~5 hours".
func readyIn(d time.Duration) string {
	return "ready in ~" + formatSpan(d)
}

// formatSpan renders a duration in whole hours or days, rounding up.
func formatSpan(d time.Duration) string {
	if d <= time.Hour {
		return "1 hour"
	}
	hours := int(math.Ceil(d.Hours()))
	if hours < 48 {
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%d days", int(math.Ceil(d.Hours()/24)))
}

// WriteText prints the report as a checklist, with each fix indented under
// its check.
func WriteText(w io.Writer, r *models.SetupReport) {
	width := 0
	for _, c := range r.Checks {
		width = max(width, len(c.Title))
	}
	for _, c := range r.Checks {
		fmt.Fprintf(w, "[%-7s] %-*s  %s\n", c.Status, width, c.Title, c.Detail)
		if c.Fix != "" && c.Status != models.SetupOK {
			for _, line := range strings.Split(c.Fix, "\n") {
				fmt.Fprintf(w, "            %*s  %s\n", width, "", line)
			}
		}
	}
	fmt.Fprintf(w, "\n%d failed, %d warnings, %d waiting for history\n",
		r.Count(models.SetupFail), r.Count(models.SetupWarn), r.Count(models.SetupPending))
}
