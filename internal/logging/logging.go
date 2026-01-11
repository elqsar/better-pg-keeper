// Package logging provides structured logging setup for pganalyzer.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/elqsar/pganalyzer/internal/config"
)

// Setup initializes the global logger based on configuration.
// Returns the configured logger.
func Setup(cfg config.LoggingConfig) *slog.Logger {
	level := parseLevel(cfg.Level)
	handler := createHandler(cfg.Format, os.Stdout, level)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// SetupWithWriter initializes the logger with a custom writer.
func SetupWithWriter(cfg config.LoggingConfig, w io.Writer) *slog.Logger {
	level := parseLevel(cfg.Level)
	handler := createHandler(cfg.Format, w, level)
	return slog.New(handler)
}

// parseLevel converts a string log level to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// createHandler creates a slog.Handler based on format.
func createHandler(format string, w io.Writer, level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{
		Level: level,
	}

	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(w, opts)
	default:
		return slog.NewTextHandler(w, opts)
	}
}

// LevelFromString converts a string to slog.Level.
func LevelFromString(level string) slog.Level {
	return parseLevel(level)
}

// LogLevel represents a log level that can be used in contexts.
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)
