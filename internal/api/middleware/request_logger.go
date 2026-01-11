package middleware

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// RequestLoggerConfig holds configuration for request logging.
type RequestLoggerConfig struct {
	Enabled   bool
	SkipPaths []string // Paths to skip logging (e.g., health checks)
}

// DefaultRequestLoggerConfig returns a default configuration.
func DefaultRequestLoggerConfig() RequestLoggerConfig {
	return RequestLoggerConfig{
		Enabled:   true,
		SkipPaths: []string{"/health"},
	}
}

// RequestLogger returns a middleware that logs HTTP requests.
func RequestLogger(cfg RequestLoggerConfig) echo.MiddlewareFunc {
	skipMap := make(map[string]bool)
	for _, path := range cfg.SkipPaths {
		skipMap[path] = true
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !cfg.Enabled {
				return next(c)
			}

			// Skip logging for certain paths
			if skipMap[c.Path()] {
				return next(c)
			}

			start := time.Now()
			req := c.Request()

			// Process request
			err := next(c)

			// Calculate duration
			duration := time.Since(start)
			res := c.Response()

			// Determine log level based on status code
			level := slog.LevelInfo
			if res.Status >= 500 {
				level = slog.LevelError
			} else if res.Status >= 400 {
				level = slog.LevelWarn
			}

			// Build log attributes
			attrs := []any{
				"method", req.Method,
				"path", req.URL.Path,
				"status", res.Status,
				"duration_ms", duration.Milliseconds(),
				"size", res.Size,
			}

			// Add query string if present
			if req.URL.RawQuery != "" {
				attrs = append(attrs, "query", req.URL.RawQuery)
			}

			// Add client IP
			attrs = append(attrs, "ip", c.RealIP())

			// Add request ID if present
			if reqID := c.Response().Header().Get(echo.HeaderXRequestID); reqID != "" {
				attrs = append(attrs, "request_id", reqID)
			}

			// Add error if present
			if err != nil {
				attrs = append(attrs, "error", err.Error())
			}

			// Log the request
			slog.Log(c.Request().Context(), level, "http request", attrs...)

			return err
		}
	}
}

// RequestLoggerWithConfig returns request logger middleware with custom config.
func RequestLoggerWithConfig(enabled bool, skipPaths ...string) echo.MiddlewareFunc {
	cfg := RequestLoggerConfig{
		Enabled:   enabled,
		SkipPaths: skipPaths,
	}
	return RequestLogger(cfg)
}
