// Package api provides HTTP API server functionality for pganalyzer.
package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// ErrorResponse represents a standardized error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// Error codes for API errors.
const (
	ErrCodeBadRequest     = "BAD_REQUEST"
	ErrCodeUnauthorized   = "UNAUTHORIZED"
	ErrCodeNotFound       = "NOT_FOUND"
	ErrCodeInternalError  = "INTERNAL_ERROR"
	ErrCodeConflict       = "CONFLICT"
	ErrCodeValidation     = "VALIDATION_ERROR"
	ErrCodeDatabaseError  = "DATABASE_ERROR"
	ErrCodeCollectionBusy = "COLLECTION_BUSY"
)

// NewErrorResponse creates a new error response.
func NewErrorResponse(message, code string) *ErrorResponse {
	return &ErrorResponse{
		Error: message,
		Code:  code,
	}
}

// NewErrorResponseWithDetails creates a new error response with additional details.
func NewErrorResponseWithDetails(message, code, details string) *ErrorResponse {
	return &ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	}
}

// HTTPError sends an error response with the specified status code.
func HTTPError(c echo.Context, status int, message, code string) error {
	return c.JSON(status, NewErrorResponse(message, code))
}

// BadRequest sends a 400 Bad Request error.
func BadRequest(c echo.Context, message string) error {
	return HTTPError(c, http.StatusBadRequest, message, ErrCodeBadRequest)
}

// ValidationError sends a 400 Bad Request error for validation failures.
func ValidationError(c echo.Context, message string) error {
	return HTTPError(c, http.StatusBadRequest, message, ErrCodeValidation)
}

// Unauthorized sends a 401 Unauthorized error.
func Unauthorized(c echo.Context, message string) error {
	return HTTPError(c, http.StatusUnauthorized, message, ErrCodeUnauthorized)
}

// NotFound sends a 404 Not Found error.
func NotFound(c echo.Context, message string) error {
	return HTTPError(c, http.StatusNotFound, message, ErrCodeNotFound)
}

// Conflict sends a 409 Conflict error.
func Conflict(c echo.Context, message string) error {
	return HTTPError(c, http.StatusConflict, message, ErrCodeConflict)
}

// InternalError sends a 500 Internal Server Error.
func InternalError(c echo.Context, message string) error {
	return HTTPError(c, http.StatusInternalServerError, message, ErrCodeInternalError)
}

// DatabaseError sends a 500 Internal Server Error for database failures.
func DatabaseError(c echo.Context, message string) error {
	return HTTPError(c, http.StatusInternalServerError, message, ErrCodeDatabaseError)
}

// CollectionBusy sends a 409 Conflict error for busy collection.
func CollectionBusy(c echo.Context) error {
	return HTTPError(c, http.StatusConflict, "collection already in progress", ErrCodeCollectionBusy)
}

// CustomHTTPErrorHandler handles Echo errors and converts them to our error format.
func CustomHTTPErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	var (
		code    = http.StatusInternalServerError
		message = "internal server error"
		errCode = ErrCodeInternalError
	)

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			message = m
		} else {
			message = http.StatusText(code)
		}
		switch code {
		case http.StatusBadRequest:
			errCode = ErrCodeBadRequest
		case http.StatusUnauthorized:
			errCode = ErrCodeUnauthorized
		case http.StatusNotFound:
			errCode = ErrCodeNotFound
		case http.StatusConflict:
			errCode = ErrCodeConflict
		}
	}

	c.Logger().Error(err)
	c.JSON(code, NewErrorResponse(message, errCode))
}
