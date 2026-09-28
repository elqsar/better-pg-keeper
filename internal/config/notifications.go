package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// NotificationsConfig controls alerts and digests sent to chat or webhooks.
type NotificationsConfig struct {
	Enabled bool `yaml:"enabled"`
	// MinSeverity is the lowest suggestion severity that triggers an alert:
	// critical, warning or info. The digest always covers every severity.
	MinSeverity string `yaml:"min_severity"`
	// RenotifyAfter repeats an alert for an issue still active after this long.
	// Zero never repeats.
	RenotifyAfter Duration `yaml:"renotify_after"`
	// NotifyResolved announces when an alerted issue goes away.
	NotifyResolved bool `yaml:"notify_resolved"`
	// ResolveGrace is how long an issue must stay gone before it is announced
	// as resolved. An issue that returns within it is treated as never having
	// left, so a metric hovering around a threshold does not alert repeatedly.
	ResolveGrace Duration `yaml:"resolve_grace"`
	// DashboardURL is the externally reachable base URL of the web UI, used to
	// link alerts to suggestion pages. Optional.
	DashboardURL string `yaml:"dashboard_url"`
	// CollectionStaleAfter raises an alert when no collection has succeeded for
	// this long, e.g. because PostgreSQL is unreachable. Zero disables it.
	CollectionStaleAfter Duration        `yaml:"collection_stale_after"`
	Digest               DigestConfig    `yaml:"digest"`
	Channels             []ChannelConfig `yaml:"channels"`
}

// DigestConfig schedules a periodic summary of the database's state.
type DigestConfig struct {
	Enabled bool `yaml:"enabled"`
	// Schedule is daily or weekly.
	Schedule string `yaml:"schedule"`
	// Weekday is the day a weekly digest is sent, e.g. monday.
	Weekday string `yaml:"weekday"`
	// Hour is the hour of day (0-23, server local time) the digest is sent.
	Hour int `yaml:"hour"`
}

// ChannelConfig is one destination for notifications.
type ChannelConfig struct {
	// Type is slack (an incoming webhook) or webhook (JSON POST).
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	// Headers are added to webhook requests, e.g. Authorization.
	Headers map[string]string `yaml:"headers"`
}

// Channel types.
const (
	ChannelSlack   = "slack"
	ChannelWebhook = "webhook"
)

func defaultNotifications() NotificationsConfig {
	return NotificationsConfig{
		Enabled:              false,
		MinSeverity:          "critical",
		RenotifyAfter:        Duration(24 * time.Hour),
		NotifyResolved:       true,
		ResolveGrace:         Duration(time.Hour),
		CollectionStaleAfter: Duration(15 * time.Minute),
		Digest: DigestConfig{
			Enabled:  true,
			Schedule: "weekly",
			Weekday:  "monday",
			Hour:     9,
		},
	}
}

// ParseWeekday converts a day name such as "monday" or "mon" to a time.Weekday.
func ParseWeekday(s string) (time.Weekday, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for d := time.Sunday; d <= time.Saturday; d++ {
		name := strings.ToLower(d.String())
		if s == name || s == name[:3] {
			return d, nil
		}
	}
	return 0, fmt.Errorf("unknown weekday %q", s)
}

func validateNotifications(cfg *NotificationsConfig) ValidationErrors {
	var errs ValidationErrors
	if !cfg.Enabled {
		return nil
	}

	switch cfg.MinSeverity {
	case "critical", "warning", "info":
	default:
		errs = append(errs, ValidationError{
			Field:   "notifications.min_severity",
			Message: fmt.Sprintf("must be critical, warning or info, got %q", cfg.MinSeverity),
		})
	}

	if cfg.RenotifyAfter < 0 || cfg.ResolveGrace < 0 || cfg.CollectionStaleAfter < 0 {
		errs = append(errs, ValidationError{
			Field:   "notifications",
			Message: "renotify_after, resolve_grace and collection_stale_after must not be negative",
		})
	}

	if cfg.DashboardURL != "" {
		if u, err := url.Parse(cfg.DashboardURL); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, ValidationError{
				Field:   "notifications.dashboard_url",
				Message: fmt.Sprintf("must be an absolute URL, got %q", cfg.DashboardURL),
			})
		}
	}

	if cfg.Digest.Enabled {
		if cfg.Digest.Schedule != "daily" && cfg.Digest.Schedule != "weekly" {
			errs = append(errs, ValidationError{
				Field:   "notifications.digest.schedule",
				Message: fmt.Sprintf("must be daily or weekly, got %q", cfg.Digest.Schedule),
			})
		}
		if cfg.Digest.Schedule == "weekly" {
			if _, err := ParseWeekday(cfg.Digest.Weekday); err != nil {
				errs = append(errs, ValidationError{Field: "notifications.digest.weekday", Message: err.Error()})
			}
		}
		if cfg.Digest.Hour < 0 || cfg.Digest.Hour > 23 {
			errs = append(errs, ValidationError{
				Field:   "notifications.digest.hour",
				Message: fmt.Sprintf("must be between 0 and 23, got %d", cfg.Digest.Hour),
			})
		}
	}

	if len(cfg.Channels) == 0 {
		errs = append(errs, ValidationError{
			Field:   "notifications.channels",
			Message: "at least one channel is required when notifications are enabled",
		})
	}
	for i, ch := range cfg.Channels {
		field := fmt.Sprintf("notifications.channels[%d]", i)
		if ch.Type != ChannelSlack && ch.Type != ChannelWebhook {
			errs = append(errs, ValidationError{
				Field:   field + ".type",
				Message: fmt.Sprintf("must be slack or webhook, got %q", ch.Type),
			})
		}
		// An unset environment variable expands to an empty URL.
		if u, err := url.Parse(ch.URL); ch.URL == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, ValidationError{
				Field:   field + ".url",
				Message: "must be an http(s) URL (is its environment variable set?)",
			})
		}
	}

	return errs
}
