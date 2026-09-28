package notifier

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
)

// fakeStorage keeps suggestions and notifier state in memory.
type fakeStorage struct {
	suggestions []models.Suggestion
	states      map[string]models.NotificationState
	times       map[string]time.Time
	snapshots   []models.Snapshot // query_stats snapshots
	deltas      []models.QueryStatDelta
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{states: map[string]models.NotificationState{}, times: map[string]time.Time{}}
}

func (f *fakeStorage) GetSuggestionsByStatus(_ context.Context, _ int64, status string) ([]models.Suggestion, error) {
	var out []models.Suggestion
	for _, s := range f.suggestions {
		if s.Status == status {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStorage) ListNotificationStates(context.Context, int64) ([]models.NotificationState, error) {
	var out []models.NotificationState
	for _, st := range f.states {
		out = append(out, st)
	}
	return out, nil
}

func (f *fakeStorage) UpsertNotificationState(_ context.Context, st *models.NotificationState) error {
	f.states[stateKey(st.RuleID, st.TargetObject)] = *st
	return nil
}

func (f *fakeStorage) DeleteNotificationState(_ context.Context, _ int64, ruleID, target string) error {
	delete(f.states, stateKey(ruleID, target))
	return nil
}

func (f *fakeStorage) GetNotifierTime(_ context.Context, _ int64, key string) (time.Time, bool, error) {
	t, ok := f.times[key]
	return t, ok, nil
}

func (f *fakeStorage) SetNotifierTime(_ context.Context, _ int64, key string, t time.Time) error {
	f.times[key] = t
	return nil
}

func (f *fakeStorage) GetEarliestSnapshotWithCollector(_ context.Context, _ int64, _ string, notBefore time.Time) (*models.Snapshot, error) {
	for _, s := range f.snapshots { // ordered oldest first
		if !s.CapturedAt.Before(notBefore) {
			return &s, nil
		}
	}
	return nil, nil
}

func (f *fakeStorage) GetLatestSnapshotWithCollector(_ context.Context, _ int64, _ string, notAfter time.Time) (*models.Snapshot, error) {
	for i := len(f.snapshots) - 1; i >= 0; i-- {
		if !f.snapshots[i].CapturedAt.After(notAfter) {
			return &f.snapshots[i], nil
		}
	}
	return nil, nil
}

func (f *fakeStorage) GetQueryStatsDelta(context.Context, int64, int64) ([]models.QueryStatDelta, error) {
	return f.deltas, nil
}

func (f *fakeStorage) set(id int64, severity, status string) {
	for i := range f.suggestions {
		if f.suggestions[i].ID == id {
			f.suggestions[i].Severity = severity
			f.suggestions[i].Status = status
			return
		}
	}
	f.suggestions = append(f.suggestions, models.Suggestion{
		ID: id, RuleID: "slow_query", TargetObject: "queryid:" + string(rune('0'+id)),
		Title: "Slow query " + string(rune('0'+id)), Severity: severity, Status: status,
	})
}

// recorder is a channel that records messages and can be made to fail.
type recorder struct {
	sent []Message
	fail bool
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) Send(_ context.Context, m Message) error {
	if r.fail {
		return errors.New("boom")
	}
	r.sent = append(r.sent, m)
	return nil
}

func (r *recorder) take() []Message {
	out := r.sent
	r.sent = nil
	return out
}

type fakeCollection struct {
	last time.Time
	err  string
}

func (f *fakeCollection) CollectionStatus() (time.Time, string) { return f.last, f.err }

type harness struct {
	n       *Notifier
	store   *fakeStorage
	channel *recorder
	coll    *fakeCollection
	now     time.Time
}

func newHarness(t *testing.T, mutate func(*config.NotificationsConfig)) *harness {
	t.Helper()
	cfg := config.Default().Notifications
	cfg.Enabled = true
	cfg.Digest.Enabled = false
	cfg.CollectionStaleAfter = 0
	cfg.DashboardURL = "https://pga.example.com/"
	if mutate != nil {
		mutate(&cfg)
	}
	h := &harness{
		store:   newFakeStorage(),
		channel: &recorder{},
		coll:    &fakeCollection{},
		now:     time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC), // a Monday
	}
	n, err := New(Options{
		Config: cfg, Storage: h.store, Collection: h.coll, Channels: []Channel{h.channel},
		InstanceID: 1, Instance: "db:5432/app",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return h.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.n = n
	return h
}

func (h *harness) tick(t *testing.T, advance time.Duration) []Message {
	t.Helper()
	h.now = h.now.Add(advance)
	h.n.Tick(context.Background())
	return h.channel.take()
}

func onlyItems(t *testing.T, msgs []Message) []Item {
	t.Helper()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(msgs), msgs)
	}
	return msgs[0].Items
}

func TestAlertsOnceForNewCriticalIssue(t *testing.T) {
	h := newHarness(t, nil)
	h.store.set(1, models.SeverityCritical, models.StatusActive)
	h.store.set(2, models.SeverityWarning, models.StatusActive) // below min_severity

	items := onlyItems(t, h.tick(t, 0))
	if len(items) != 1 || items[0].Event != EventOpened || items[0].Title != "Slow query 1" {
		t.Fatalf("items = %+v, want one opened alert for suggestion 1", items)
	}
	if items[0].URL != "https://pga.example.com/suggestions/1" {
		t.Errorf("URL = %q", items[0].URL)
	}

	if msgs := h.tick(t, time.Minute); len(msgs) != 0 {
		t.Errorf("repeated alert on next tick: %+v", msgs)
	}
}

func TestEscalationAndReminder(t *testing.T) {
	h := newHarness(t, func(c *config.NotificationsConfig) { c.MinSeverity = models.SeverityWarning })
	h.store.set(1, models.SeverityWarning, models.StatusActive)
	h.tick(t, 0)

	h.store.set(1, models.SeverityCritical, models.StatusActive)
	items := onlyItems(t, h.tick(t, time.Minute))
	if items[0].Event != EventEscalated || items[0].PreviousSeverity != models.SeverityWarning {
		t.Fatalf("items = %+v, want escalation from warning", items)
	}

	if msgs := h.tick(t, 23*time.Hour); len(msgs) != 0 {
		t.Fatalf("reminder before renotify_after: %+v", msgs)
	}
	items = onlyItems(t, h.tick(t, time.Hour))
	if items[0].Event != EventReminder {
		t.Fatalf("items = %+v, want a reminder after 24h", items)
	}
}

func TestFlappingIssueStaysQuiet(t *testing.T) {
	h := newHarness(t, nil)
	h.store.set(1, models.SeverityCritical, models.StatusActive)
	h.tick(t, 0)

	// Gone for less than resolve_grace, then back: nothing to say either way.
	h.store.set(1, models.SeverityCritical, models.StatusResolved)
	if msgs := h.tick(t, time.Minute); len(msgs) != 0 {
		t.Fatalf("alerted on disappearance: %+v", msgs)
	}
	if msgs := h.tick(t, 30*time.Minute); len(msgs) != 0 {
		t.Fatalf("alerted within grace: %+v", msgs)
	}
	h.store.set(1, models.SeverityCritical, models.StatusActive)
	if msgs := h.tick(t, time.Minute); len(msgs) != 0 {
		t.Fatalf("alerted on return within grace: %+v", msgs)
	}
	if st := h.store.states[stateKey("slow_query", "queryid:1")]; st.ClearedAt != nil {
		t.Errorf("state still cleared after the issue returned: %+v", st)
	}
}

func TestResolvedAfterGrace(t *testing.T) {
	h := newHarness(t, nil)
	h.store.set(1, models.SeverityCritical, models.StatusActive)
	h.tick(t, 0)

	h.store.set(1, models.SeverityCritical, models.StatusResolved)
	h.tick(t, time.Minute)
	items := onlyItems(t, h.tick(t, time.Hour))
	if items[0].Event != EventResolved || items[0].Title != "Slow query 1" {
		t.Fatalf("items = %+v, want resolved", items)
	}
	if len(h.store.states) != 0 {
		t.Errorf("state not forgotten after resolution: %+v", h.store.states)
	}
	if msgs := h.tick(t, time.Hour); len(msgs) != 0 {
		t.Errorf("resolved announced twice: %+v", msgs)
	}
}

func TestDowngradeBelowMinimumIsAnnounced(t *testing.T) {
	h := newHarness(t, nil)
	h.store.set(1, models.SeverityCritical, models.StatusActive)
	h.tick(t, 0)

	h.store.set(1, models.SeverityWarning, models.StatusActive)
	h.tick(t, time.Minute)
	items := onlyItems(t, h.tick(t, time.Hour))
	if items[0].Event != EventDowngraded || items[0].Severity != models.SeverityWarning {
		t.Fatalf("items = %+v, want downgrade to warning", items)
	}
}

func TestDismissedIssueIsDroppedSilently(t *testing.T) {
	h := newHarness(t, nil)
	h.store.set(1, models.SeverityCritical, models.StatusActive)
	h.tick(t, 0)

	h.store.set(1, models.SeverityCritical, models.StatusDismissed)
	h.tick(t, time.Minute)
	if msgs := h.tick(t, 2*time.Hour); len(msgs) != 0 {
		t.Fatalf("dismissed issue announced: %+v", msgs)
	}
	if len(h.store.states) != 0 {
		t.Errorf("state kept for dismissed issue: %+v", h.store.states)
	}
}

func TestFailedDeliveryIsRetried(t *testing.T) {
	h := newHarness(t, nil)
	h.store.set(1, models.SeverityCritical, models.StatusActive)

	h.channel.fail = true
	h.tick(t, 0)
	if len(h.store.states) != 0 {
		t.Fatalf("state recorded although nothing was delivered: %+v", h.store.states)
	}

	h.channel.fail = false
	items := onlyItems(t, h.tick(t, time.Minute))
	if items[0].Event != EventOpened {
		t.Fatalf("items = %+v, want the opened alert retried", items)
	}
}

func TestCollectionAlertAndRecovery(t *testing.T) {
	h := newHarness(t, func(c *config.NotificationsConfig) {
		c.CollectionStaleAfter = config.Duration(15 * time.Minute)
	})
	start := h.now
	h.coll.last = start
	if msgs := h.tick(t, 14*time.Minute); len(msgs) != 0 {
		t.Fatalf("alerted before threshold: %+v", msgs)
	}

	h.coll.err = "connection refused"
	msgs := h.tick(t, 2*time.Minute)
	if len(msgs) != 1 || msgs[0].Kind != KindHealth || !strings.Contains(msgs[0].Detail, "connection refused") {
		t.Fatalf("msgs = %+v, want one health alert with the error", msgs)
	}
	if msgs := h.tick(t, time.Hour); len(msgs) != 0 {
		t.Fatalf("health alert repeated before renotify_after: %+v", msgs)
	}

	h.coll.last, h.coll.err = h.now.Add(time.Minute), ""
	msgs = h.tick(t, time.Minute)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Title, "recovered") {
		t.Fatalf("msgs = %+v, want recovery", msgs)
	}
}

func TestCollectionNeverSucceededCountsFromStartup(t *testing.T) {
	h := newHarness(t, func(c *config.NotificationsConfig) {
		c.CollectionStaleAfter = config.Duration(15 * time.Minute)
	})
	// Zero last success must not read as "down since year 1" right at startup.
	if msgs := h.tick(t, time.Minute); len(msgs) != 0 {
		t.Fatalf("alerted at startup: %+v", msgs)
	}
	if msgs := h.tick(t, 15*time.Minute); len(msgs) != 1 {
		t.Fatalf("expected alert after threshold, got %+v", msgs)
	}
}

func TestLastDigestSlot(t *testing.T) {
	loc := time.UTC
	weekly := config.DigestConfig{Enabled: true, Schedule: "weekly", Weekday: "monday", Hour: 9}
	daily := config.DigestConfig{Enabled: true, Schedule: "daily", Hour: 9}

	tests := []struct {
		name string
		cfg  config.DigestConfig
		now  time.Time
		want time.Time
	}{
		{"weekly, Monday after the hour", weekly, time.Date(2026, 9, 28, 9, 30, 0, 0, loc), time.Date(2026, 9, 28, 9, 0, 0, 0, loc)},
		{"weekly, Monday before the hour", weekly, time.Date(2026, 9, 28, 8, 59, 0, 0, loc), time.Date(2026, 9, 21, 9, 0, 0, 0, loc)},
		{"weekly, midweek", weekly, time.Date(2026, 10, 1, 12, 0, 0, 0, loc), time.Date(2026, 9, 28, 9, 0, 0, 0, loc)},
		{"daily, after the hour", daily, time.Date(2026, 10, 1, 12, 0, 0, 0, loc), time.Date(2026, 10, 1, 9, 0, 0, 0, loc)},
		{"daily, before the hour", daily, time.Date(2026, 10, 1, 8, 0, 0, 0, loc), time.Date(2026, 9, 30, 9, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lastDigestSlot(tt.cfg, tt.now)
			if err != nil || !got.Equal(tt.want) {
				t.Errorf("lastDigestSlot = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestDigestSchedule(t *testing.T) {
	h := newHarness(t, func(c *config.NotificationsConfig) {
		c.Digest = config.DigestConfig{Enabled: true, Schedule: "weekly", Weekday: "monday", Hour: 9}
	})
	h.coll.last = h.now

	// First start records the current slot instead of sending immediately.
	for _, m := range h.tick(t, 0) {
		if m.Kind == KindDigest {
			t.Fatalf("digest sent on first start: %+v", m)
		}
	}

	// Nothing until next Monday 09:00.
	if msgs := h.tick(t, 6*24*time.Hour+22*time.Hour); len(msgs) != 0 {
		t.Fatalf("digest sent early: %+v", msgs)
	}

	since := h.now.Add(-7 * 24 * time.Hour)
	h.store.suggestions = []models.Suggestion{
		{ID: 1, RuleID: "r", TargetObject: "a", Title: "New critical", Severity: models.SeverityCritical,
			Status: models.StatusActive, FirstSeenAt: since.Add(time.Hour), LastSeenAt: h.now},
		{ID: 2, RuleID: "r", TargetObject: "b", Title: "Old warning", Severity: models.SeverityWarning,
			Status: models.StatusActive, FirstSeenAt: since.Add(-time.Hour), LastSeenAt: h.now},
		{ID: 3, RuleID: "r", TargetObject: "c", Title: "Fixed", Severity: models.SeverityWarning,
			Status: models.StatusResolved, FirstSeenAt: since.Add(-48 * time.Hour), LastSeenAt: since.Add(time.Hour)},
	}
	h.store.states[stateKey("r", "a")] = models.NotificationState{RuleID: "r", TargetObject: "a", Severity: models.SeverityCritical, NotifiedAt: h.now}
	h.store.snapshots = []models.Snapshot{
		{ID: 10, CapturedAt: since.Add(-time.Hour)},
		{ID: 11, CapturedAt: since.Add(time.Hour)},
		{ID: 12, CapturedAt: h.now.Add(2 * time.Hour)},
	}
	h.store.deltas = []models.QueryStatDelta{
		{QueryID: 7, Query: "SELECT  *\n FROM orders", DeltaCalls: 100, DeltaTotalTime: 750, MeanExecTime: 7.5},
		{QueryID: 8, Query: "SELECT 1", DeltaCalls: 10, DeltaTotalTime: 250, MeanExecTime: 25},
	}

	msgs := h.tick(t, 2*time.Hour+time.Minute)
	if len(msgs) != 1 || msgs[0].Kind != KindDigest {
		t.Fatalf("msgs = %+v, want one digest", msgs)
	}
	d := msgs[0].Digest
	if d.Critical != 1 || d.Warning != 1 || d.NewTotal != 1 || d.ResolvedTotal != 1 {
		t.Errorf("digest counts = %+v", d)
	}
	if len(d.TopQueries) != 2 || d.TopQueries[0].QueryID != 7 || d.TopQueries[0].Share != 0.75 {
		t.Errorf("top queries = %+v, want query 7 first at 75%%", d.TopQueries)
	}
	if d.TopQueries[0].Query != "SELECT * FROM orders" {
		t.Errorf("query not normalised: %q", d.TopQueries[0].Query)
	}

	if msgs := h.tick(t, time.Hour); len(msgs) != 0 {
		t.Errorf("digest sent twice for one slot: %+v", msgs)
	}
}

func TestAlertTitle(t *testing.T) {
	got := alertTitle([]Item{
		{Event: EventOpened, Severity: "critical"},
		{Event: EventOpened, Severity: "critical"},
		{Event: EventResolved, Severity: "critical"},
	})
	if got != "2 new critical, 1 resolved" {
		t.Errorf("alertTitle = %q", got)
	}
}
