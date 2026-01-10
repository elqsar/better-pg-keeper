// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/config"
)

// errorResponse represents a standardized error response.
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// BasicAuth creates a Basic Authentication middleware.
// It validates credentials against the provided config.
// The health endpoint is excluded from authentication.
func BasicAuth(authConfig config.AuthConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth if disabled
			if !authConfig.Enabled {
				return next(c)
			}

			// Skip auth for health endpoint
			if c.Path() == "/health" {
				return next(c)
			}

			// Parse Authorization header
			username, password, ok := c.Request().BasicAuth()
			if !ok {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="pganalyzer"`)
				return c.JSON(http.StatusUnauthorized, errorResponse{
					Error: "missing credentials",
					Code:  "UNAUTHORIZED",
				})
			}

			// Validate credentials
			if username != authConfig.Username || password != authConfig.Password {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="pganalyzer"`)
				return c.JSON(http.StatusUnauthorized, errorResponse{
					Error: "invalid credentials",
					Code:  "UNAUTHORIZED",
				})
			}

			return next(c)
		}
	}
}

// RequireAuth is a middleware that requires authentication for specific routes.
// Use this when you want to selectively protect routes.
func RequireAuth(authConfig config.AuthConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth if disabled
			if !authConfig.Enabled {
				return next(c)
			}

			// Parse Authorization header
			username, password, ok := c.Request().BasicAuth()
			if !ok {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="pganalyzer"`)
				return c.JSON(http.StatusUnauthorized, errorResponse{
					Error: "missing credentials",
					Code:  "UNAUTHORIZED",
				})
			}

			// Validate credentials
			if username != authConfig.Username || password != authConfig.Password {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="pganalyzer"`)
				return c.JSON(http.StatusUnauthorized, errorResponse{
					Error: "invalid credentials",
					Code:  "UNAUTHORIZED",
				})
			}

			return next(c)
		}
	}
}
