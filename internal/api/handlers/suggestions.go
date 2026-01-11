package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/models"
)

// SuggestionsStorage defines the storage interface needed by the suggestions handler.
type SuggestionsStorage interface {
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
	GetSuggestionByID(ctx context.Context, id int64) (*models.Suggestion, error)
	DismissSuggestion(ctx context.Context, id int64) error
}

// SuggestionsHandler handles suggestion API requests.
type SuggestionsHandler struct {
	storage    SuggestionsStorage
	instanceID int64
}

// SuggestionResponse represents a suggestion in the API response.
type SuggestionResponse struct {
	ID           int64                  `json:"id"`
	RuleID       string                 `json:"rule_id"`
	Severity     string                 `json:"severity"`
	Title        string                 `json:"title"`
	Description  string                 `json:"description"`
	TargetObject string                 `json:"target_object"`
	FirstSeenAt  string                 `json:"first_seen_at"`
	LastSeenAt   string                 `json:"last_seen_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
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

	// Verify the suggestion exists
	sug, err := h.storage.GetSuggestionByID(ctx, id)
	if err != nil {
		c.Logger().Errorf("failed to get suggestion: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get suggestion",
			"code":  "DATABASE_ERROR",
		})
	}

	if sug == nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "suggestion not found",
			"code":  "NOT_FOUND",
		})
	}

	// Check if already dismissed
	if sug.Status == models.StatusDismissed {
		return c.JSON(http.StatusConflict, map[string]string{
			"error": "suggestion is already dismissed",
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

	dismissedAt := ""
	if sug.DismissedAt != nil {
		dismissedAt = sug.DismissedAt.Format("2006-01-02T15:04:05Z")
	}

	return c.JSON(http.StatusOK, DismissResponse{
		ID:          id,
		Status:      sug.Status,
		DismissedAt: dismissedAt,
	})
}

// suggestionToResponse converts a Suggestion model to SuggestionResponse.
func suggestionToResponse(sug models.Suggestion) SuggestionResponse {
	resp := SuggestionResponse{
		ID:           sug.ID,
		RuleID:       sug.RuleID,
		Severity:     sug.Severity,
		Title:        sug.Title,
		Description:  sug.Description,
		TargetObject: sug.TargetObject,
		FirstSeenAt:  sug.FirstSeenAt.Format("2006-01-02T15:04:05Z"),
		LastSeenAt:   sug.LastSeenAt.Format("2006-01-02T15:04:05Z"),
	}

	// Parse metadata JSON
	if sug.Metadata != "" {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(sug.Metadata), &metadata); err == nil {
			resp.Metadata = metadata
		}
	}

	return resp
}
