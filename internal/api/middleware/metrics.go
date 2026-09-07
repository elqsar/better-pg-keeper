package middleware

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/metrics"
)

// Metrics returns a middleware that records request counts and durations.
//
// The `path` label uses the matched route pattern (e.g. "/queries/:id") rather than
// the request URI, so a route with an id parameter contributes one time series
// instead of one per distinct id. Requests that match no route are labelled
// "unmatched" for the same reason.
func Metrics() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			// c.Path() is only populated with the route pattern after routing, so
			// it is read here rather than before the handler runs.
			path := c.Path()
			if path == "" {
				path = "unmatched"
			}
			method := c.Request().Method

			status := c.Response().Status
			if err != nil {
				// The error handler has not run yet, so derive the status the way
				// Echo will: an HTTPError carries its own code, anything else is a 500.
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				} else {
					status = 500
				}
			}

			metrics.HTTPRequestDuration.WithLabelValues(method, path).Observe(time.Since(start).Seconds())
			metrics.HTTPRequestsTotal.WithLabelValues(method, path, strconv.Itoa(status)).Inc()

			return err
		}
	}
}
