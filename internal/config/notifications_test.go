package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validNotifications() NotificationsConfig {
	n := defaultNotifications()
	n.Enabled = true
	n.Channels = []ChannelConfig{{Type: ChannelSlack, URL: "https://hooks.slack.com/services/x"}}
	return n
}

func TestValidateNotifications(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*NotificationsConfig)
		wantErr string
	}{
		{"valid", func(*NotificationsConfig) {}, ""},
		{"disabled skips checks", func(n *NotificationsConfig) { n.Enabled = false; n.Channels = nil }, ""},
		{"no channels", func(n *NotificationsConfig) { n.Channels = nil }, "notifications.channels"},
		{"unset env var leaves empty url", func(n *NotificationsConfig) { n.Channels[0].URL = "" }, "environment variable"},
		{"bad channel type", func(n *NotificationsConfig) { n.Channels[0].Type = "email" }, "channels[0].type"},
		{"bad severity", func(n *NotificationsConfig) { n.MinSeverity = "high" }, "min_severity"},
		{"relative dashboard url", func(n *NotificationsConfig) { n.DashboardURL = "pga.local" }, "dashboard_url"},
		{"bad schedule", func(n *NotificationsConfig) { n.Digest.Schedule = "hourly" }, "digest.schedule"},
		{"bad weekday", func(n *NotificationsConfig) { n.Digest.Weekday = "someday" }, "digest.weekday"},
		{"bad hour", func(n *NotificationsConfig) { n.Digest.Hour = 24 }, "digest.hour"},
		{"weekday ignored for daily", func(n *NotificationsConfig) { n.Digest.Schedule = "daily"; n.Digest.Weekday = "" }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := validNotifications()
			tt.mutate(&n)
			errs := validateNotifications(&n)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(errs.Error(), tt.wantErr) {
				t.Fatalf("errors = %v, want one mentioning %q", errs, tt.wantErr)
			}
		})
	}
}

func TestParseWeekday(t *testing.T) {
	for in, want := range map[string]time.Weekday{"monday": time.Monday, "Fri": time.Friday, " sunday ": time.Sunday} {
		if got, err := ParseWeekday(in); err != nil || got != want {
			t.Errorf("ParseWeekday(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestLoadNotificationsFromYAML(t *testing.T) {
	t.Setenv("TEST_SLACK_URL", "https://hooks.slack.com/services/abc")
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
postgres:
  host: localhost
  database: app
  user: u
server:
  auth:
    enabled: false
notifications:
  enabled: true
  min_severity: warning
  channels:
    - type: slack
      url: ${TEST_SLACK_URL}
    - type: webhook
      url: https://example.com/hook
      headers:
        Authorization: Bearer x
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n := cfg.Notifications
	if n.MinSeverity != "warning" || len(n.Channels) != 2 || n.Channels[0].URL != "https://hooks.slack.com/services/abc" {
		t.Errorf("notifications = %+v", n)
	}
	// Unset keys keep their defaults.
	if n.RenotifyAfter.Duration() != 24*time.Hour || !n.Digest.Enabled || n.Digest.Weekday != "monday" {
		t.Errorf("defaults lost: %+v", n)
	}
	if n.Channels[1].Headers["Authorization"] != "Bearer x" {
		t.Errorf("headers = %v", n.Channels[1].Headers)
	}
}
