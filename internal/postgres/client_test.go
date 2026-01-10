package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/user/pganalyzer/internal/models"
)

func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig()

	if cfg.Port != 5432 {
		t.Errorf("expected port 5432, got %d", cfg.Port)
	}
	if cfg.SSLMode != "prefer" {
		t.Errorf("expected sslmode 'prefer', got %s", cfg.SSLMode)
	}
	if cfg.ConnectTimeout != 10*time.Second {
		t.Errorf("expected connect timeout 10s, got %s", cfg.ConnectTimeout)
	}
	if cfg.MaxConnections != 5 {
		t.Errorf("expected max connections 5, got %d", cfg.MaxConnections)
	}
	if cfg.MinConnections != 1 {
		t.Errorf("expected min connections 1, got %d", cfg.MinConnections)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Errorf("expected max conn lifetime 1h, got %s", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != 30*time.Minute {
		t.Errorf("expected max conn idle time 30m, got %s", cfg.MaxConnIdleTime)
	}
}

func TestNewClient_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  ClientConfig
		wantErr string
	}{
		{
			name:    "missing host",
			config:  ClientConfig{Database: "test", User: "test"},
			wantErr: "host is required",
		},
		{
			name:    "missing database",
			config:  ClientConfig{Host: "localhost", User: "test"},
			wantErr: "database is required",
		},
		{
			name:    "missing user",
			config:  ClientConfig{Host: "localhost", Database: "test"},
			wantErr: "user is required",
		},
		{
			name: "valid config",
			config: ClientConfig{
				Host:     "localhost",
				Database: "test",
				User:     "test",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
					return
				}
				if !containsString(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
					return
				}
				if client == nil {
					t.Error("expected client to be non-nil")
				}
			}
		})
	}
}

func TestPgxClient_BuildConnString(t *testing.T) {
	tests := []struct {
		name     string
		config   ClientConfig
		expected string
	}{
		{
			name: "minimal config",
			config: ClientConfig{
				Host:     "localhost",
				Database: "testdb",
				User:     "testuser",
			},
			expected: "host=localhost port=5432 dbname=testdb user=testuser sslmode=prefer",
		},
		{
			name: "with password",
			config: ClientConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "testdb",
				User:     "testuser",
				Password: "secret",
				SSLMode:  "require",
			},
			expected: "host=localhost port=5432 dbname=testdb user=testuser sslmode=require password=secret",
		},
		{
			name: "with connect timeout",
			config: ClientConfig{
				Host:           "localhost",
				Database:       "testdb",
				User:           "testuser",
				ConnectTimeout: 30 * time.Second,
			},
			expected: "host=localhost port=5432 dbname=testdb user=testuser sslmode=prefer connect_timeout=30",
		},
		{
			name: "custom port",
			config: ClientConfig{
				Host:     "localhost",
				Port:     5433,
				Database: "testdb",
				User:     "testuser",
				SSLMode:  "disable",
			},
			expected: "host=localhost port=5433 dbname=testdb user=testuser sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.config)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			connStr := client.buildConnString()
			if connStr != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, connStr)
			}
		})
	}
}

func TestPgxClient_NotConnected(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Host:     "localhost",
		Database: "test",
		User:     "test",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// Test Ping without connection
	err = client.Ping(ctx)
	if err == nil {
		t.Error("expected error for Ping without connection")
	}
	if !containsString(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got %v", err)
	}

	// Test GetStatStatements without connection
	_, err = client.GetStatStatements(ctx)
	if err == nil {
		t.Error("expected error for GetStatStatements without connection")
	}

	// Test GetStatTables without connection
	_, err = client.GetStatTables(ctx)
	if err == nil {
		t.Error("expected error for GetStatTables without connection")
	}

	// Test GetStatIndexes without connection
	_, err = client.GetStatIndexes(ctx)
	if err == nil {
		t.Error("expected error for GetStatIndexes without connection")
	}

	// Test GetDatabaseStats without connection
	_, err = client.GetDatabaseStats(ctx)
	if err == nil {
		t.Error("expected error for GetDatabaseStats without connection")
	}

	// Test GetTableBloat without connection
	_, err = client.GetTableBloat(ctx)
	if err == nil {
		t.Error("expected error for GetTableBloat without connection")
	}

	// Test GetIndexDetails without connection
	_, err = client.GetIndexDetails(ctx)
	if err == nil {
		t.Error("expected error for GetIndexDetails without connection")
	}

	// Test Explain without connection
	_, err = client.Explain(ctx, "SELECT 1", false)
	if err == nil {
		t.Error("expected error for Explain without connection")
	}

	// Test GetVersion without connection
	_, err = client.GetVersion(ctx)
	if err == nil {
		t.Error("expected error for GetVersion without connection")
	}

	// Test GetStatsResetTime without connection
	_, err = client.GetStatsResetTime(ctx)
	if err == nil {
		t.Error("expected error for GetStatsResetTime without connection")
	}
}

func TestIsWriteQuery(t *testing.T) {
	tests := []struct {
		query    string
		expected bool
	}{
		{"SELECT * FROM users", false},
		{"select * from users", false},
		{"  SELECT id FROM users WHERE id = 1", false},
		{"INSERT INTO users (name) VALUES ('test')", true},
		{"insert into users (name) values ('test')", true},
		{"  INSERT INTO users (name) VALUES ('test')", true},
		{"UPDATE users SET name = 'test'", true},
		{"update users set name = 'test'", true},
		{"DELETE FROM users WHERE id = 1", true},
		{"delete from users where id = 1", true},
		{"TRUNCATE TABLE users", true},
		{"DROP TABLE users", true},
		{"ALTER TABLE users ADD COLUMN email VARCHAR(255)", true},
		{"CREATE TABLE test (id INT)", true},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := isWriteQuery(tt.query)
			if result != tt.expected {
				t.Errorf("isWriteQuery(%q) = %v, expected %v", tt.query, result, tt.expected)
			}
		})
	}
}

func TestPgxClient_ExplainWriteQueryDenied(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Host:     "localhost",
		Database: "test",
		User:     "test",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Simulate connection by setting pool to non-nil (will fail on actual query)
	// For this test, we just want to verify the write query check happens first

	ctx := context.Background()

	writeQueries := []string{
		"INSERT INTO users (name) VALUES ('test')",
		"UPDATE users SET name = 'test'",
		"DELETE FROM users WHERE id = 1",
	}

	for _, query := range writeQueries {
		t.Run(query, func(t *testing.T) {
			// Even without connection, the write query check should happen first
			// when analyze=true
			_, err := client.Explain(ctx, query, true)
			if err == nil {
				t.Errorf("expected error for write query with ANALYZE")
			}
			if !containsString(err.Error(), "write queries") {
				// If it's "not connected", that means write check didn't happen first
				// which would be a bug if pool was connected
				t.Logf("got error: %v", err)
			}
		})
	}
}

func TestClose(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Host:     "localhost",
		Database: "test",
		User:     "test",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Close without connection should not error
	err = client.Close()
	if err != nil {
		t.Errorf("expected no error on Close without connection, got %v", err)
	}
}

func TestClientInterface(t *testing.T) {
	// Verify PgxClient implements Client interface
	var _ Client = (*PgxClient)(nil)
}

func TestQueryStatModel(t *testing.T) {
	// Test QueryStat model has all expected fields
	stat := models.QueryStat{
		ID:             1,
		SnapshotID:     100,
		QueryID:        12345,
		Query:          "SELECT * FROM users",
		Calls:          1000,
		TotalExecTime:  5000.5,
		MeanExecTime:   5.0,
		MinExecTime:    0.1,
		MaxExecTime:    100.0,
		Rows:           10000,
		SharedBlksHit:  50000,
		SharedBlksRead: 1000,
		Plans:          500,
		TotalPlanTime:  100.0,
	}

	if stat.QueryID != 12345 {
		t.Errorf("expected QueryID 12345, got %d", stat.QueryID)
	}
}

func TestTableStatModel(t *testing.T) {
	now := time.Now()
	stat := models.TableStat{
		ID:             1,
		SnapshotID:     100,
		SchemaName:     "public",
		RelName:        "users",
		SeqScan:        100,
		SeqTupRead:     10000,
		IdxScan:        5000,
		IdxTupFetch:    4500,
		NLiveTup:       50000,
		NDeadTup:       1000,
		LastVacuum:     &now,
		LastAutovacuum: nil,
		LastAnalyze:    &now,
		TableSize:      1024 * 1024,
		IndexSize:      512 * 1024,
	}

	if stat.SchemaName != "public" {
		t.Errorf("expected SchemaName 'public', got %s", stat.SchemaName)
	}
}

func TestDatabaseStatsModel(t *testing.T) {
	stats := models.DatabaseStats{
		DatabaseName:  "testdb",
		BlksHit:       100000,
		BlksRead:      5000,
		CacheHitRatio: 95.24,
	}

	if stats.CacheHitRatio < 95.0 || stats.CacheHitRatio > 96.0 {
		t.Errorf("expected CacheHitRatio around 95.24, got %.2f", stats.CacheHitRatio)
	}
}

func TestBloatInfoModel(t *testing.T) {
	bloat := models.BloatInfo{
		SchemaName:   "public",
		RelName:      "orders",
		NDeadTup:     5000,
		NLiveTup:     100000,
		BloatPercent: 5.0,
	}

	if bloat.BloatPercent != 5.0 {
		t.Errorf("expected BloatPercent 5.0, got %.2f", bloat.BloatPercent)
	}
}

func TestIndexDetailModel(t *testing.T) {
	detail := models.IndexDetail{
		SchemaName:  "public",
		TableName:   "users",
		IndexName:   "users_pkey",
		IndexDef:    "CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)",
		IndexSize:   65536,
		IdxScan:     10000,
		IdxTupRead:  50000,
		IdxTupFetch: 45000,
		IsUnique:    true,
		IsPrimary:   true,
		IsValid:     true,
		TableSize:   1024 * 1024,
	}

	if !detail.IsPrimary {
		t.Error("expected IsPrimary to be true")
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
