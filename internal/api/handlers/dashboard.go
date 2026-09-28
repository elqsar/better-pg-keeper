package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
)

// DashboardStorage defines the storage interface needed by the dashboard handler.
type DashboardStorage interface {
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	GetSuggestionsByStatus(ctx context.Context, instanceID int64, status string) ([]models.Suggestion, error)
}

// DashboardHandler handles dashboard API requests.
type DashboardHandler struct {
	storage    DashboardStorage
	instanceID int64
	queries    *QueryWindowConfig
}

// WithQueryWindow makes query figures cover the slow-query window and use the
// configured slow-query threshold.
func (h *DashboardHandler) WithQueryWindow(cfg QueryWindowConfig) *DashboardHandler {
	h.queries = &cfg
	return h
}

// DashboardResponse represents the dashboard API response.
type DashboardResponse struct {
	CacheHitRatio    *float64 `json:"cache_hit_ratio"`
	TotalQueries     int      `json:"total_queries"`
	SlowQueriesCount int      `json:"slow_queries_count"`
	// QueryWindowSeconds is the span the query figures cover; 0 means lifetime
	// statistics, before a slow-query window of history exists.
	QueryWindowSeconds float64             `json:"query_window_seconds"`
	SlowQueryMs        float64             `json:"slow_query_ms"`
	ActiveSuggestions  int                 `json:"active_suggestions"`
	TopQueries         []TopQuerySummary   `json:"top_queries"`
	RecentSuggestions  []SuggestionSummary `json:"recent_suggestions"`
}

// TopQuerySummary represents a summarized query for the dashboard.
type TopQuerySummary struct {
	QueryID         int64   `json:"queryid"`
	QueryPreview    string  `json:"query_preview"`
	Calls           int64   `json:"calls"`
	MeanExecTimeMs  float64 `json:"mean_exec_time_ms"`
	TotalExecTimeMs float64 `json:"total_exec_time_ms"`
}

// SuggestionSummary represents a summarized suggestion for the dashboard.
type SuggestionSummary struct {
	ID           int64     `json:"id"`
	Severity     string    `json:"severity"`
	Title        string    `json:"title"`
	TargetObject string    `json:"target_object"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(storage DashboardStorage, instanceID int64) *DashboardHandler {
	return &DashboardHandler{
		storage:    storage,
		instanceID: instanceID,
	}
}

// GetDashboard handles GET /api/v1/dashboard requests.
func (h *DashboardHandler) GetDashboard(c echo.Context) error {
	ctx := c.Request().Context()

	response := DashboardResponse{
		TopQueries:        []TopQuerySummary{},
		RecentSuggestions: []SuggestionSummary{},
	}

	// Get latest snapshot
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get snapshot data",
			"code":  "DATABASE_ERROR",
		})
	}

	if snapshot != nil {
		response.CacheHitRatio = snapshot.CacheHitRatio

		// Get query stats
		stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
		if err != nil {
			c.Logger().Errorf("failed to get query stats: %v", err)
		} else {
			summary, err := summarizeQueries(ctx, h.queries, h.instanceID, stats, 5)
			if err != nil {
				c.Logger().Errorf("failed to get recent query stats: %v", err)
			}
			response.TotalQueries = summary.Total
			response.SlowQueriesCount = summary.Slow
			response.QueryWindowSeconds = summary.Window.Seconds()
			for _, q := range summary.Top {
				response.TopQueries = append(response.TopQueries, TopQuerySummary{
					QueryID:         q.QueryID,
					QueryPreview:    truncateQuery(q.QueryPreview, 80),
					Calls:           q.Calls,
					MeanExecTimeMs:  q.MeanExecTimeMs,
					TotalExecTimeMs: q.TotalExecTimeMs,
				})
			}
		}
	}
	response.SlowQueryMs = defaultSlowQueryMs
	if h.queries != nil && h.queries.SlowQueryMs > 0 {
		response.SlowQueryMs = h.queries.SlowQueryMs
	}

	// Get active suggestions
	suggestions, err := h.storage.GetSuggestionsByStatus(ctx, h.instanceID, models.StatusActive)
	if err != nil {
		c.Logger().Errorf("failed to get suggestions: %v", err)
	} else {
		response.ActiveSuggestions = len(suggestions)

		// Recent 5 suggestions
		limit := 5
		if len(suggestions) < limit {
			limit = len(suggestions)
		}
		for i := 0; i < limit; i++ {
			sug := suggestions[i]
			response.RecentSuggestions = append(response.RecentSuggestions, SuggestionSummary{
				ID:           sug.ID,
				Severity:     sug.Severity,
				Title:        sug.Title,
				TargetObject: sug.TargetObject,
				FirstSeenAt:  sug.FirstSeenAt,
			})
		}
	}

	return c.JSON(http.StatusOK, response)
}

// truncateQuery truncates a query string to a maximum length.
func truncateQuery(query string, maxLen int) string {
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen-3] + "..."
}
