package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/models"
	"github.com/user/pganalyzer/internal/scheduler"
)

// SnapshotsStorage defines the storage interface needed by the snapshots handler.
type SnapshotsStorage interface {
	ListSnapshots(ctx context.Context, instanceID int64, limit int) ([]models.Snapshot, error)
}

// SnapshotsHandler handles snapshot API requests.
type SnapshotsHandler struct {
	storage    SnapshotsStorage
	scheduler  *scheduler.Scheduler
	instanceID int64
}

// SnapshotResponse represents a snapshot in the API response.
type SnapshotResponse struct {
	ID            int64    `json:"id"`
	CapturedAt    string   `json:"captured_at"`
	PGVersion     string   `json:"pg_version"`
	CacheHitRatio *float64 `json:"cache_hit_ratio,omitempty"`
	StatsReset    *string  `json:"stats_reset,omitempty"`
}

// SnapshotsListResponse represents the snapshots list response.
type SnapshotsListResponse struct {
	Snapshots []SnapshotResponse `json:"snapshots"`
	Total     int                `json:"total"`
}

// TriggerSnapshotResponse represents the trigger snapshot response.
type TriggerSnapshotResponse struct {
	SnapshotID int64  `json:"snapshot_id"`
	Status     string `json:"status"`
	Duration   string `json:"duration"`
	Message    string `json:"message,omitempty"`
}

// NewSnapshotsHandler creates a new SnapshotsHandler.
func NewSnapshotsHandler(storage SnapshotsStorage, sched *scheduler.Scheduler, instanceID int64) *SnapshotsHandler {
	return &SnapshotsHandler{
		storage:    storage,
		scheduler:  sched,
		instanceID: instanceID,
	}
}

// ListSnapshots handles GET /api/v1/snapshots requests.
// Query params: limit (default 20, max 100)
func (h *SnapshotsHandler) ListSnapshots(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse limit parameter
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// Get snapshots
	snapshots, err := h.storage.ListSnapshots(ctx, h.instanceID, limit)
	if err != nil {
		c.Logger().Errorf("failed to list snapshots: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to list snapshots",
			"code":  "DATABASE_ERROR",
		})
	}

	// Convert to response format
	responses := make([]SnapshotResponse, len(snapshots))
	for i, snap := range snapshots {
		responses[i] = snapshotToResponse(snap)
	}

	return c.JSON(http.StatusOK, SnapshotsListResponse{
		Snapshots: responses,
		Total:     len(responses),
	})
}

// TriggerSnapshot handles POST /api/v1/snapshots requests.
func (h *SnapshotsHandler) TriggerSnapshot(c echo.Context) error {
	ctx := c.Request().Context()

	// Trigger manual snapshot collection
	result, err := h.scheduler.TriggerSnapshot(ctx)
	if err != nil {
		// Check if it's a "busy" error
		if err.Error() == "manual trigger already in progress" {
			return c.JSON(http.StatusConflict, map[string]string{
				"error": "collection already in progress",
				"code":  "COLLECTION_BUSY",
			})
		}
		c.Logger().Errorf("failed to trigger snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to trigger snapshot",
			"code":  "INTERNAL_ERROR",
		})
	}

	response := TriggerSnapshotResponse{
		Duration: result.Duration.String(),
	}

	// Determine status based on results
	if result.CollectionError != "" {
		response.Status = "error"
		response.Message = result.CollectionError
	} else if result.CollectionResult != nil && result.CollectionResult.HasErrors() {
		response.Status = "partial"
		response.SnapshotID = result.CollectionResult.SnapshotID
		response.Message = "some collectors failed"
	} else if result.CollectionResult != nil {
		response.Status = "success"
		response.SnapshotID = result.CollectionResult.SnapshotID
	} else {
		response.Status = "unknown"
	}

	return c.JSON(http.StatusOK, response)
}

// snapshotToResponse converts a Snapshot model to SnapshotResponse.
func snapshotToResponse(snap models.Snapshot) SnapshotResponse {
	resp := SnapshotResponse{
		ID:            snap.ID,
		CapturedAt:    snap.CapturedAt.Format("2006-01-02T15:04:05Z"),
		PGVersion:     snap.PGVersion,
		CacheHitRatio: snap.CacheHitRatio,
	}

	if snap.StatsReset != nil {
		t := snap.StatsReset.Format("2006-01-02T15:04:05Z")
		resp.StatsReset = &t
	}

	return resp
}
