package handlers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
)

// ActivityStorage defines the storage interface needed by the activity API handler.
type ActivityStorage interface {
	GetCurrentConnectionActivity(ctx context.Context, instanceID int64) (*models.ConnectionActivity, error)
	GetCurrentLongRunningQueries(ctx context.Context, instanceID int64) ([]models.LongRunningQuery, error)
	GetCurrentIdleInTransaction(ctx context.Context, instanceID int64) ([]models.IdleInTransaction, error)
	GetCurrentBlockedQueries(ctx context.Context, instanceID int64) ([]models.BlockedQuery, error)
	GetCurrentLockStats(ctx context.Context, instanceID int64) (*models.LockStats, error)
}

// ActivityHandler handles activity API requests.
type ActivityHandler struct {
	storage    ActivityStorage
	instanceID int64
}

// ActivityResponse represents current database activity, locks, and blocking chains.
type ActivityResponse struct {
	Connections        *models.ConnectionActivity `json:"connections"`
	LongRunningQueries []models.LongRunningQuery  `json:"long_running_queries"`
	IdleInTransaction  []models.IdleInTransaction `json:"idle_in_transaction"`
	BlockedQueries     []models.BlockedQuery      `json:"blocked_queries"`
	LockStats          *models.LockStats          `json:"lock_stats"`
}

// NewActivityHandler creates a new ActivityHandler.
func NewActivityHandler(storage ActivityStorage, instanceID int64) *ActivityHandler {
	return &ActivityHandler{
		storage:    storage,
		instanceID: instanceID,
	}
}

// GetActivity handles GET /api/v1/activity requests.
func (h *ActivityHandler) GetActivity(c echo.Context) error {
	ctx := c.Request().Context()

	connections, err := h.storage.GetCurrentConnectionActivity(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get current connection activity: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get connection activity",
			"code":  "DATABASE_ERROR",
		})
	}

	longRunning, err := h.storage.GetCurrentLongRunningQueries(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get current long-running queries: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get long-running queries",
			"code":  "DATABASE_ERROR",
		})
	}
	if longRunning == nil {
		longRunning = []models.LongRunningQuery{}
	}

	idleInTx, err := h.storage.GetCurrentIdleInTransaction(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get current idle-in-transaction sessions: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get idle-in-transaction sessions",
			"code":  "DATABASE_ERROR",
		})
	}
	if idleInTx == nil {
		idleInTx = []models.IdleInTransaction{}
	}

	blocked, err := h.storage.GetCurrentBlockedQueries(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get current blocked queries: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get blocked queries",
			"code":  "DATABASE_ERROR",
		})
	}
	if blocked == nil {
		blocked = []models.BlockedQuery{}
	}

	lockStats, err := h.storage.GetCurrentLockStats(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get current lock stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get lock stats",
			"code":  "DATABASE_ERROR",
		})
	}

	return c.JSON(http.StatusOK, ActivityResponse{
		Connections:        connections,
		LongRunningQueries: longRunning,
		IdleInTransaction:  idleInTx,
		BlockedQueries:     blocked,
		LockStats:          lockStats,
	})
}
