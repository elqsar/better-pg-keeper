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
		{Setting{Setting: "2", Unit: "TB"}, 2 << 40, true},
		{Setting{Setting: "4", Unit: "16kB"}, 64 << 10, true},
		{Setting{Setting: "2", Unit: "64MB"}, 128 << 20, true},
		{Setting{Setting: "999999999999", Unit: "TB"}, 0, false}, // int64 overflow
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
		{Setting{Setting: "NaN", Unit: "ms"}, 0, false},
		{Setting{Setting: "+Inf", Unit: "ms"}, 0, false},
		{Setting{Setting: "1e30", Unit: "d"}, 0, false},
		{Setting{Setting: "on"}, 0, false},
	}
	for _, tc := range durations {
		got, ok := tc.s.Duration()
		if got != tc.want || ok != tc.ok {
			t.Errorf("Duration(%s %s) = %s, %v; want %s, %v", tc.s.Setting, tc.s.Unit, got, ok, tc.want, tc.ok)
		}
	}

	if _, ok := (Setting{Setting: "NaN"}).Float(); ok {
		t.Error("Float(NaN) should return ok=false")
	}
	if _, ok := (Setting{Setting: "+Inf"}).Float(); ok {
		t.Error("Float(+Inf) should return ok=false")
	}

	display := map[Setting]string{
		{Setting: "16384", Unit: "8kB"}:                                   "128MB",
		{Setting: "4096", Unit: "kB"}:                                     "4MB",
		{Setting: "1536", Unit: "kB"}:                                     "1536kB",
		{Setting: "2", Unit: "64MB"}:                                      "128MB",
		{Setting: "0", Unit: "ms"}:                                        "0 (disabled)",
		{Setting: "30000", Unit: "ms"}:                                    "30s",
		{Setting: "-1", Unit: "ms"}:                                       "-1 (disabled)",
		{Name: "log_min_duration_statement", Setting: "0", Unit: "ms"}:    "0ms (log all)",
		{Name: "log_min_duration_statement", Setting: "-1", Unit: "ms"}:   "-1 (disabled)",
		{Name: "log_temp_files", Setting: "0", Unit: "kB"}:                "0kB (log all)",
		{Name: "autovacuum_vacuum_cost_delay", Setting: "0", Unit: "ms"}:  "0ms",
		{Name: "autovacuum_vacuum_cost_delay", Setting: "-1", Unit: "ms"}: "-1 (inherit)",
		{Name: "autovacuum_work_mem", Setting: "-1", Unit: "kB"}:          "-1 (inherit)",
		{Name: "autovacuum_vacuum_cost_limit", Setting: "-1"}:             "-1 (inherit)",
		{Name: "max_slot_wal_keep_size", Setting: "-1", Unit: "MB"}:       "-1 (unlimited)",
		{Setting: "off"}:  "off",
		{Setting: "5000"}: "5000",
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
