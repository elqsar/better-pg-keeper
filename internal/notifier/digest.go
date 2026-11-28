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
	saveCtx, cancel := persistCtx()
	defer cancel()
	return n.storage.SetNotifierTime(saveCtx, n.instanceID, digestSlotKey, slot)
}

// slotOnDay builds the scheduled digest time for calendar day y-m-d at hour
// in day's location. On a spring-forward DST day when wall-clock hour does not
// exist (e.g. 02:00 -> 03:00), Go's time.Date normalizes backward to hour-1;
// advancing past the gap lands on the first valid instant after the skipped
// hour instead of firing an hour early or shifting adjacent days.
func slotOnDay(day time.Time, hour int) time.Time {
	t := time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, day.Location())
	if t.Hour() < hour {
		t = t.Add(time.Duration(hour-t.Hour()) * time.Hour)
	}
	return t
}

// lastDigestSlot returns the most recent scheduled digest time at or before now,
// in now's location.
func lastDigestSlot(d config.DigestConfig, now time.Time) (time.Time, error) {
	// Anchor calendar stepping at local noon so AddDate(0, 0, -1) never lands
	// on a midnight DST boundary.
	day := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())
	slot := slotOnDay(day, d.Hour)
	if slot.After(now) {
		day = day.AddDate(0, 0, -1)
		slot = slotOnDay(day, d.Hour)
	}
	if d.Schedule != "weekly" {
		return slot, nil
	}
	weekday, err := config.ParseWeekday(d.Weekday)
	if err != nil {
		return time.Time{}, err
	}
	for day.Weekday() != weekday {
		day = day.AddDate(0, 0, -1)
	}
	return slotOnDay(day, d.Hour), nil
}

func previousSlot(d config.DigestConfig, slot time.Time) time.Time {
	day := time.Date(slot.Year(), slot.Month(), slot.Day(), 12, 0, 0, 0, slot.Location())
	if d.Schedule == "weekly" {
		day = day.AddDate(0, 0, -7)
	} else {
		day = day.AddDate(0, 0, -1)
	}
	return slotOnDay(day, d.Hour)
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
