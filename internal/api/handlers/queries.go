package handlers

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

// QueriesStorage defines the storage interface needed by the queries handler.
type QueriesStorage interface {
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	GetQueryStats(ctx context.Context, snapshotID int64) ([]models.QueryStat, error)
	SaveExplainPlan(ctx context.Context, plan *models.ExplainPlan) (int64, error)
	GetExplainPlan(ctx context.Context, queryID int64) (*models.ExplainPlan, error)
}

// QueriesHandler handles query API requests.
type QueriesHandler struct {
	storage    QueriesStorage
	pgClient   postgres.Client
	instanceID int64
}

// QueryListResponse represents the paginated query list response.
type QueryListResponse struct {
	Queries []QueryDetail `json:"queries"`
	Total   int           `json:"total"`
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
}

// QueryDetail represents detailed query information.
type QueryDetail struct {
	QueryID         int64   `json:"queryid"`
	Query           string  `json:"query"`
	Calls           int64   `json:"calls"`
	MeanExecTimeMs  float64 `json:"mean_exec_time_ms"`
	MinExecTimeMs   float64 `json:"min_exec_time_ms"`
	MaxExecTimeMs   float64 `json:"max_exec_time_ms"`
	TotalExecTimeMs float64 `json:"total_exec_time_ms"`
	RowsPerCall     float64 `json:"rows_per_call"`
	CacheHitRatio   float64 `json:"cache_hit_ratio"`
}

// TopQueriesResponse represents the top queries response.
type TopQueriesResponse struct {
	Queries []QueryDetail `json:"queries"`
	Metric  string        `json:"metric"`
	Limit   int           `json:"limit"`
}

// ExplainResponse represents the explain plan response.
type ExplainResponse struct {
	QueryID       int64    `json:"queryid"`
	PlanText      string   `json:"plan_text"`
	PlanJSON      string   `json:"plan_json,omitempty"`
	CapturedAt    string   `json:"captured_at"`
	ExecutionTime *float64 `json:"execution_time,omitempty"`
	UsedParams    bool     `json:"used_params"`
}

// ExplainRequest represents the request body for parameterized EXPLAIN.
type ExplainRequest struct {
	Parameters []ExplainParameter `json:"parameters,omitempty"`
}

// ExplainParameter represents a single query parameter.
type ExplainParameter struct {
	Position int    `json:"position"` // 1-based ($1, $2, etc.)
	Value    string `json:"value"`
	Type     string `json:"type"` // text, integer, numeric, boolean, timestamp, uuid
}

// NewQueriesHandler creates a new QueriesHandler.
func NewQueriesHandler(storage QueriesStorage, pgClient postgres.Client, instanceID int64) *QueriesHandler {
	return &QueriesHandler{
		storage:    storage,
		pgClient:   pgClient,
		instanceID: instanceID,
	}
}

// ListQueries handles GET /api/v1/queries requests.
// Query params: sort (calls, mean_time, total_time, rows), order (asc, desc), limit, offset
func (h *QueriesHandler) ListQueries(c echo.Context) error {
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
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}

	// Validate sort field
	validSortFields := map[string]bool{
		"calls": true, "mean_time": true, "total_time": true, "rows": true,
	}
	if !validSortFields[sortField] {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid sort field, must be one of: calls, mean_time, total_time, rows",
			"code":  "VALIDATION_ERROR",
		})
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

	if snapshot == nil {
		return c.JSON(http.StatusOK, QueryListResponse{
			Queries: []QueryDetail{},
			Total:   0,
			Page:    1,
			PerPage: limit,
		})
	}

	// Get query stats
	stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get query stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get query stats",
			"code":  "DATABASE_ERROR",
		})
	}

	// Sort stats
	sortStats(stats, sortField, order == "desc")

	// Calculate total and apply pagination
	total := len(stats)
	if offset >= total {
		return c.JSON(http.StatusOK, QueryListResponse{
			Queries: []QueryDetail{},
			Total:   total,
			Page:    (offset / limit) + 1,
			PerPage: limit,
		})
	}
	end := offset + limit
	if end > total {
		end = total
	}
	pageStats := stats[offset:end]

	// Convert to response format
	queries := make([]QueryDetail, len(pageStats))
	for i, stat := range pageStats {
		queries[i] = statToQueryDetail(stat)
	}

	return c.JSON(http.StatusOK, QueryListResponse{
		Queries: queries,
		Total:   total,
		Page:    (offset / limit) + 1,
		PerPage: limit,
	})
}

// GetTopQueries handles GET /api/v1/queries/top requests.
// Query params: metric (calls, time, rows), limit
func (h *QueriesHandler) GetTopQueries(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse query parameters
	metric := c.QueryParam("metric")
	if metric == "" {
		metric = "time"
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	// Validate metric
	validMetrics := map[string]string{
		"calls": "calls",
		"time":  "total_time",
		"rows":  "rows",
	}
	sortField, ok := validMetrics[metric]
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid metric, must be one of: calls, time, rows",
			"code":  "VALIDATION_ERROR",
		})
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

	if snapshot == nil {
		return c.JSON(http.StatusOK, TopQueriesResponse{
			Queries: []QueryDetail{},
			Metric:  metric,
			Limit:   limit,
		})
	}

	// Get query stats
	stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get query stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get query stats",
			"code":  "DATABASE_ERROR",
		})
	}

	// Sort by metric (descending)
	sortStats(stats, sortField, true)

	// Take top N
	if len(stats) > limit {
		stats = stats[:limit]
	}

	// Convert to response format
	queries := make([]QueryDetail, len(stats))
	for i, stat := range stats {
		queries[i] = statToQueryDetail(stat)
	}

	return c.JSON(http.StatusOK, TopQueriesResponse{
		Queries: queries,
		Metric:  metric,
		Limit:   limit,
	})
}

// ExplainQuery handles POST /api/v1/queries/:id/explain requests.
// Optionally accepts a JSON body with parameters for parameterized EXPLAIN.
func (h *QueriesHandler) ExplainQuery(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse query ID
	queryID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid query ID",
			"code":  "VALIDATION_ERROR",
		})
	}

	// Parse optional request body for parameters
	var req ExplainRequest
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
				"code":  "VALIDATION_ERROR",
			})
		}
	}

	// Get latest snapshot to find the query
	snapshot, err := h.storage.GetLatestSnapshot(ctx, h.instanceID)
	if err != nil {
		c.Logger().Errorf("failed to get latest snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get snapshot data",
			"code":  "DATABASE_ERROR",
		})
	}

	if snapshot == nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "no snapshots available",
			"code":  "NOT_FOUND",
		})
	}

	// Find the query
	stats, err := h.storage.GetQueryStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get query stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get query stats",
			"code":  "DATABASE_ERROR",
		})
	}

	var queryText string
	for _, stat := range stats {
		if stat.QueryID == queryID {
			queryText = stat.Query
			break
		}
	}

	if queryText == "" {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "query not found",
			"code":  "NOT_FOUND",
		})
	}

	// Detect placeholders and build parameter slice
	placeholders := extractPlaceholders(queryText)
	var maxPos int
	if len(placeholders) > 0 {
		maxPos = placeholders[len(placeholders)-1]
	}
	params := buildParamSlice(req.Parameters, maxPos)

	// Run EXPLAIN (not ANALYZE - safer)
	var plan *models.ExplainPlan
	usedParams := false
	if len(params) > 0 {
		plan, err = h.pgClient.ExplainWithParams(ctx, queryText, params, false)
		usedParams = err == nil
	} else {
		plan, err = h.pgClient.Explain(ctx, queryText, false)
	}
	if err != nil {
		c.Logger().Errorf("failed to run EXPLAIN: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to run EXPLAIN: " + err.Error(),
			"code":  "DATABASE_ERROR",
		})
	}

	// Set the query ID and captured time
	plan.QueryID = queryID
	plan.CapturedAt = time.Now()

	// Save the plan
	if _, err := h.storage.SaveExplainPlan(ctx, plan); err != nil {
		c.Logger().Errorf("failed to save explain plan: %v", err)
		// Don't fail the request, just log the error
	}

	return c.JSON(http.StatusOK, ExplainResponse{
		QueryID:       queryID,
		PlanText:      plan.PlanText,
		PlanJSON:      plan.PlanJSON,
		CapturedAt:    plan.CapturedAt.Format(time.RFC3339),
		ExecutionTime: plan.ExecutionTime,
		UsedParams:    usedParams,
	})
}

// sortStats sorts query stats by the specified field.
func sortStats(stats []models.QueryStat, field string, desc bool) {
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

// statToQueryDetail converts a QueryStat to QueryDetail.
func statToQueryDetail(stat models.QueryStat) QueryDetail {
	var rowsPerCall float64
	if stat.Calls > 0 {
		rowsPerCall = float64(stat.Rows) / float64(stat.Calls)
	}

	var cacheHitRatio float64
	totalBlocks := stat.SharedBlksHit + stat.SharedBlksRead
	if totalBlocks > 0 {
		cacheHitRatio = float64(stat.SharedBlksHit) / float64(totalBlocks) * 100
	}

	return QueryDetail{
		QueryID:         stat.QueryID,
		Query:           stat.Query,
		Calls:           stat.Calls,
		MeanExecTimeMs:  stat.MeanExecTime,
		MinExecTimeMs:   stat.MinExecTime,
		MaxExecTimeMs:   stat.MaxExecTime,
		TotalExecTimeMs: stat.TotalExecTime,
		RowsPerCall:     rowsPerCall,
		CacheHitRatio:   cacheHitRatio,
	}
}

// extractPlaceholders finds all $N placeholders in query text.
// Returns sorted unique placeholder positions (e.g., [1, 2, 3]).
func extractPlaceholders(query string) []int {
	re := regexp.MustCompile(`\$(\d+)`)
	matches := re.FindAllStringSubmatch(query, -1)

	seen := make(map[int]bool)
	var positions []int
	for _, match := range matches {
		pos, _ := strconv.Atoi(match[1])
		if !seen[pos] {
			seen[pos] = true
			positions = append(positions, pos)
		}
	}
	sort.Ints(positions)
	return positions
}

// buildParamSlice converts ExplainParameter slice to []any for pgx.
// Returns nil if no valid parameters provided.
func buildParamSlice(params []ExplainParameter, maxPos int) []any {
	if len(params) == 0 || maxPos == 0 {
		return nil
	}

	result := make([]any, maxPos)
	hasValues := false
	for _, p := range params {
		if p.Position > 0 && p.Position <= maxPos && p.Value != "" {
			result[p.Position-1] = convertParamValue(p.Value, p.Type)
			hasValues = true
		}
	}

	if !hasValues {
		return nil
	}
	return result
}

// convertParamValue converts string value to appropriate Go type based on type hint.
func convertParamValue(value, typeHint string) any {
	switch typeHint {
	case "integer", "int", "bigint":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil {
			return v
		}
	case "numeric", "decimal", "float", "double":
		if v, err := strconv.ParseFloat(value, 64); err == nil {
			return v
		}
	case "boolean", "bool":
		if v, err := strconv.ParseBool(value); err == nil {
			return v
		}
	case "timestamp", "timestamptz":
		if v, err := time.Parse(time.RFC3339, value); err == nil {
			return v
		}
	}
	// Default: return as string (works for text, varchar, uuid, unknown types)
	return value
}
