package models

import "time"

// IndexValidation is the planner's verdict on a hypothetical index (hypopg).
type IndexValidation struct {
	CostBefore float64 `json:"cost_before"`
	CostAfter  float64 `json:"cost_after"`
	UsesIndex  bool    `json:"uses_index"`
}

// IndexKeys is an existing btree index and its key columns in order.
type IndexKeys struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

// TableIndexInfo is what an index proposal on a table is judged against.
type TableIndexInfo struct {
	EstimatedRows int64       `json:"estimated_rows"`
	AvgKeyWidth   int         `json:"avg_key_width"` // bytes, sum over the proposed columns
	Indexes       []IndexKeys `json:"indexes"`
}

// QueryPlanReport holds generic plans of the busiest queries and the index
// proposals derived from them. It is stored as one JSON document per snapshot.
type QueryPlanReport struct {
	// HypoPG is set when proposals were checked with hypothetical indexes.
	HypoPG  bool               `json:"hypopg"`
	Queries []QueryPlanFinding `json:"queries"`
}

// QueryPlanFinding is what the generic plan of one query showed.
type QueryPlanFinding struct {
	QueryID int64  `json:"queryid"`
	Query   string `json:"query"`
	// TimeShare is this query's share of total execution time over the
	// selection window (0-1), and TotalTimeMs the time itself.
	TimeShare   float64   `json:"time_share"`
	TotalTimeMs float64   `json:"total_time_ms"`
	ExplainedAt time.Time `json:"explained_at"`
	// Error is set when the query could not be planned (e.g. missing privilege).
	Error     string           `json:"error,omitempty"`
	TotalCost float64          `json:"total_cost,omitempty"`
	SeqScans  []SeqScanFinding `json:"seq_scans,omitempty"`
}

// SeqScanFinding is one sequential scan in a query's plan and, when its filter
// allows, the index that would replace it.
type SeqScanFinding struct {
	Schema   string  `json:"schema"`
	Table    string  `json:"table"`
	Filter   string  `json:"filter,omitempty"`
	PlanRows float64 `json:"plan_rows"`
	Cost     float64 `json:"cost"`
	// EstimatedRows is the table's row estimate (pg_class.reltuples).
	EstimatedRows int64 `json:"estimated_rows"`
	// Columns is the proposed index; empty with SkipReason set when no index
	// is proposed for this scan.
	Columns        []string         `json:"columns,omitempty"`
	SkipReason     string           `json:"skip_reason,omitempty"`
	EstimatedBytes int64            `json:"estimated_bytes,omitempty"`
	Validation     *IndexValidation `json:"validation,omitempty"`
}
