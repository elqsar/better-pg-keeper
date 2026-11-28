package middleware

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// CrossOriginProtection rejects cross-origin unsafe browser requests
// (POST/PUT/DELETE/PATCH) using Go's stdlib Sec-Fetch-Site and Origin checks
// while allowing same-origin requests and non-browser API clients.
func CrossOriginProtection() echo.MiddlewareFunc {
	cop := http.NewCrossOriginProtection()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if err := cop.Check(c.Request()); err != nil {
				return c.JSON(http.StatusForbidden, errorResponse{
					Error: "cross-origin request blocked",
					Code:  "FORBIDDEN",
				})
			}
			return next(c)
		}
	}
}
