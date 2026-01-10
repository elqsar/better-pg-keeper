package handlers

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/models"
)

// PageStorage defines the storage interface needed by page handlers.
type PageStorage interface {
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	GetActiveSuggestions(ctx context.Context, instanceID int64) ([]models.Suggestion, error)
	GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error)
	GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error)
	GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error)
	GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error)
}

// PageHandler handles HTML page rendering.
type PageHandler struct {
	storage    PageStorage
	instanceID int64
	version    string
}

// NewPageHandler creates a new PageHandler.
func NewPageHandler(storage PageStorage, instanceID int64, version string) *PageHandler {
	return &PageHandler{
		storage:    storage,
		instanceID: instanceID,
		version:    version,
	}
}

// BasePageData contains common data for all pages.
type BasePageData struct {
	Title        string
	ActivePage   string
	Version      string
	LastSnapshot time.Time
}

// DashboardPageData contains data for the dashboard page.
type DashboardPageData struct {
	BasePageData
	CacheHitRatio     float64
	TotalQueries      int64
	SlowQueriesCount  int
	ActiveSuggestions int
	TopQueries        []DashboardQuery
	RecentSuggestions []DashboardSuggestion
}

// DashboardQuery represents a query summary for the dashboard.
type DashboardQuery struct {
	QueryID         int64
	QueryPreview    string
	Calls           int64
	MeanExecTimeMs  float64
	TotalExecTimeMs float64
}

// DashboardSuggestion represents a suggestion summary for the dashboard.
type DashboardSuggestion struct {
	ID           int64
	Severity     string
	Title        string
	TargetObject string
	FirstSeenAt  time.Time
}

// Dashboard handles GET / requests.
func (h *PageHandler) Dashboard(c echo.Context) error {
	ctx := c.Request().Context()

	data := DashboardPageData{
		BasePageData: BasePageData{
			Title:      "Dashboard",
			ActivePage: "dashboard",
			Version:    h.version,
		},
		TopQueries:        []DashboardQuery{},
		RecentSuggestions: []DashboardSuggestion{},
	}

	// Get latest snapshot
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
	}

	if snapshot != nil {
		data.LastSnapshot = snapshot.CapturedAt
		if snapshot.CacheHitRatio != nil {
			data.CacheHitRatio = *snapshot.CacheHitRatio
		}

		// Get query stats
		stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
		if err != nil {
			c.Logger().Errorf("failed to get query stats: %v", err)
		} else {
			data.TotalQueries = int64(len(stats))

			// Count slow queries (mean_exec_time > 1000ms)
			for _, stat := range stats {
				if stat.MeanExecTime > 1000 {
					data.SlowQueriesCount++
				}
			}

			// Sort by total time and get top 5
			sort.Slice(stats, func(i, j int) bool {
				return stats[i].TotalExecTime > stats[j].TotalExecTime
			})
			limit := 5
			if len(stats) < limit {
				limit = len(stats)
			}
			for i := 0; i < limit; i++ {
				stat := stats[i]
				data.TopQueries = append(data.TopQueries, DashboardQuery{
					QueryID:         stat.QueryID,
					QueryPreview:    truncateString(stat.Query, 80),
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
		data.ActiveSuggestions = len(suggestions)

		// Recent 5 suggestions
		limit := 5
		if len(suggestions) < limit {
			limit = len(suggestions)
		}
		for i := 0; i < limit; i++ {
			sug := suggestions[i]
			data.RecentSuggestions = append(data.RecentSuggestions, DashboardSuggestion{
				ID:           sug.ID,
				Severity:     sug.Severity,
				Title:        sug.Title,
				TargetObject: sug.TargetObject,
				FirstSeenAt:  sug.FirstSeenAt,
			})
		}
	}

	return c.Render(http.StatusOK, "dashboard", data)
}

// QueriesPageData contains data for the queries page.
type QueriesPageData struct {
	BasePageData
	Queries     []PageQuery
	Sort        string
	Order       string
	CurrentPage int
	TotalPages  int
	Total       int
}

// PageQuery represents a query for the page.
type PageQuery struct {
	QueryID       int64
	Query         string
	Calls         int64
	MeanExecTime  float64
	TotalExecTime float64
	RowsPerCall   float64
	CacheHitRatio float64
}

// Queries handles GET /queries requests.
func (h *PageHandler) Queries(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse query parameters
	sortField := c.QueryParam("sort")
	if sortField == "" {
		sortField = "total_time"
	}
	order := c.QueryParam("order")
	if order == "" {
		order = "desc"
	}
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	const perPage = 20

	data := QueriesPageData{
		BasePageData: BasePageData{
			Title:      "Queries",
			ActivePage: "queries",
			Version:    h.version,
		},
		Queries:     []PageQuery{},
		Sort:        sortField,
		Order:       order,
		CurrentPage: page,
	}

	// Get latest snapshot
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
		return c.Render(http.StatusOK, "queries", data)
	}

	if snapshot != nil {
		data.LastSnapshot = snapshot.CapturedAt

		// Get query stats
		stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
		if err != nil {
			c.Logger().Errorf("failed to get query stats: %v", err)
			return c.Render(http.StatusOK, "queries", data)
		}

		// Sort stats
		sortQueryStats(stats, sortField, order == "desc")

		// Calculate pagination
		data.Total = len(stats)
		data.TotalPages = (data.Total + perPage - 1) / perPage
		if data.TotalPages < 1 {
			data.TotalPages = 1
		}

		// Apply pagination
		offset := (page - 1) * perPage
		if offset >= len(stats) {
			offset = 0
			data.CurrentPage = 1
		}
		end := offset + perPage
		if end > len(stats) {
			end = len(stats)
		}

		// Convert to page format
		for _, stat := range stats[offset:end] {
			data.Queries = append(data.Queries, PageQuery{
				QueryID:       stat.QueryID,
				Query:         stat.Query,
				Calls:         stat.Calls,
				MeanExecTime:  stat.MeanExecTime,
				TotalExecTime: stat.TotalExecTime,
				RowsPerCall:   calculateRowsPerCall(stat.Rows, stat.Calls),
				CacheHitRatio: calculateCacheHitRatio(stat.SharedBlksHit, stat.SharedBlksRead),
			})
		}
	}

	return c.Render(http.StatusOK, "queries", data)
}

// QueryDetailPageData contains data for the query detail page.
type QueryDetailPageData struct {
	BasePageData
	Query       *PageQueryDetail
	ExplainPlan *models.ExplainPlan
}

// PageQueryDetail represents detailed query info for the page.
type PageQueryDetail struct {
	QueryID       int64
	Query         string
	Calls         int64
	MeanExecTime  float64
	MinExecTime   float64
	MaxExecTime   float64
	TotalExecTime float64
	Rows          int64
	RowsPerCall   float64
	SharedBlksHit int64
	SharedBlksRead int64
	CacheHitRatio float64
	Plans         int64
	TotalPlanTime float64
}

// QueryDetail handles GET /queries/:id requests.
func (h *PageHandler) QueryDetail(c echo.Context) error {
	ctx := c.Request().Context()

	queryID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.Redirect(http.StatusFound, "/queries")
	}

	data := QueryDetailPageData{
		BasePageData: BasePageData{
			Title:      "Query Details",
			ActivePage: "queries",
			Version:    h.version,
		},
	}

	// Get latest snapshot
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
		return c.Redirect(http.StatusFound, "/queries")
	}

	if snapshot == nil {
		return c.Redirect(http.StatusFound, "/queries")
	}

	data.LastSnapshot = snapshot.CapturedAt

	// Get query stats
	stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get query stats: %v", err)
		return c.Redirect(http.StatusFound, "/queries")
	}

	// Find the query
	for _, stat := range stats {
		if stat.QueryID == queryID {
			data.Query = &PageQueryDetail{
				QueryID:        stat.QueryID,
				Query:          stat.Query,
				Calls:          stat.Calls,
				MeanExecTime:   stat.MeanExecTime,
				MinExecTime:    stat.MinExecTime,
				MaxExecTime:    stat.MaxExecTime,
				TotalExecTime:  stat.TotalExecTime,
				Rows:           stat.Rows,
				RowsPerCall:    calculateRowsPerCall(stat.Rows, stat.Calls),
				SharedBlksHit:  stat.SharedBlksHit,
				SharedBlksRead: stat.SharedBlksRead,
				CacheHitRatio:  calculateCacheHitRatio(stat.SharedBlksHit, stat.SharedBlksRead),
				Plans:          stat.Plans,
				TotalPlanTime:  stat.TotalPlanTime,
			}
			break
		}
	}

	if data.Query == nil {
		return c.Redirect(http.StatusFound, "/queries")
	}

	// Get EXPLAIN plan if available
	plan, err := h.storage.GetExplainPlan(ctx, queryID)
	if err != nil {
		c.Logger().Errorf("failed to get explain plan: %v", err)
	}
	data.ExplainPlan = plan

	return c.Render(http.StatusOK, "query_detail", data)
}

// SchemaPageData contains data for the schema page.
type SchemaPageData struct {
	BasePageData
	ActiveTab string
	Tables    []PageTable
	Indexes   []PageIndex
	Bloat     []PageBloat
}

// PageTable represents a table for the schema page.
type PageTable struct {
	SchemaName     string
	RelName        string
	TableSize      int64
	NLiveTup       int64
	NDeadTup       int64
	SeqScan        int64
	IdxScan        int64
	LastVacuum     *time.Time
	LastAutovacuum *time.Time
	LastAnalyze    *time.Time
}

// PageIndex represents an index for the schema page.
type PageIndex struct {
	SchemaName   string
	RelName      string
	IndexRelName string
	IdxScan      int64
	IndexSize    int64
	IsUnique     bool
	IsPrimary    bool
}

// PageBloat represents table bloat info for the schema page.
type PageBloat struct {
	SchemaName   string
	RelName      string
	NLiveTup     int64
	NDeadTup     int64
	BloatPercent float64
}

// Schema handles GET /schema requests.
func (h *PageHandler) Schema(c echo.Context) error {
	ctx := c.Request().Context()

	tab := c.QueryParam("tab")
	if tab == "" {
		tab = "tables"
	}

	data := SchemaPageData{
		BasePageData: BasePageData{
			Title:      "Schema",
			ActivePage: "schema",
			Version:    h.version,
		},
		ActiveTab: tab,
		Tables:    []PageTable{},
		Indexes:   []PageIndex{},
		Bloat:     []PageBloat{},
	}

	// Get latest snapshot
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
		return c.Render(http.StatusOK, "schema", data)
	}

	if snapshot == nil {
		return c.Render(http.StatusOK, "schema", data)
	}

	data.LastSnapshot = snapshot.CapturedAt

	switch tab {
	case "tables":
		tables, err := h.storage.GetTableStats(ctx, snapshot.ID)
		if err != nil {
			c.Logger().Errorf("failed to get table stats: %v", err)
		} else {
			for _, t := range tables {
				data.Tables = append(data.Tables, PageTable{
					SchemaName:     t.SchemaName,
					RelName:        t.RelName,
					TableSize:      t.TableSize,
					NLiveTup:       t.NLiveTup,
					NDeadTup:       t.NDeadTup,
					SeqScan:        t.SeqScan,
					IdxScan:        t.IdxScan,
					LastVacuum:     t.LastVacuum,
					LastAutovacuum: t.LastAutovacuum,
					LastAnalyze:    t.LastAnalyze,
				})
			}
		}

	case "indexes":
		indexes, err := h.storage.GetIndexStats(ctx, snapshot.ID)
		if err != nil {
			c.Logger().Errorf("failed to get index stats: %v", err)
		} else {
			for _, idx := range indexes {
				data.Indexes = append(data.Indexes, PageIndex{
					SchemaName:   idx.SchemaName,
					RelName:      idx.RelName,
					IndexRelName: idx.IndexRelName,
					IdxScan:      idx.IdxScan,
					IndexSize:    idx.IndexSize,
					IsUnique:     idx.IsUnique,
					IsPrimary:    idx.IsPrimary,
				})
			}
		}

	case "bloat":
		bloat, err := h.storage.GetBloatStats(ctx, snapshot.ID)
		if err != nil {
			c.Logger().Errorf("failed to get bloat stats: %v", err)
		} else {
			for _, b := range bloat {
				data.Bloat = append(data.Bloat, PageBloat{
					SchemaName:   b.SchemaName,
					RelName:      b.RelName,
					NLiveTup:     b.NLiveTup,
					NDeadTup:     b.NDeadTup,
					BloatPercent: b.BloatPercent,
				})
			}
		}
	}

	return c.Render(http.StatusOK, "schema", data)
}

// SuggestionsPageData contains data for the suggestions page.
type SuggestionsPageData struct {
	BasePageData
	Suggestions    []PageSuggestion
	StatusFilter   string
	SeverityFilter string
	CriticalCount  int
	WarningCount   int
	InfoCount      int
	TotalCount     int
}

// PageSuggestion represents a suggestion for the page.
type PageSuggestion struct {
	ID           int64
	RuleID       string
	Severity     string
	Title        string
	Description  string
	TargetObject string
	Status       string
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
}

// Suggestions handles GET /suggestions requests.
func (h *PageHandler) Suggestions(c echo.Context) error {
	ctx := c.Request().Context()

	statusFilter := c.QueryParam("status")
	if statusFilter == "" {
		statusFilter = "active"
	}
	severityFilter := c.QueryParam("severity")

	data := SuggestionsPageData{
		BasePageData: BasePageData{
			Title:      "Suggestions",
			ActivePage: "suggestions",
			Version:    h.version,
		},
		Suggestions:    []PageSuggestion{},
		StatusFilter:   statusFilter,
		SeverityFilter: severityFilter,
	}

	// Get latest snapshot for timestamp
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
	}
	if snapshot != nil {
		data.LastSnapshot = snapshot.CapturedAt
	}

	// Get suggestions
	suggestions, err := h.storage.GetActiveSuggestions(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get suggestions: %v", err)
		return c.Render(http.StatusOK, "suggestions", data)
	}

	// Filter and count
	for _, sug := range suggestions {
		// Count by severity
		switch sug.Severity {
		case "critical":
			data.CriticalCount++
		case "warning":
			data.WarningCount++
		case "info":
			data.InfoCount++
		}
		data.TotalCount++

		// Apply severity filter
		if severityFilter != "" && sug.Severity != severityFilter {
			continue
		}

		data.Suggestions = append(data.Suggestions, PageSuggestion{
			ID:           sug.ID,
			RuleID:       sug.RuleID,
			Severity:     sug.Severity,
			Title:        sug.Title,
			Description:  sug.Description,
			TargetObject: sug.TargetObject,
			Status:       sug.Status,
			FirstSeenAt:  sug.FirstSeenAt,
			LastSeenAt:   sug.LastSeenAt,
		})
	}

	return c.Render(http.StatusOK, "suggestions", data)
}

// Helper functions

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func sortQueryStats(stats []models.QueryStat, field string, desc bool) {
	sort.Slice(stats, func(i, j int) bool {
		var less bool
		switch field {
		case "calls":
			less = stats[i].Calls < stats[j].Calls
		case "mean_time":
			less = stats[i].MeanExecTime < stats[j].MeanExecTime
		case "total_time":
			less = stats[i].TotalExecTime < stats[j].TotalExecTime
		case "rows":
			less = stats[i].Rows < stats[j].Rows
		default:
			less = stats[i].TotalExecTime < stats[j].TotalExecTime
		}
		if desc {
			return !less
		}
		return less
	})
}

func calculateRowsPerCall(rows, calls int64) float64 {
	if calls == 0 {
		return 0
	}
	return float64(rows) / float64(calls)
}

func calculateCacheHitRatio(hit, read int64) float64 {
	total := hit + read
	if total == 0 {
		return 100.0 // Assume 100% if no blocks accessed
	}
	return float64(hit) / float64(total) * 100
}
