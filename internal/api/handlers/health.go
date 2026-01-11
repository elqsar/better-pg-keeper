// Package handlers provides HTTP handlers for the API endpoints.
package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/scheduler"
)

// HealthStorage defines the storage interface needed by the health handler.
type HealthStorage interface {
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
}

// HealthHandler handles health check requests.
type HealthHandler struct {
	storage    HealthStorage
	pgClient   postgres.Client
	scheduler  *scheduler.Scheduler
	instanceID int64
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status       string     `json:"status"`
	PGConnected  bool       `json:"pg_connected"`
	LastSnapshot *time.Time `json:"last_snapshot,omitempty"`
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(storage HealthStorage, pgClient postgres.Client, sched *scheduler.Scheduler, instanceID int64) *HealthHandler {
	return &HealthHandler{
		storage:    storage,
		pgClient:   pgClient,
		scheduler:  sched,
		instanceID: instanceID,
	}
}

// GetHealth handles GET /health requests.
func (h *HealthHandler) GetHealth(c echo.Context) error {
	ctx := c.Request().Context()

	// Check PostgreSQL connectivity
	pgConnected := false
	if err := h.pgClient.Ping(ctx); err == nil {
		pgConnected = true
	}

	// Get last snapshot time
	var lastSnapshot *time.Time
	if snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID); err == nil && snapshot != nil {
		lastSnapshot = &snapshot.CapturedAt
	}

	status := "ok"
	if !pgConnected {
		status = "degraded"
	}

	return c.JSON(http.StatusOK, HealthResponse{
		Status:       status,
		PGConnected:  pgConnected,
		LastSnapshot: lastSnapshot,
	})
}
