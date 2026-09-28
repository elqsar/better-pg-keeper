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
	"github.com/elqsar/pganalyzer/internal/setup"
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
	setup      *setup.Checker
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status       string     `json:"status"`
	PGConnected  bool       `json:"pg_connected"`
	LastSnapshot *time.Time `json:"last_snapshot,omitempty"`
	// Setup counts setup problems; see /setup. It does not affect Status, so
	// uptime monitors keep meaning "PGAnalyzer is running".
	Setup *SetupSummary `json:"setup,omitempty"`
}

// SetupSummary counts setup checks by outcome.
type SetupSummary struct {
	Failed  int `json:"failed"`
	Warned  int `json:"warned"`
	Pending int `json:"pending"`
}

// WithSetup adds the setup summary to health responses.
func (h *HealthHandler) WithSetup(c *setup.Checker) *HealthHandler {
	h.setup = c
	return h
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

	resp := HealthResponse{
		Status:       status,
		PGConnected:  pgConnected,
		LastSnapshot: lastSnapshot,
	}
	if h.setup != nil {
		r := h.setup.Report(ctx)
		resp.Setup = &SetupSummary{
			Failed:  r.Count(models.SetupFail),
			Warned:  r.Count(models.SetupWarn),
			Pending: r.Count(models.SetupPending),
		}
	}
	return c.JSON(http.StatusOK, resp)
}
