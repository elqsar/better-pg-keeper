package handlers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/user/pganalyzer/internal/models"
)

// SchemaStorage defines the storage interface needed by the schema handler.
type SchemaStorage interface {
	GetLatestSnapshot(ctx context.Context, instanceID int64) (*models.Snapshot, error)
	GetTableStats(ctx context.Context, snapshotID int64) ([]models.TableStat, error)
	GetIndexStats(ctx context.Context, snapshotID int64) ([]models.IndexStat, error)
	GetBloatStats(ctx context.Context, snapshotID int64) ([]models.BloatInfo, error)
}

// SchemaHandler handles schema API requests.
type SchemaHandler struct {
	storage    SchemaStorage
	instanceID int64
}

// TableResponse represents a table in the API response.
type TableResponse struct {
	Schema       string  `json:"schema"`
	Name         string  `json:"name"`
	FullName     string  `json:"full_name"`
	LiveTuples   int64   `json:"live_tuples"`
	DeadTuples   int64   `json:"dead_tuples"`
	SeqScans     int64   `json:"seq_scans"`
	IdxScans     int64   `json:"idx_scans"`
	TableSize    int64   `json:"table_size"`
	IndexSize    int64   `json:"index_size"`
	TotalSize    int64   `json:"total_size"`
	LastVacuum   *string `json:"last_vacuum,omitempty"`
	LastAnalyze  *string `json:"last_analyze,omitempty"`
	ScanRatio    float64 `json:"seq_scan_ratio"`
}

// TablesResponse represents the tables list response.
type TablesResponse struct {
	Tables []TableResponse `json:"tables"`
	Total  int             `json:"total"`
}

// IndexResponse represents an index in the API response.
type IndexResponse struct {
	Schema    string `json:"schema"`
	Table     string `json:"table"`
	Name      string `json:"name"`
	FullName  string `json:"full_name"`
	Scans     int64  `json:"scans"`
	TupRead   int64  `json:"tuples_read"`
	TupFetch  int64  `json:"tuples_fetched"`
	Size      int64  `json:"size"`
	IsUnique  bool   `json:"is_unique"`
	IsPrimary bool   `json:"is_primary"`
}

// IndexesResponse represents the indexes list response.
type IndexesResponse struct {
	Indexes []IndexResponse `json:"indexes"`
	Total   int             `json:"total"`
}

// BloatResponse represents a table with bloat in the API response.
type BloatResponse struct {
	Schema       string  `json:"schema"`
	Table        string  `json:"table"`
	FullName     string  `json:"full_name"`
	DeadTuples   int64   `json:"dead_tuples"`
	LiveTuples   int64   `json:"live_tuples"`
	BloatPercent float64 `json:"bloat_percent"`
}

// BloatListResponse represents the bloat list response.
type BloatListResponse struct {
	Tables []BloatResponse `json:"tables"`
	Total  int             `json:"total"`
}

// NewSchemaHandler creates a new SchemaHandler.
func NewSchemaHandler(storage SchemaStorage, instanceID int64) *SchemaHandler {
	return &SchemaHandler{
		storage:    storage,
		instanceID: instanceID,
	}
}

// GetTables handles GET /api/v1/schema/tables requests.
func (h *SchemaHandler) GetTables(c echo.Context) error {
	ctx := c.Request().Context()

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
		return c.JSON(http.StatusOK, TablesResponse{
			Tables: []TableResponse{},
			Total:  0,
		})
	}

	// Get table stats
	stats, err := h.storage.GetTableStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get table stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get table stats",
			"code":  "DATABASE_ERROR",
		})
	}

	// Convert to response format
	tables := make([]TableResponse, len(stats))
	for i, stat := range stats {
		tables[i] = tableStatToResponse(stat)
	}

	return c.JSON(http.StatusOK, TablesResponse{
		Tables: tables,
		Total:  len(tables),
	})
}

// GetIndexes handles GET /api/v1/schema/indexes requests.
func (h *SchemaHandler) GetIndexes(c echo.Context) error {
	ctx := c.Request().Context()

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
		return c.JSON(http.StatusOK, IndexesResponse{
			Indexes: []IndexResponse{},
			Total:   0,
		})
	}

	// Get index stats
	stats, err := h.storage.GetIndexStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get index stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get index stats",
			"code":  "DATABASE_ERROR",
		})
	}

	// Convert to response format
	indexes := make([]IndexResponse, len(stats))
	for i, stat := range stats {
		indexes[i] = indexStatToResponse(stat)
	}

	return c.JSON(http.StatusOK, IndexesResponse{
		Indexes: indexes,
		Total:   len(indexes),
	})
}

// GetBloat handles GET /api/v1/schema/bloat requests.
func (h *SchemaHandler) GetBloat(c echo.Context) error {
	ctx := c.Request().Context()

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
		return c.JSON(http.StatusOK, BloatListResponse{
			Tables: []BloatResponse{},
			Total:  0,
		})
	}

	// Get bloat stats
	stats, err := h.storage.GetBloatStats(ctx, snapshot.ID)
	if err != nil {
		c.Logger().Errorf("failed to get bloat stats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to get bloat stats",
			"code":  "DATABASE_ERROR",
		})
	}

	// Convert to response format
	tables := make([]BloatResponse, len(stats))
	for i, stat := range stats {
		tables[i] = BloatResponse{
			Schema:       stat.SchemaName,
			Table:        stat.RelName,
			FullName:     stat.SchemaName + "." + stat.RelName,
			DeadTuples:   stat.NDeadTup,
			LiveTuples:   stat.NLiveTup,
			BloatPercent: stat.BloatPercent,
		}
	}

	return c.JSON(http.StatusOK, BloatListResponse{
		Tables: tables,
		Total:  len(tables),
	})
}

// tableStatToResponse converts a TableStat to TableResponse.
func tableStatToResponse(stat models.TableStat) TableResponse {
	resp := TableResponse{
		Schema:      stat.SchemaName,
		Name:        stat.RelName,
		FullName:    stat.SchemaName + "." + stat.RelName,
		LiveTuples:  stat.NLiveTup,
		DeadTuples:  stat.NDeadTup,
		SeqScans:    stat.SeqScan,
		IdxScans:    stat.IdxScan,
		TableSize:   stat.TableSize,
		IndexSize:   stat.IndexSize,
		TotalSize:   stat.TableSize + stat.IndexSize,
	}

	// Calculate seq scan ratio
	totalScans := stat.SeqScan + stat.IdxScan
	if totalScans > 0 {
		resp.ScanRatio = float64(stat.SeqScan) / float64(totalScans)
	}

	// Format timestamps
	if stat.LastVacuum != nil {
		t := stat.LastVacuum.Format("2006-01-02T15:04:05Z")
		resp.LastVacuum = &t
	}
	if stat.LastAnalyze != nil {
		t := stat.LastAnalyze.Format("2006-01-02T15:04:05Z")
		resp.LastAnalyze = &t
	}

	return resp
}

// indexStatToResponse converts an IndexStat to IndexResponse.
func indexStatToResponse(stat models.IndexStat) IndexResponse {
	return IndexResponse{
		Schema:    stat.SchemaName,
		Table:     stat.RelName,
		Name:      stat.IndexRelName,
		FullName:  stat.SchemaName + "." + stat.RelName + "." + stat.IndexRelName,
		Scans:     stat.IdxScan,
		TupRead:   stat.IdxTupRead,
		TupFetch:  stat.IdxTupFetch,
		Size:      stat.IndexSize,
		IsUnique:  stat.IsUnique,
		IsPrimary: stat.IsPrimary,
	}
}
