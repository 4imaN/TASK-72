package common

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// AppError is the standard error envelope returned by all API endpoints.
type AppError struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Fields  map[string][]string `json:"fields,omitempty"`
	TraceID string              `json:"trace_id,omitempty"`
}

// ErrorResponse writes a JSON error response with the given HTTP status.
func ErrorResponse(c echo.Context, status int, code, message string) error {
	traceID := ""
	if v := c.Get("trace_id"); v != nil {
		traceID = v.(string)
	}
	return c.JSON(status, AppError{
		Code:    code,
		Message: message,
		TraceID: traceID,
	})
}

// Unauthorized returns a 401 response.
func Unauthorized(c echo.Context, msg string) error {
	return ErrorResponse(c, http.StatusUnauthorized, "auth.unauthenticated", msg)
}

// Forbidden returns a 403 response.
func Forbidden(c echo.Context, msg string) error {
	return ErrorResponse(c, http.StatusForbidden, "auth.forbidden", msg)
}

// BadRequest returns a 400 response.
func BadRequest(c echo.Context, code, msg string) error {
	return ErrorResponse(c, http.StatusBadRequest, code, msg)
}

// Internal returns a 500 response with a generic message.
func Internal(c echo.Context) error {
	return ErrorResponse(c, http.StatusInternalServerError, "internal", "An internal error occurred")
}
