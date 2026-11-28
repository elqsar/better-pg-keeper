package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/verify"
)

// SuggestionsStorage defines the storage interface needed by the suggestions handler.
type SuggestionsStorage interface {
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
	GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error)
	DismissSuggestion(ctx context.Context, id int64) error
	UndismissSuggestion(ctx context.Context, id int64) error
}

// SuggestionsHandler handles suggestion API requests.
type SuggestionsHandler struct {
	storage    SuggestionsStorage
	instanceID int64
	verifier   *verify.Verifier
}

// WithVerifier enables fix verification on GET /api/v1/suggestions/:id for resolved suggestions.
func (h *SuggestionsHandler) WithVerifier(v *verify.Verifier) *SuggestionsHandler {
	h.verifier = v
	return h
}

// SuggestionResponse represents a suggestion in the API response.
type SuggestionResponse struct {
	ID           int64  `json:"id"`
	RuleID       string `json:"rule_id"`
	Severity     string `json:"severity"`
	Status       string `json:"status"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	TargetObject string `json:"target_object"`
	FirstSeenAt  string `json:"first_seen_at"`
	LastSeenAt   string `json:"last_seen_at"`
	DismissedAt  string `json:"dismissed_at,omitempty"`
	ResolvedAt   string `json:"resolved_at,omitempty"`
	// Metadata is passed through as stored. Decoding it into a map would turn
	// every number into a float64 and corrupt 64-bit query ids.
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// SuggestionDetailResponse represents a single suggestion with optional fix verification.
type SuggestionDetailResponse struct {
	SuggestionResponse
	Verification *verify.Result `json:"verification,omitempty"`
}

// SuggestionsListResponse represents the suggestions list response.
type SuggestionsListResponse struct {
	Suggestions []SuggestionResponse `json:"suggestions"`
	Total       int                  `json:"total"`
}

// DismissResponse represents the dismiss response.
type DismissResponse struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	DismissedAt string `json:"dismissed_at"`
}

// UndismissResponse represents the undismiss response.
type UndismissResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// NewSuggestionsHandler creates a new SuggestionsHandler.
func NewSuggestionsHandler(storage SuggestionsStorage, instanceID int64) *SuggestionsHandler {
	return &SuggestionsHandler{
		storage:    storage,
		instanceID: instanceID,
	}
}

// ListSuggestions handles GET /api/v1/suggestions requests.
// Query params: status (active, dismissed), severity (critical, warning, info)
func (h *SuggestionsHandler) ListSuggestions(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse query parameters
	statusFilter := c.QueryParam("status")
	if statusFilter == "" {
		statusFilter = models.StatusActive
	}
	severityFilter := c.QueryParam("severity")

	// Get suggestions by status
	suggestions, err := h.storage.GetSuggestionsByStatus(ctx, h.instanceID, statusFilter)
	if err != nil {
		c.Logger().Errorf("failed to get suggestions: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get suggestions",
			"code":  "DATABASE_ERROR",
		})
	}

	// Filter by severity
	if severityFilter != "" {
		filtered := make([]models.Suggestion, 0)
		for _, sug := range suggestions {
			if sug.Severity == severityFilter {
				filtered = append(filtered, sug)
			}
		}
		suggestions = filtered
	}

	// Convert to response format
	responses := make([]SuggestionResponse, len(suggestions))
	for i, sug := range suggestions {
		responses[i] = suggestionToResponse(sug)
	}

	return c.JSON(http.StatusOK, SuggestionsListResponse{
		Suggestions: responses,
		Total:       len(responses),
	})
}

// GetSuggestion handles GET /api/v1/suggestions/:id requests.
func (h *SuggestionsHandler) GetSuggestion(c echo.Context) error {
	ctx := c.Request().Context()

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid suggestion ID",
			"code":  "VALIDATION_ERROR",
		})
	}

	sug, err := h.storage.GetSuggestionByID(ctx, id)
	if err != nil {
		c.Logger().Errorf("failed to get suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get suggestion",
			"code":  "DATABASE_ERROR",
		})
	}
	if sug == nil || (sug.InstanceID != 0 && sug.InstanceID != h.instanceID) {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "suggestion not found",
			"code":  "NOT_FOUND",
		})
	}

	resp := SuggestionDetailResponse{
		SuggestionResponse: suggestionToResponse(*sug),
	}
	if h.verifier != nil && sug.Status == models.StatusResolved {
		v, err := h.verifier.Verify(ctx, *sug, time.Now())
		if err != nil {
			c.Logger().Errorf("failed to verify suggestion %d: %v", sug.ID, err)
		} else {
			resp.Verification = v
		}
	}

	return c.JSON(http.StatusOK, resp)
}

// DismissSuggestion handles POST /api/v1/suggestions/:id/dismiss requests.
func (h *SuggestionsHandler) DismissSuggestion(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse suggestion ID
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid suggestion ID",
			"code":  "VALIDATION_ERROR",
		})
	}

	// Verify the suggestion exists and belongs to this instance
	sug, err := h.storage.GetSuggestionByID(ctx, id)
	if err != nil {
		c.Logger().Errorf("failed to get suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get suggestion",
			"code":  "DATABASE_ERROR",
		})
	}

	if sug == nil || (sug.InstanceID != 0 && sug.InstanceID != h.instanceID) {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "suggestion not found",
			"code":  "NOT_FOUND",
		})
	}

	// Check current status
	if sug.Status == models.StatusDismissed {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "suggestion is already dismissed",
			"code":  "CONFLICT",
		})
	}
	if sug.Status == models.StatusResolved {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "cannot dismiss a resolved suggestion",
			"code":  "CONFLICT",
		})
	}

	// Dismiss the suggestion
	if err := h.storage.DismissSuggestion(ctx, id); err != nil {
		c.Logger().Errorf("failed to dismiss suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to dismiss suggestion",
			"code":  "DATABASE_ERROR",
		})
	}

	// Get the updated suggestion
	sug, err = h.storage.GetSuggestionByID(ctx, id)
	if err != nil {
		c.Logger().Errorf("failed to get updated suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "suggestion dismissed but failed to fetch updated state",
			"code":  "DATABASE_ERROR",
		})
	}

	status := models.StatusDismissed
	dismissedAt := ""
	if sug != nil {
		status = sug.Status
		if sug.DismissedAt != nil {
			dismissedAt = sug.DismissedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
	}

	return c.JSON(http.StatusOK, DismissResponse{
		ID:          id,
		Status:      status,
		DismissedAt: dismissedAt,
	})
}

// UndismissSuggestion handles POST /api/v1/suggestions/:id/undismiss requests.
func (h *SuggestionsHandler) UndismissSuggestion(c echo.Context) error {
	ctx := c.Request().Context()

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid suggestion ID",
			"code":  "VALIDATION_ERROR",
		})
	}

	sug, err := h.storage.GetSuggestionByID(ctx, id)
	if err != nil {
		c.Logger().Errorf("failed to get suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get suggestion",
			"code":  "DATABASE_ERROR",
		})
	}
	if sug == nil || (sug.InstanceID != 0 && sug.InstanceID != h.instanceID) {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "suggestion not found",
			"code":  "NOT_FOUND",
		})
	}

	if sug.Status != models.StatusDismissed {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "suggestion is not dismissed",
			"code":  "CONFLICT",
		})
	}

	if err := h.storage.UndismissSuggestion(ctx, id); err != nil {
		c.Logger().Errorf("failed to undismiss suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to undismiss suggestion",
			"code":  "DATABASE_ERROR",
		})
	}

	return c.JSON(http.StatusOK, UndismissResponse{
		ID:     id,
		Status: models.StatusActive,
	})
}

// suggestionToResponse converts a Suggestion model to SuggestionResponse.
func suggestionToResponse(sug models.Suggestion) SuggestionResponse {
	resp := SuggestionResponse{
		ID:           sug.ID,
		RuleID:       sug.RuleID,
		Severity:     sug.Severity,
		Status:       sug.Status,
		Title:        sug.Title,
		Description:  sug.Description,
		TargetObject: sug.TargetObject,
		FirstSeenAt:  sug.FirstSeenAt.UTC().Format("2006-01-02T15:04:05Z"),
		LastSeenAt:   sug.LastSeenAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if sug.DismissedAt != nil {
		resp.DismissedAt = sug.DismissedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if sug.ResolvedAt != nil {
		resp.ResolvedAt = sug.ResolvedAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	if sug.Metadata != "" && json.Valid([]byte(sug.Metadata)) {
		resp.Metadata = json.RawMessage(sug.Metadata)
	}

	return resp
}
