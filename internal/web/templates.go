// Package web provides the web UI for pganalyzer.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// TemplateRenderer is a custom Echo renderer using html/template.
type TemplateRenderer struct {
	templates *template.Template
}

// NewTemplateRenderer creates a new TemplateRenderer with templates loaded from embedded FS.
func NewTemplateRenderer() (*TemplateRenderer, error) {
	funcMap := template.FuncMap{
		"formatTime":          formatTime,
		"formatTimeShort":     formatTimeShort,
		"formatDuration":      formatDuration,
		"formatBytes":         formatBytes,
		"formatNumber":        formatNumber,
		"formatPercent":       formatPercent,
		"truncate":            truncate,
		"severityClass":       severityClass,
		"severityIcon":        severityIcon,
		"cacheRatioClass":     cacheRatioClass,
		"add":                 add,
		"sub":                 sub,
		"mul":                 mul,
		"div":                 div,
		"seq":                 seq,
		"safeHTML":            safeHTML,
		"severityBadgeClass":  severityBadgeClass,
		"suggestionCardClass": suggestionCardClass,
		"dict":                dict,
		"eq":                  eq,
		"formatDurationSec":   formatDurationSec,
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &TemplateRenderer{templates: tmpl}, nil
}

// Render implements echo.Renderer interface.
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

// StaticFS returns the embedded static files filesystem.
func StaticFS() embed.FS {
	return staticFS
}

// Template helper functions

// formatTime formats a time.Time to a human-readable string.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}
	return t.Format("Jan 2, 2006 15:04:05")
}

// formatTimeShort formats a time.Time to a short date string.
func formatTimeShort(t time.Time) string {
	if t.IsZero() {
		return "Never"
	}
	return t.Format("Jan 2, 15:04")
}

// formatDuration formats milliseconds to a human-readable duration.
func formatDuration(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.2f µs", ms*1000)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.2f ms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.2f s", ms/1000)
	}
	return fmt.Sprintf("%.2f min", ms/60000)
}

// formatDurationSec formats seconds to a human-readable duration.
func formatDurationSec(seconds float64) string {
	if seconds < 1 {
		return fmt.Sprintf("%.0f ms", seconds*1000)
	}
	if seconds < 60 {
		return fmt.Sprintf("%.1f s", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%.1f min", seconds/60)
	}
	return fmt.Sprintf("%.1f h", seconds/3600)
}

// formatBytes formats bytes to a human-readable size.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatNumber formats a number with thousand separators.
func formatNumber(n int64) string {
	str := fmt.Sprintf("%d", n)
	if n < 0 {
		str = str[1:]
	}

	var result strings.Builder
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}

	if n < 0 {
		return "-" + result.String()
	}
	return result.String()
}

// formatPercent formats a float as a percentage.
func formatPercent(f float64) string {
	return fmt.Sprintf("%.2f%%", f)
}

// truncate truncates a string to a maximum length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// severityClass returns the CSS class for a severity level.
func severityClass(severity string) string {
	switch severity {
	case "critical":
		return "severity-critical"
	case "warning":
		return "severity-warning"
	case "info":
		return "severity-info"
	default:
		return ""
	}
}

// severityIcon returns an icon/emoji for a severity level.
func severityIcon(severity string) string {
	switch severity {
	case "critical":
		return "!"
	case "warning":
		return "!"
	case "info":
		return "i"
	default:
		return ""
	}
}

// cacheRatioClass returns a CSS class based on cache hit ratio.
func cacheRatioClass(ratio float64) string {
	if ratio >= 99 {
		return "cache-excellent"
	}
	if ratio >= 95 {
		return "cache-good"
	}
	if ratio >= 90 {
		return "cache-warning"
	}
	return "cache-critical"
}

// add adds two integers.
func add(a, b int) int {
	return a + b
}

// sub subtracts b from a.
func sub(a, b int) int {
	return a - b
}

// mul multiplies two integers.
func mul(a, b int) int {
	return a * b
}

// div divides a by b.
func div(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

// seq generates a sequence of integers from 1 to n.
func seq(n int) []int {
	result := make([]int, n)
	for i := 0; i < n; i++ {
		result[i] = i + 1
	}
	return result
}

// safeHTML marks a string as safe HTML (use with caution).
func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

// severityBadgeClass returns Tailwind CSS classes for severity badges.
func severityBadgeClass(severity string) string {
	switch severity {
	case "critical":
		return "badge-critical"
	case "warning":
		return "badge-warning"
	case "info":
		return "badge-info"
	default:
		return "badge-gray"
	}
}

// suggestionCardClass returns Tailwind CSS classes for suggestion cards.
func suggestionCardClass(severity string) string {
	switch severity {
	case "critical":
		return "suggestion-card-critical"
	case "warning":
		return "suggestion-card-warning"
	case "info":
		return "suggestion-card-info"
	default:
		return "suggestion-card"
	}
}

// dict creates a map from key-value pairs for use in templates.
func dict(values ...interface{}) map[string]interface{} {
	if len(values)%2 != 0 {
		return nil
	}
	d := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			continue
		}
		d[key] = values[i+1]
	}
	return d
}

// eq compares two values for equality.
func eq(a, b interface{}) bool {
	return a == b
}
