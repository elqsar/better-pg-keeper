package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
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
