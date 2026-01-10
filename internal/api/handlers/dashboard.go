package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/models"
)

// DashboardStorage defines the storage interface needed by the dashboard handler.
type DashboardStorage interface {
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error)
}

// DashboardHandler handles dashboard API requests.
type DashboardHandler struct {
	storage    DashboardStorage
	instanceID int64
}

// DashboardResponse represents the dashboard API response.
type DashboardResponse struct {
	CacheHitRatio      *float64            `json:"cache_hit_ratio"`
	TotalQueries       int                 `json:"total_queries"`
	SlowQueriesCount   int                 `json:"slow_queries_count"`
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
			response.TotalQueries = len(stats)

			// Count slow queries (mean_exec_time > 1000ms)
			slowQueryThreshold := 1000.0 // 1 second
			for _, stat := range stats {
				if stat.MeanExecTime > slowQueryThreshold {
					response.SlowQueriesCount++
				}
			}

			// Top 5 queries by total time
			limit := 5
			if len(stats) < limit {
				limit = len(stats)
			}
			for i := 0; i < limit; i++ {
				stat := stats[i]
				response.TopQueries = append(response.TopQueries, TopQuerySummary{
					QueryID:         stat.QueryID,
					QueryPreview:    truncateQuery(stat.Query, 80),
					Calls:           stat.Calls,
					MeanExecTimeMs:  stat.MeanExecTime,
					TotalExecTimeMs: stat.TotalExecTime,
				})
			}
		}
	}

	// Get active suggestions
	suggestions, err := h.storage.GetActiveSuggestions(ctx, h.instanceID)
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
