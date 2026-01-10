package web

import (
	"bytes"
	"testing"
	"time"
)

func TestNewTemplateRenderer(t *testing.T) {
	renderer, err := NewTemplateRenderer()
	if err != nil {
		t.Fatalf("NewTemplateRenderer() error = %v", err)
	}
	if renderer == nil {
		t.Fatal("NewTemplateRenderer() returned nil")
	}
	if renderer.templates == nil {
		t.Fatal("templates is nil")
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "zero time",
			input:    time.Time{},
			expected: "Never",
		},
		{
			name:     "valid time",
			input:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expected: "Jan 15, 2024 10:30:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTime(tt.input)
			if result != tt.expected {
				t.Errorf("formatTime() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFormatTimeShort(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{
			name:     "zero time",
			input:    time.Time{},
			expected: "Never",
		},
		{
			name:     "valid time",
			input:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expected: "Jan 15, 10:30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeShort(tt.input)
			if result != tt.expected {
				t.Errorf("formatTimeShort() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		contains string
	}{
		{"microseconds", 0.5, "µs"},
		{"milliseconds", 100.0, "ms"},
		{"seconds", 5000.0, "s"},
		{"minutes", 120000.0, "min"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.input)
			if !bytes.Contains([]byte(result), []byte(tt.contains)) {
				t.Errorf("formatDuration(%v) = %v, expected to contain %v", tt.input, result, tt.contains)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"bytes", 100, "100 B"},
		{"kilobytes", 2048, "2.0 KB"},
		{"megabytes", 1048576, "1.0 MB"},
		{"gigabytes", 1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.input)
			if result != tt.expected {
				t.Errorf("formatBytes(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{"small", 100, "100"},
		{"thousands", 1234, "1,234"},
		{"millions", 1234567, "1,234,567"},
		{"negative", -1234, "-1,234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNumber(tt.input)
			if result != tt.expected {
				t.Errorf("formatNumber(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatPercent(t *testing.T) {
	result := formatPercent(99.123)
	expected := "99.12%"
	if result != expected {
		t.Errorf("formatPercent(99.123) = %v, want %v", result, expected)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %v, want %v", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestSeverityClass(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"critical", "severity-critical"},
		{"warning", "severity-warning"},
		{"info", "severity-info"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := severityClass(tt.input)
			if result != tt.expected {
				t.Errorf("severityClass(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCacheRatioClass(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected string
	}{
		{"excellent", 99.5, "cache-excellent"},
		{"good", 97.0, "cache-good"},
		{"warning", 92.0, "cache-warning"},
		{"critical", 85.0, "cache-critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cacheRatioClass(tt.input)
			if result != tt.expected {
				t.Errorf("cacheRatioClass(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMathFunctions(t *testing.T) {
	if result := add(2, 3); result != 5 {
		t.Errorf("add(2, 3) = %d, want 5", result)
	}
	if result := sub(5, 3); result != 2 {
		t.Errorf("sub(5, 3) = %d, want 2", result)
	}
	if result := mul(3, 4); result != 12 {
		t.Errorf("mul(3, 4) = %d, want 12", result)
	}
	if result := div(10, 2); result != 5 {
		t.Errorf("div(10, 2) = %d, want 5", result)
	}
	if result := div(10, 0); result != 0 {
		t.Errorf("div(10, 0) = %d, want 0", result)
	}
}

func TestSeq(t *testing.T) {
	result := seq(5)
	expected := []int{1, 2, 3, 4, 5}
	if len(result) != len(expected) {
		t.Fatalf("seq(5) len = %d, want %d", len(result), len(expected))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("seq(5)[%d] = %d, want %d", i, v, expected[i])
		}
	}
}

func TestStaticFS(t *testing.T) {
	staticFS := StaticFS()

	// Try to read the style.css file
	content, err := staticFS.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("StaticFS().ReadFile() error = %v", err)
	}
	if len(content) == 0 {
		t.Error("style.css is empty")
	}
}
