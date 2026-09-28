// Package notifier sends alerts and periodic digests to chat and webhooks, so a
// team learns about database problems without watching the dashboard.
package notifier

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elqsar/pganalyzer/internal/models"
)

// Message kinds.
const (
	KindAlert  = "alert"
	KindHealth = "health"
	KindDigest = "digest"
	KindTest   = "test"
)

// Item events.
const (
	EventOpened     = "opened"
	EventEscalated  = "escalated"
	EventReminder   = "still_active"
	EventResolved   = "resolved"
	EventDowngraded = "downgraded"
)

// maxListed caps how many items a message lists before summarising the rest.
const maxListed = 15

// Message is one notification. Channels render it for their destination; the
// webhook channel sends it as JSON, with Text holding a Markdown rendering.
type Message struct {
	Kind     string    `json:"kind"`
	Instance string    `json:"instance"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail,omitempty"`
	Items    []Item    `json:"items,omitempty"`
	Digest   *Digest   `json:"digest,omitempty"`
	URL      string    `json:"url,omitempty"`
	SentAt   time.Time `json:"sent_at"`
	Text     string    `json:"text"`
}

// Item is one issue in an alert or digest.
type Item struct {
	Event            string `json:"event"`
	Severity         string `json:"severity"`
	PreviousSeverity string `json:"previous_severity,omitempty"`
	RuleID           string `json:"rule_id"`
	Target           string `json:"target"`
	Title            string `json:"title"`
	URL              string `json:"url,omitempty"`
}

// Digest summarises a period.
type Digest struct {
	PeriodStart    time.Time  `json:"period_start"`
	PeriodEnd      time.Time  `json:"period_end"`
	Critical       int        `json:"critical"`
	Warning        int        `json:"warning"`
	Info           int        `json:"info"`
	New            []Item     `json:"new"`
	NewTotal       int        `json:"new_total"`
	Resolved       []Item     `json:"resolved"`
	ResolvedTotal  int        `json:"resolved_total"`
	TopQueries     []TopQuery `json:"top_queries,omitempty"`
	LastCollection time.Time  `json:"last_collection,omitempty"`
}

// TopQuery is a query's share of database execution time over the period.
type TopQuery struct {
	QueryID     int64   `json:"queryid"`
	Query       string  `json:"query"`
	TotalTimeMs float64 `json:"total_time_ms"`
	Share       float64 `json:"share"` // 0-1 of all execution time in the period
	Calls       int64   `json:"calls"`
	MeanTimeMs  float64 `json:"mean_time_ms"`
	URL         string  `json:"url,omitempty"`
}

func severityRank(s string) int {
	switch s {
	case models.SeverityCritical:
		return 3
	case models.SeverityWarning:
		return 2
	case models.SeverityInfo:
		return 1
	}
	return 0
}

// sortItems orders items most severe first, keeping the given order otherwise.
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		return severityRank(items[i].Severity) > severityRank(items[j].Severity)
	})
}

// format abstracts the few differences between Slack mrkdwn and Markdown.
type format struct {
	bold func(string) string
	link func(text, url string) string
	code func(string) string
	esc  func(string) string
}

var markdown = format{
	bold: func(s string) string { return "**" + s + "**" },
	link: func(text, url string) string { return "[" + text + "](" + url + ")" },
	code: func(s string) string { return "`" + strings.ReplaceAll(s, "`", "'") + "`" },
	esc:  func(s string) string { return s },
}

var slackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

var slackMrkdwn = format{
	bold: func(s string) string { return "*" + s + "*" },
	link: func(text, url string) string { return "<" + url + "|" + text + ">" },
	code: func(s string) string { return "`" + strings.ReplaceAll(s, "`", "'") + "`" },
	esc:  slackEscaper.Replace,
}

// render produces the human-readable body of a message.
func render(m Message, f format) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", f.bold(f.esc("pganalyzer · "+m.Instance)), f.esc(m.Title))
	if m.Detail != "" {
		b.WriteString(f.esc(m.Detail) + "\n")
	}

	if m.Digest != nil {
		renderDigest(&b, m.Digest, f)
	} else {
		renderItems(&b, m.Items, f)
	}

	if m.URL != "" {
		b.WriteString("\n" + f.link("Open dashboard", m.URL) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderItems(b *strings.Builder, items []Item, f format) {
	for i, it := range items {
		if i == maxListed {
			fmt.Fprintf(b, "…and %d more\n", len(items)-maxListed)
			break
		}
		b.WriteString("• " + itemLine(it, f) + "\n")
	}
}

func itemLine(it Item, f format) string {
	title := f.esc(it.Title)
	if it.URL != "" {
		title = f.link(title, it.URL)
	}
	switch it.Event {
	case EventResolved:
		return "✅ Resolved: " + title
	case EventDowngraded:
		return fmt.Sprintf("✅ Now %s: %s", it.Severity, title)
	case EventEscalated:
		return fmt.Sprintf("%s %s (was %s): %s", severityEmoji(it.Severity), strings.ToUpper(it.Severity), it.PreviousSeverity, title)
	case EventReminder:
		return fmt.Sprintf("%s %s, still active: %s", severityEmoji(it.Severity), strings.ToUpper(it.Severity), title)
	default:
		return fmt.Sprintf("%s %s: %s", severityEmoji(it.Severity), strings.ToUpper(it.Severity), title)
	}
}

func severityEmoji(s string) string {
	switch s {
	case models.SeverityCritical:
		return "🔴"
	case models.SeverityWarning:
		return "🟠"
	default:
		return "🔵"
	}
}

func renderDigest(b *strings.Builder, d *Digest, f format) {
	fmt.Fprintf(b, "Active issues: %d critical, %d warning, %d info\n", d.Critical, d.Warning, d.Info)

	fmt.Fprintf(b, "\n%s\n", f.bold(fmt.Sprintf("New in this period (%d)", d.NewTotal)))
	if d.NewTotal == 0 {
		b.WriteString("None\n")
	}
	renderItems(b, d.New, f)
	if more := d.NewTotal - len(d.New); more > 0 && len(d.New) < maxListed {
		fmt.Fprintf(b, "…and %d more\n", more)
	}

	fmt.Fprintf(b, "\n%s\n", f.bold(fmt.Sprintf("Resolved in this period (%d)", d.ResolvedTotal)))
	renderItems(b, d.Resolved, f)
	if more := d.ResolvedTotal - len(d.Resolved); more > 0 && len(d.Resolved) < maxListed {
		fmt.Fprintf(b, "…and %d more\n", more)
	}

	if len(d.TopQueries) > 0 {
		fmt.Fprintf(b, "\n%s\n", f.bold("Top queries by total execution time"))
		for i, q := range d.TopQueries {
			line := fmt.Sprintf("%d. %.0f%% of DB time · %d calls · %s avg — %s",
				i+1, q.Share*100, q.Calls, formatMs(q.MeanTimeMs), f.code(q.Query))
			if q.URL != "" {
				line += " " + f.link("details", q.URL)
			}
			b.WriteString(line + "\n")
		}
	}

	if d.LastCollection.IsZero() {
		b.WriteString("\n⚠️ No successful collection since pganalyzer started\n")
	} else {
		fmt.Fprintf(b, "\nLast successful collection: %s\n", d.LastCollection.Format("2006-01-02 15:04 MST"))
	}
}

func formatMs(ms float64) string {
	switch {
	case ms >= 1000:
		return fmt.Sprintf("%.1f s", ms/1000)
	case ms >= 10:
		return fmt.Sprintf("%.0f ms", ms)
	default:
		return fmt.Sprintf("%.1f ms", ms)
	}
}

// shortQuery collapses whitespace and truncates a query for display.
func shortQuery(q string, max int) string {
	q = strings.Join(strings.Fields(q), " ")
	if len([]rune(q)) <= max {
		return q
	}
	return string([]rune(q)[:max-1]) + "…"
}
