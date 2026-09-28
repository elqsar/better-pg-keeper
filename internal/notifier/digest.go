package notifier

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/verify"
)

// topQueryCount is how many queries the digest lists.
const topQueryCount = 5

// maybeSendDigest sends the digest when a scheduled slot has passed since the
// last one. A fresh install waits for the next slot rather than sending at
// whatever time it happens to start; after downtime the missed digest is sent once.
func (n *Notifier) maybeSendDigest(ctx context.Context) error {
	d := n.cfg.Digest
	if !d.Enabled {
		return nil
	}
	now := n.now()
	slot, err := lastDigestSlot(d, now)
	if err != nil {
		return err
	}

	last, ok, err := n.storage.GetNotifierTime(ctx, n.instanceID, digestSlotKey)
	if err != nil {
		return err
	}
	if !ok {
		return n.storage.SetNotifierTime(ctx, n.instanceID, digestSlotKey, slot)
	}
	if !slot.After(last) {
		return nil
	}

	msg, err := n.buildDigest(ctx, previousSlot(d, slot), now)
	if err != nil {
		return err
	}
	if err := n.deliver(ctx, msg); err != nil {
		return err
	}
	return n.storage.SetNotifierTime(ctx, n.instanceID, digestSlotKey, slot)
}

// lastDigestSlot returns the most recent scheduled digest time at or before now,
// in now's location.
func lastDigestSlot(d config.DigestConfig, now time.Time) (time.Time, error) {
	slot := time.Date(now.Year(), now.Month(), now.Day(), d.Hour, 0, 0, 0, now.Location())
	if slot.After(now) {
		slot = slot.AddDate(0, 0, -1)
	}
	if d.Schedule != "weekly" {
		return slot, nil
	}
	weekday, err := config.ParseWeekday(d.Weekday)
	if err != nil {
		return time.Time{}, err
	}
	for slot.Weekday() != weekday {
		slot = slot.AddDate(0, 0, -1)
	}
	return slot, nil
}

func previousSlot(d config.DigestConfig, slot time.Time) time.Time {
	if d.Schedule == "weekly" {
		return slot.AddDate(0, 0, -7)
	}
	return slot.AddDate(0, 0, -1)
}

// buildDigest summarises issues and query load between since and now.
func (n *Notifier) buildDigest(ctx context.Context, since, now time.Time) (Message, error) {
	active, err := n.storage.GetSuggestionsByStatus(ctx, n.instanceID, models.StatusActive)
	if err != nil {
		return Message{}, err
	}
	resolved, err := n.storage.GetSuggestionsByStatus(ctx, n.instanceID, models.StatusResolved)
	if err != nil {
		return Message{}, err
	}

	d := &Digest{PeriodStart: since, PeriodEnd: now}
	for _, s := range active {
		switch s.Severity {
		case models.SeverityCritical:
			d.Critical++
		case models.SeverityWarning:
			d.Warning++
		default:
			d.Info++
		}
		if !s.FirstSeenAt.Before(since) {
			d.NewTotal++
			if len(d.New) < maxListed {
				d.New = append(d.New, n.item(EventOpened, s, ""))
			}
		}
	}
	// Suggestions are listed most severe first, so the capped lists keep the
	// most important ones.
	for _, s := range resolved {
		if !resolvedAt(s).Before(since) {
			d.ResolvedTotal++
			if len(d.Resolved) < maxListed {
				it := n.item(EventResolved, s, "")
				it.Outcome = n.outcome(ctx, s, now)
				d.Resolved = append(d.Resolved, it)
			}
		}
	}

	d.TopQueries, err = n.topQueries(ctx, since, now)
	if err != nil {
		// The rest of the digest is still worth sending.
		n.logger.Warn("notifier: top queries unavailable for digest", "error", err)
	}

	if n.collection != nil {
		d.LastCollection, _ = n.collection.CollectionStatus()
	}

	period := "Daily"
	if n.cfg.Digest.Schedule == "weekly" {
		period = "Weekly"
	}
	return Message{
		Kind:   KindDigest,
		Title:  fmt.Sprintf("%s digest, %s – %s", period, since.Format("Jan 2"), now.Format("Jan 2")),
		Digest: d,
		URL:    n.link("/"),
	}, nil
}

// topQueries ranks queries by execution time spent within the period, using the
// earliest and latest query snapshots inside it.
func (n *Notifier) topQueries(ctx context.Context, since, now time.Time) ([]TopQuery, error) {
	first, err := n.storage.GetEarliestSnapshotWithCollector(ctx, n.instanceID, analyzer.DomainQueryStats, since)
	if err != nil || first == nil {
		return nil, err
	}
	last, err := n.storage.GetLatestSnapshotWithCollector(ctx, n.instanceID, analyzer.DomainQueryStats, now)
	if err != nil || last == nil || last.ID == first.ID {
		return nil, err
	}
	deltas, err := n.storage.GetQueryStatsDelta(ctx, first.ID, last.ID)
	if err != nil {
		return nil, err
	}

	var total float64
	for _, q := range deltas {
		total += q.DeltaTotalTime
	}
	if total <= 0 {
		return nil, nil
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].DeltaTotalTime > deltas[j].DeltaTotalTime })

	var top []TopQuery
	for _, q := range deltas {
		if len(top) == topQueryCount || q.DeltaTotalTime <= 0 {
			break
		}
		top = append(top, TopQuery{
			QueryID: q.QueryID, Query: shortQuery(q.Query, 80),
			TotalTimeMs: q.DeltaTotalTime, Share: q.DeltaTotalTime / total,
			Calls: q.DeltaCalls, MeanTimeMs: q.MeanExecTime,
			URL: n.link(fmt.Sprintf("/queries/%d", q.QueryID)),
		})
	}
	return top, nil
}

// resolvedAt is when a suggestion resolved. Rows resolved before resolution
// times were stored fall back to the last analysis that still saw the issue.
func resolvedAt(s models.Suggestion) time.Time {
	if s.ResolvedAt != nil {
		return *s.ResolvedAt
	}
	return s.LastSeenAt
}

// outcome summarises fix verification for a resolved issue, or "" when there
// is no verdict yet. Verification is best effort: the digest goes out without it.
func (n *Notifier) outcome(ctx context.Context, s models.Suggestion, now time.Time) string {
	if n.verifier == nil {
		return ""
	}
	res, err := n.verifier.Verify(ctx, s, now)
	if err != nil {
		n.logger.Warn("notifier: fix verification failed", "suggestion", s.ID, "error", err)
		return ""
	}
	if res == nil {
		return ""
	}
	switch res.Verdict {
	case verify.VerdictImproved, verify.VerdictRegressed, verify.VerdictUnchanged, verify.VerdictStopped:
		return res.Summary
	}
	return ""
}
