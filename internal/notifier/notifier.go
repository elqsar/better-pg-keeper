package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
)

// tickInterval is how often the notifier checks for something to send. Alerts
// therefore go out within a minute of the analysis that raised them.
const tickInterval = time.Minute

// digestSlotKey stores the scheduled time of the last digest sent.
const digestSlotKey = "last_digest_slot"

// Storage is what the notifier reads and records.
type Storage interface {
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
	ListNotificationStates(ctx context.Context, instanceID int64) ([]models.NotificationState, error)
	UpsertNotificationState(ctx context.Context, st *models.NotificationState) error
	DeleteNotificationState(ctx context.Context, instanceID int64, ruleID, targetObject string) error
	GetNotifierTime(ctx context.Context, instanceID int64, key string) (time.Time, bool, error)
	SetNotifierTime(ctx context.Context, instanceID int64, key string, t time.Time) error
	GetEarliestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notBefore time.Time) (*models.Snapshot, error)
	GetLatestSnapshotWithCollector(ctx context.Context, instanceID int64, collector string, notAfter time.Time) (*models.Snapshot, error)
	GetQueryStatsDelta(ctx context.Context, fromSnapshotID, toSnapshotID int64) ([]models.QueryStatDelta, error)
}

// CollectionStatus reports whether collection is working.
type CollectionStatus interface {
	// CollectionStatus returns when a collection last fully succeeded (zero if
	// never) and the latest collection error, if the latest one failed.
	CollectionStatus() (lastSuccess time.Time, lastError string)
}

// Options configures a Notifier.
type Options struct {
	Config     config.NotificationsConfig
	Storage    Storage
	Collection CollectionStatus
	Channels   []Channel
	InstanceID int64
	// Instance names the monitored database in messages, e.g. host:port/db.
	Instance string
	Logger   *slog.Logger
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// Notifier decides what to send and sends it.
type Notifier struct {
	cfg        config.NotificationsConfig
	storage    Storage
	collection CollectionStatus
	channels   []Channel
	instanceID int64
	instance   string
	logger     *slog.Logger
	now        func() time.Time
	started    time.Time

	mu sync.Mutex // serialises ticks
	// The collection alert lives in memory: after a restart, collection has to
	// fail for a full CollectionStaleAfter again before alerting.
	collectionAlerted   bool
	collectionAlertedAt time.Time
}

// New creates a Notifier.
func New(opts Options) (*Notifier, error) {
	if opts.Storage == nil {
		return nil, errors.New("notifier: storage is required")
	}
	if len(opts.Channels) == 0 {
		return nil, errors.New("notifier: at least one channel is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Notifier{
		cfg:        opts.Config,
		storage:    opts.Storage,
		collection: opts.Collection,
		channels:   opts.Channels,
		instanceID: opts.InstanceID,
		instance:   opts.Instance,
		logger:     opts.Logger,
		now:        opts.Now,
		started:    opts.Now(),
	}, nil
}

// Run checks for notifications every minute until ctx is cancelled.
func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.Tick(ctx)
		}
	}
}

// Tick runs one round of checks. Each check logs its own failures so one broken
// part does not stop the others.
func (n *Notifier) Tick(ctx context.Context) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if err := n.checkCollection(ctx); err != nil {
		n.logger.Warn("notifier: collection check failed", "error", err)
	}
	if err := n.evaluateSuggestions(ctx); err != nil {
		n.logger.Warn("notifier: alert evaluation failed", "error", err)
	}
	if err := n.maybeSendDigest(ctx); err != nil {
		n.logger.Warn("notifier: digest failed", "error", err)
	}
}

// SendTest sends a message confirming the channels work. Every channel must
// accept it, since the point is to check each one.
func SendTest(ctx context.Context, channels []Channel, instance, dashboardURL string) error {
	m := Message{
		Kind: KindTest, Instance: instance, Title: "test notification",
		Detail: "Notifications are set up correctly.", URL: dashboardURL, SentAt: time.Now(),
	}
	var errs []error
	for _, ch := range channels {
		if err := ch.Send(ctx, m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ch.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// deliver sends to every channel. It succeeds if any channel accepted the
// message: state is then recorded, so a permanently broken channel cannot make
// the working ones repeat the same alert every minute.
func (n *Notifier) deliver(ctx context.Context, m Message) error {
	m.Instance = n.instance
	m.SentAt = n.now()

	var errs []error
	delivered := 0
	for _, ch := range n.channels {
		if err := ch.Send(ctx, m); err != nil {
			n.logger.Warn("notifier: delivery failed", "channel", ch.Name(), "kind", m.Kind, "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", ch.Name(), err))
			continue
		}
		delivered++
	}
	if delivered == 0 {
		return errors.Join(errs...)
	}
	n.logger.Info("notifier: sent", "kind", m.Kind, "title", m.Title, "channels", delivered)
	return nil
}

func stateKey(ruleID, target string) string { return ruleID + "\x00" + target }

// evaluateSuggestions compares alertable suggestions with what has already been
// sent, and sends one message covering everything that changed.
//
// An issue alerts when it first reaches the minimum severity, when its severity
// rises, and again every RenotifyAfter while it stays active. When it stops being
// alertable it is announced only after ResolveGrace, and only if it has not
// come back in the meantime, which absorbs metrics hovering around a threshold.
func (n *Notifier) evaluateSuggestions(ctx context.Context) error {
	now := n.now()
	minRank := severityRank(n.cfg.MinSeverity)

	active, err := n.storage.GetSuggestionsByStatus(ctx, n.instanceID, models.StatusActive)
	if err != nil {
		return err
	}
	resolved, err := n.storage.GetSuggestionsByStatus(ctx, n.instanceID, models.StatusResolved)
	if err != nil {
		return err
	}
	stateList, err := n.storage.ListNotificationStates(ctx, n.instanceID)
	if err != nil {
		return err
	}

	states := make(map[string]models.NotificationState, len(stateList))
	for _, st := range stateList {
		states[stateKey(st.RuleID, st.TargetObject)] = st
	}
	activeByKey := make(map[string]models.Suggestion, len(active))
	for _, s := range active {
		activeByKey[stateKey(s.RuleID, s.TargetObject)] = s
	}
	resolvedKeys := make(map[string]bool, len(resolved))
	for _, s := range resolved {
		resolvedKeys[stateKey(s.RuleID, s.TargetObject)] = true
	}

	var items []Item
	// Writes that record a sent item are applied only if delivery succeeds, so
	// a failed send is retried next tick. Silent bookkeeping is always applied.
	var onDelivered, always []func() error

	for _, sug := range active {
		if severityRank(sug.Severity) < minRank {
			continue
		}
		key := stateKey(sug.RuleID, sug.TargetObject)
		st, known := states[key]
		next := models.NotificationState{
			InstanceID: n.instanceID, RuleID: sug.RuleID, TargetObject: sug.TargetObject,
			Severity: sug.Severity, NotifiedAt: now,
		}

		switch {
		case !known:
			items = append(items, n.item(EventOpened, sug, ""))
			onDelivered = append(onDelivered, n.upsert(next))
		case severityRank(sug.Severity) > severityRank(st.Severity):
			items = append(items, n.item(EventEscalated, sug, st.Severity))
			onDelivered = append(onDelivered, n.upsert(next))
		case st.ClearedAt != nil:
			// Back within the grace period: it never really went away.
			next.NotifiedAt = st.NotifiedAt
			always = append(always, n.upsert(next))
		case n.cfg.RenotifyAfter > 0 && now.Sub(st.NotifiedAt) >= n.cfg.RenotifyAfter.Duration():
			items = append(items, n.item(EventReminder, sug, ""))
			onDelivered = append(onDelivered, n.upsert(next))
		case sug.Severity != st.Severity:
			// Lower but still alertable: record it so a later rise alerts again.
			next.NotifiedAt = st.NotifiedAt
			always = append(always, n.upsert(next))
		}
	}

	for key, st := range states {
		if sug, ok := activeByKey[key]; ok && severityRank(sug.Severity) >= minRank {
			continue
		}
		if st.ClearedAt == nil {
			cleared := st
			cleared.ClearedAt = &now
			always = append(always, n.upsert(cleared))
			continue
		}
		if now.Sub(*st.ClearedAt) < n.cfg.ResolveGrace.Duration() {
			continue
		}

		forget := n.forget(st)
		var item *Item
		if n.cfg.NotifyResolved {
			if sug, ok := activeByKey[key]; ok {
				it := n.item(EventDowngraded, sug, st.Severity)
				item = &it
			} else if resolvedKeys[key] {
				// Dismissed or deleted suggestions are dropped silently: someone
				// already decided about them.
				it := Item{Event: EventResolved, Severity: st.Severity, RuleID: st.RuleID,
					Target: st.TargetObject, Title: resolvedTitle(resolved, st)}
				item = &it
			}
		}
		if item == nil {
			always = append(always, forget)
			continue
		}
		items = append(items, *item)
		onDelivered = append(onDelivered, forget)
	}

	var errs []error
	for _, apply := range always {
		errs = append(errs, apply())
	}
	if len(items) > 0 {
		sortItems(items)
		msg := Message{Kind: KindAlert, Title: alertTitle(items), Items: items, URL: n.link("/suggestions")}
		if err := n.deliver(ctx, msg); err != nil {
			errs = append(errs, err)
			return errors.Join(errs...)
		}
		for _, apply := range onDelivered {
			errs = append(errs, apply())
		}
	}
	return errors.Join(errs...)
}

func (n *Notifier) upsert(st models.NotificationState) func() error {
	return func() error { return n.storage.UpsertNotificationState(context.Background(), &st) }
}

func (n *Notifier) forget(st models.NotificationState) func() error {
	return func() error {
		return n.storage.DeleteNotificationState(context.Background(), st.InstanceID, st.RuleID, st.TargetObject)
	}
}

func (n *Notifier) item(event string, sug models.Suggestion, previous string) Item {
	return Item{
		Event: event, Severity: sug.Severity, PreviousSeverity: previous,
		RuleID: sug.RuleID, Target: sug.TargetObject, Title: sug.Title,
		URL: n.link(fmt.Sprintf("/suggestions/%d", sug.ID)),
	}
}

// link returns an absolute dashboard URL for path, or "" without a dashboard URL.
func (n *Notifier) link(path string) string {
	if n.cfg.DashboardURL == "" {
		return ""
	}
	return strings.TrimRight(n.cfg.DashboardURL, "/") + path
}

func resolvedTitle(resolved []models.Suggestion, st models.NotificationState) string {
	for _, s := range resolved {
		if s.RuleID == st.RuleID && s.TargetObject == st.TargetObject {
			return s.Title
		}
	}
	return st.RuleID + " " + st.TargetObject
}

// alertTitle summarises the items, e.g. "2 new critical, 1 resolved".
func alertTitle(items []Item) string {
	counts := map[string]int{}
	var order []string
	add := func(label string) {
		if counts[label] == 0 {
			order = append(order, label)
		}
		counts[label]++
	}
	for _, it := range items {
		switch it.Event {
		case EventOpened:
			add("new " + it.Severity)
		case EventEscalated:
			add("escalated to " + it.Severity)
		case EventReminder:
			add("still " + it.Severity)
		case EventResolved, EventDowngraded:
			add("resolved")
		}
	}
	parts := make([]string, 0, len(order))
	for _, label := range order {
		parts = append(parts, fmt.Sprintf("%d %s", counts[label], label))
	}
	return strings.Join(parts, ", ")
}

// checkCollection alerts when no collection has succeeded for
// CollectionStaleAfter, repeats every RenotifyAfter, and announces recovery.
func (n *Notifier) checkCollection(ctx context.Context) error {
	threshold := n.cfg.CollectionStaleAfter.Duration()
	if n.collection == nil || threshold <= 0 {
		return nil
	}
	now := n.now()
	lastSuccess, lastErr := n.collection.CollectionStatus()
	// Before the first success, measure from startup rather than from never.
	since := lastSuccess
	if since.Before(n.started) {
		since = n.started
	}
	down := now.Sub(since)

	if down >= threshold {
		repeat := n.cfg.RenotifyAfter.Duration()
		if n.collectionAlerted && (repeat <= 0 || now.Sub(n.collectionAlertedAt) < repeat) {
			return nil
		}
		detail := fmt.Sprintf("No successful collection for %s, so issues are not being detected.",
			down.Round(time.Minute))
		if lastErr != "" {
			detail += " Last error: " + shortQuery(lastErr, 300)
		}
		if err := n.deliver(ctx, Message{Kind: KindHealth, Title: "⚠️ data collection is failing", Detail: detail, URL: n.link("/")}); err != nil {
			return err
		}
		n.collectionAlerted, n.collectionAlertedAt = true, now
		return nil
	}

	if n.collectionAlerted {
		if err := n.deliver(ctx, Message{Kind: KindHealth, Title: "✅ data collection recovered", URL: n.link("/")}); err != nil {
			return err
		}
		n.collectionAlerted = false
	}
	return nil
}
