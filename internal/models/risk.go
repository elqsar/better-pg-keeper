package models

import "time"

// OutageRisk holds the signals behind the usual "the database stopped" incidents:
// transaction ID wraparound, WAL retained by replication slots, sequences running
// out, forgotten prepared transactions and disk growth.
//
// It is stored as one JSON document per snapshot: nothing queries it by column,
// and the analyzer always needs all of it together.
type OutageRisk struct {
	// InRecovery is set on a standby, where WAL positions are not comparable and
	// freezing is driven by the primary.
	InRecovery bool `json:"in_recovery"`
	// Database is the database PGAnalyzer is connected to; per-table data
	// (OldestTables, Sequences) covers only this one.
	Database string `json:"database"`

	// Wraparound. Ages are in transactions (or multixacts) since the oldest
	// unfrozen ID; PostgreSQL stops accepting writes near 2^31.
	FreezeMaxAge          int64         `json:"freeze_max_age"`
	MultixactFreezeMaxAge int64         `json:"multixact_freeze_max_age"`
	Databases             []DatabaseAge `json:"databases"`
	// OldestTables covers only the database PGAnalyzer is connected to.
	OldestTables []TableAge `json:"oldest_tables"`

	// Things that stop VACUUM from freezing rows: old snapshots in sessions,
	// replication slots and prepared transactions.
	XminBackends  []XminBackend     `json:"xmin_backends"`
	Slots         []ReplicationSlot `json:"slots"`
	Replicas      []ReplicaLag      `json:"replicas"`
	PreparedXacts []PreparedXact    `json:"prepared_xacts"`

	Sequences []SequenceUsage `json:"sequences"`
	// SequencesUnreadable counts sequences whose value the monitoring role may
	// not read, so their usage is unknown.
	SequencesUnreadable int `json:"sequences_unreadable"`

	// DatabaseSize is the connected database; ClusterSize sums every database
	// the monitoring role can connect to.
	DatabaseSize int64 `json:"database_size"`
	ClusterSize  int64 `json:"cluster_size"`

	// Unavailable maps a check that failed (typically for lack of a privilege)
	// to its error. The other checks are still valid.
	Unavailable map[string]string `json:"unavailable,omitempty"`
}

// DatabaseAge is the freeze horizon of one database.
type DatabaseAge struct {
	Name    string `json:"name"`
	XIDAge  int64  `json:"xid_age"`
	MXIDAge int64  `json:"mxid_age"`
}

// TableAge is the freeze horizon of one table, including its TOAST table.
type TableAge struct {
	SchemaName string `json:"schemaname"`
	RelName    string `json:"relname"`
	XIDAge     int64  `json:"xid_age"`
	MXIDAge    int64  `json:"mxid_age"`
	TotalBytes int64  `json:"total_bytes"`
}

// XminBackend is a session holding back the xmin horizon.
type XminBackend struct {
	PID       int        `json:"pid"`
	Username  string     `json:"usename"`
	Database  string     `json:"datname"`
	State     string     `json:"state"`
	XminAge   int64      `json:"xmin_age"`
	XactStart *time.Time `json:"xact_start,omitempty"`
	Query     string     `json:"query"`
}

// ReplicationSlot is one row of pg_replication_slots.
type ReplicationSlot struct {
	Name   string `json:"name"`
	Type   string `json:"type"` // physical or logical
	Active bool   `json:"active"`
	// WALStatus is reserved, extended, unreserved or lost.
	WALStatus string `json:"wal_status"`
	// RetainedBytes is WAL kept on disk for this slot; nil on a standby or when
	// the slot has no restart position.
	RetainedBytes *int64 `json:"retained_bytes,omitempty"`
	// SafeWALSize is how much more WAL can be written before the slot is
	// invalidated; nil when max_slot_wal_keep_size is unlimited.
	SafeWALSize    *int64 `json:"safe_wal_size,omitempty"`
	XminAge        *int64 `json:"xmin_age,omitempty"`
	CatalogXminAge *int64 `json:"catalog_xmin_age,omitempty"`
}

// ReplicaLag is one connected standby from pg_stat_replication. Lag values are
// nil when the role lacks pg_monitor or the standby has not reported.
type ReplicaLag struct {
	ApplicationName string   `json:"application_name"`
	ClientAddr      string   `json:"client_addr"`
	State           string   `json:"state"`
	WriteLagSeconds *float64 `json:"write_lag_seconds,omitempty"`
	FlushLagSeconds *float64 `json:"flush_lag_seconds,omitempty"`
	ReplayLagSecs   *float64 `json:"replay_lag_seconds,omitempty"`
}

// PreparedXact is a two-phase transaction waiting for COMMIT/ROLLBACK PREPARED.
type PreparedXact struct {
	GID      string    `json:"gid"`
	Database string    `json:"database"`
	Owner    string    `json:"owner"`
	Prepared time.Time `json:"prepared"`
	XIDAge   int64     `json:"xid_age"`
}

// SequenceUsage is how far a sequence has advanced towards the largest value it
// can hand out: its own maximum, or the owning column's type maximum when that
// is smaller (a bigint sequence feeding an integer column).
type SequenceUsage struct {
	SchemaName   string  `json:"schemaname"`
	SequenceName string  `json:"sequencename"`
	TableName    string  `json:"table_name,omitempty"`
	ColumnName   string  `json:"column_name,omitempty"`
	ColumnType   string  `json:"column_type,omitempty"`
	LastValue    int64   `json:"last_value"`
	MaxValue     int64   `json:"max_value"`
	UsedFraction float64 `json:"used_fraction"` // 0-1
}

// SizeSample is one point of database size history.
type SizeSample struct {
	CapturedAt   time.Time `json:"captured_at"`
	ClusterBytes int64     `json:"cluster_bytes"`
}

// ConnectionPeak is the highest connection utilization seen over a period.
type ConnectionPeak struct {
	CapturedAt       time.Time `json:"captured_at"`
	TotalConnections int       `json:"total_connections"`
	IdleCount        int       `json:"idle_count"`
	MaxConnections   int       `json:"max_connections"`
}
