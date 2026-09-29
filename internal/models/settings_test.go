package models

import (
	"testing"
	"time"
)

func TestSettingValues(t *testing.T) {
	bytes := []struct {
		s    Setting
		want int64
		ok   bool
	}{
		{Setting{Setting: "16384", Unit: "8kB"}, 128 << 20, true},
		{Setting{Setting: "4096", Unit: "kB"}, 4 << 20, true},
		{Setting{Setting: "1024", Unit: "MB"}, 1 << 30, true},
		{Setting{Setting: "-1", Unit: "kB"}, 0, false},
		{Setting{Setting: "4", Unit: ""}, 0, false},
	}
	for _, tc := range bytes {
		got, ok := tc.s.Bytes()
		if got != tc.want || ok != tc.ok {
			t.Errorf("Bytes(%s %s) = %d, %v; want %d, %v", tc.s.Setting, tc.s.Unit, got, ok, tc.want, tc.ok)
		}
	}

	durations := []struct {
		s    Setting
		want time.Duration
		ok   bool
	}{
		{Setting{Setting: "0", Unit: "ms"}, 0, true},
		{Setting{Setting: "300000", Unit: "ms"}, 5 * time.Minute, true},
		{Setting{Setting: "60", Unit: "s"}, time.Minute, true},
		{Setting{Setting: "1", Unit: "min"}, time.Minute, true},
		{Setting{Setting: "-1", Unit: "ms"}, 0, false},
		{Setting{Setting: "on"}, 0, false},
	}
	for _, tc := range durations {
		got, ok := tc.s.Duration()
		if got != tc.want || ok != tc.ok {
			t.Errorf("Duration(%s %s) = %s, %v; want %s, %v", tc.s.Setting, tc.s.Unit, got, ok, tc.want, tc.ok)
		}
	}

	display := map[Setting]string{
		{Setting: "16384", Unit: "8kB"}: "128MB",
		{Setting: "4096", Unit: "kB"}:   "4MB",
		{Setting: "1536", Unit: "kB"}:   "1536kB",
		{Setting: "0", Unit: "ms"}:      "0 (disabled)",
		{Setting: "30000", Unit: "ms"}:  "30s",
		{Setting: "-1", Unit: "ms"}:     "-1 (disabled)",
		{Setting: "off"}:                "off",
		{Setting: "5000"}:               "5000",
	}
	for s, want := range display {
		if got := s.Display(); got != want {
			t.Errorf("Display(%s %s) = %q, want %q", s.Setting, s.Unit, got, want)
		}
	}

	if !(Setting{Context: "postmaster"}).NeedsRestart() || (Setting{Context: "sighup"}).NeedsRestart() {
		t.Error("NeedsRestart should be true only for postmaster settings")
	}
	var nilSettings *ServerSettings
	if _, ok := nilSettings.Get("work_mem"); ok {
		t.Error("Get on nil settings should report missing")
	}
}
