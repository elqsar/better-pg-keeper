package notifier

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/verify"
)

type fakeVerifier map[int64]*verify.Result

func (f fakeVerifier) Verify(ctx context.Context, sug models.Suggestion, now time.Time) (*verify.Result, error) {
	return f[sug.ID], nil
}

func TestDigestResolvedUsesResolvedAtAndOutcome(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	since := now.Add(-7 * 24 * time.Hour)
	inPeriod := since.Add(time.Hour)
	store := newFakeStorage()
	store.suggestions = []models.Suggestion{
		// Last seen before the period, but resolved inside it.
		{ID: 1, RuleID: "index_recommendation", TargetObject: "a", Title: "Index orders", Severity: models.SeverityWarning,
			Status: models.StatusResolved, LastSeenAt: since.Add(-time.Minute), ResolvedAt: &inPeriod},
		{ID: 2, RuleID: "slow_query", TargetObject: "b", Title: "Slow report", Severity: models.SeverityWarning,
			Status: models.StatusResolved, LastSeenAt: inPeriod, ResolvedAt: &inPeriod},
		// Resolved before the period.
		{ID: 3, RuleID: "slow_query", TargetObject: "c", Title: "Old", Severity: models.SeverityWarning,
			Status: models.StatusResolved, LastSeenAt: since.Add(-48 * time.Hour), ResolvedAt: ptr(since.Add(-47 * time.Hour))},
	}
	n, err := New(Options{
		Config: config.Default().Notifications, Storage: store, Channels: []Channel{&recorder{}},
		InstanceID: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Verifier: fakeVerifier{
			1: {Verdict: verify.VerdictImproved, Summary: "3.2s → 40ms (99% faster)"},
			2: {Verdict: verify.VerdictPending, Summary: "waiting for more calls after the fix"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := n.buildDigest(context.Background(), since, now)
	if err != nil {
		t.Fatal(err)
	}
	d := msg.Digest
	if d.ResolvedTotal != 2 || len(d.Resolved) != 2 {
		t.Fatalf("resolved = %d %+v, want issues 1 and 2", d.ResolvedTotal, d.Resolved)
	}
	outcomes := map[string]string{}
	for _, it := range d.Resolved {
		outcomes[it.Title] = it.Outcome
	}
	if outcomes["Index orders"] != "3.2s → 40ms (99% faster)" || outcomes["Slow report"] != "" {
		t.Errorf("outcomes = %v; pending verdicts must be left out", outcomes)
	}
	if text := render(msg, slackMrkdwn); !strings.Contains(text, "Index orders — 3.2s → 40ms (99% faster)") {
		t.Errorf("rendered digest missing outcome:\n%s", text)
	}
}

func ptr(t time.Time) *time.Time { return &t }
