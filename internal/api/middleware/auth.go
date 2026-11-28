// Package middleware provides HTTP middleware for the API server.
package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/config"
)

// credentialsMatch compares username and password in constant time using
// SHA-256 digests so neither contents nor length leak via timing.
func credentialsMatch(givenUser, givenPass, wantUser, wantPass string) bool {
	u1 := sha256.Sum256([]byte(givenUser))
	u2 := sha256.Sum256([]byte(wantUser))
	p1 := sha256.Sum256([]byte(givenPass))
	p2 := sha256.Sum256([]byte(wantPass))
	userOK := subtle.ConstantTimeCompare(u1[:], u2[:])
	passOK := subtle.ConstantTimeCompare(p1[:], p2[:])
	return (userOK & passOK) == 1
}

// errorResponse represents a standardized error response.
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// BasicAuth creates a Basic Authentication middleware.
// It validates credentials against the provided config.
// The /health endpoint is always excluded from authentication, along with any
// extra skipPaths (defaulting to /metrics when none are provided).
func BasicAuth(authConfig config.AuthConfig, skipPaths ...string) echo.MiddlewareFunc {
	skip := map[string]bool{"/health": true}
	if len(skipPaths) == 0 {
		skip["/metrics"] = true
	} else {
		for _, p := range skipPaths {
			if p != "" {
				skip[p] = true
			}
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth if disabled
			if !authConfig.Enabled {
				return next(c)
			}

			// Skip auth for public endpoints (health, configured metrics path)
			if skip[c.Path()] {
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

			// Validate credentials in constant time
			if !credentialsMatch(username, password, authConfig.Username, authConfig.Password) {
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

			// Validate credentials in constant time
			if !credentialsMatch(username, password, authConfig.Username, authConfig.Password) {
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
