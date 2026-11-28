package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/setup"
)

// SetupBanner summarises the setup checklist for the dashboard.
type SetupBanner struct {
	Failed  int
	Warned  int
	Pending int
	// Headline is the most important problem, or the first pending item.
	Headline string
}

// newSetupBanner returns nil when there is nothing to show.
func newSetupBanner(r *models.SetupReport) *SetupBanner {
	b := &SetupBanner{Failed: r.Count(models.SetupFail), Warned: r.Count(models.SetupWarn), Pending: r.Count(models.SetupPending)}
	for _, status := range []string{models.SetupFail, models.SetupWarn, models.SetupPending} {
		for _, c := range r.Checks {
			if c.Status == status && b.Headline == "" {
				b.Headline = c.Title + ": " + c.Detail
			}
		}
	}
	if b.Headline == "" {
		return nil
	}
	return b
}

// NeedsAttention reports whether something is broken rather than only
// waiting for history.
func (b *SetupBanner) NeedsAttention() bool {
	return b.Failed+b.Warned > 0
}

// SetupPageData contains data for the setup page.
type SetupPageData struct {
	BasePageData
	Report *models.SetupReport
}

// Setup handles GET /setup requests.
func (h *PageHandler) Setup(c echo.Context) error {
	ctx := c.Request().Context()
	data := SetupPageData{BasePageData: BasePageData{Title: "Setup", ActivePage: "setup", Version: h.version}}
	if snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID); err == nil && snapshot != nil {
		data.LastSnapshot = snapshot.CapturedAt
	}
	if h.setup != nil {
		// ?refresh=1 re-runs the checks after a fix instead of waiting for the cache.
		if c.QueryParam("refresh") != "" {
			data.Report = h.setup.Refresh(ctx)
		} else {
			data.Report = h.setup.Report(ctx)
		}
	}
	return c.Render(http.StatusOK, "setup", data)
}

// SetupAPIHandler handles GET /api/v1/setup requests.
type SetupAPIHandler struct {
	setup *setup.Checker
}

// NewSetupAPIHandler creates a new SetupAPIHandler.
func NewSetupAPIHandler(checker *setup.Checker) *SetupAPIHandler {
	return &SetupAPIHandler{setup: checker}
}

// GetSetup handles GET /api/v1/setup requests. Pass ?refresh=1 to bypass the cache.
func (h *SetupAPIHandler) GetSetup(c echo.Context) error {
	if h.setup == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "setup checker is not configured",
			"code":  "UNAVAILABLE",
		})
	}
	ctx := c.Request().Context()
	var report *models.SetupReport
	if c.QueryParam("refresh") != "" {
		report = h.setup.Refresh(ctx)
	} else {
		report = h.setup.Report(ctx)
	}
	return c.JSON(http.StatusOK, report)
}
