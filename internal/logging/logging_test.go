package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/elqsar/pganalyzer/internal/config"
)

func TestLevelFromString(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tc := range tests {
		if got := LevelFromString(tc.input); got != tc.want {
			t.Errorf("LevelFromString(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestSetupWithWriter_JSONFormatAndLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := SetupWithWriter(config.LoggingConfig{
		Level:  "warn",
		Format: "json",
	}, &buf)

	logger.Info("ignored info message")
	logger.Warn("kept warn message", "key", "val")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d: %q", len(lines), buf.String())
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("expected valid JSON log line: %v (%q)", err, lines[0])
	}
	if parsed["msg"] != "kept warn message" {
		t.Errorf("msg = %v, want 'kept warn message'", parsed["msg"])
	}
	if parsed["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", parsed["level"])
	}
	if parsed["key"] != "val" {
		t.Errorf("key = %v, want val", parsed["key"])
	}
}

func TestSetupWithWriter_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := SetupWithWriter(config.LoggingConfig{
		Level:  "debug",
		Format: "text",
	}, &buf)

	logger.Debug("debug hello", "count", 3)

	out := buf.String()
	if !strings.Contains(out, "level=DEBUG") || !strings.Contains(out, `msg="debug hello"`) || !strings.Contains(out, "count=3") {
		t.Fatalf("unexpected text log output: %q", out)
	}
}
