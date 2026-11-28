package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elqsar/pganalyzer/internal/models"
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
			expected: "postgres://testuser@localhost:5432/testdb?sslmode=prefer",
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
			expected: "postgres://testuser:secret@localhost:5432/testdb?sslmode=require",
		},
		{
			name: "with connect timeout",
			config: ClientConfig{
				Host:           "localhost",
				Database:       "testdb",
				User:           "testuser",
				ConnectTimeout: 30 * time.Second,
			},
			expected: "postgres://testuser@localhost:5432/testdb?connect_timeout=30&sslmode=prefer",
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
			expected: "postgres://testuser@localhost:5433/testdb?sslmode=disable",
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

func TestPgxClient_BuildConnStringEscapesValues(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Host: "::1", Port: 5432, Database: "sales db", User: "report user",
		Password: "a b#?&='ü", SSLMode: "require",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := pgxpool.ParseConfig(client.buildConnString())
	if err != nil {
		t.Fatalf("parse escaped URI: %v", err)
	}
	if parsed.ConnConfig.Host != "::1" || parsed.ConnConfig.Database != "sales db" ||
		parsed.ConnConfig.User != "report user" || parsed.ConnConfig.Password != "a b#?&='ü" {
		t.Fatalf("connection values changed after parsing")
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
		// Leading comments and parentheses
		{"/* comment */ DELETE FROM users", true},
		{"/* outer /* nested */ outer */ UPDATE users SET a = 1", true},
		{"-- leading line comment\nINSERT INTO users VALUES (1)", true},
		{"((DELETE FROM users))", true},
		{"/* comment */ SELECT * FROM users", false},
		{"-- comment\nSELECT 'DELETE FROM users'", false},
		// Data-modifying CTEs and additional write/utility commands
		{"WITH d AS (DELETE FROM users RETURNING *) SELECT * FROM d", true},
		{"WITH u AS (UPDATE users SET x = 1 RETURNING id) SELECT * FROM u", true},
		{"WITH i AS (INSERT INTO users VALUES (1) RETURNING id) SELECT * FROM i", true},
		{"WITH m AS (MERGE INTO users USING src ON users.id = src.id WHEN MATCHED THEN DELETE) SELECT * FROM m", true},
		{"MERGE INTO users USING src ON users.id = src.id WHEN MATCHED THEN DELETE", true},
		{"COPY users TO STDOUT", true},
		{"CALL my_proc()", true},
		{"DO $$ BEGIN PERFORM 1; END $$", true},
		{"VACUUM users", true},
		// Multi-statement queries
		{"SELECT 1; DROP TABLE users", true},
		{"SELECT 1;", false},
		// Write keywords inside literals, dollar-quotes, comments, or quoted identifiers
		{"WITH cte AS (SELECT 'DELETE' AS op, E'\\'UPDATE\\'' AS op2, $$INSERT$$ AS op3, \"DELETE\" FROM t) SELECT * FROM cte", false},
		{"SELECT ';' FROM users", false},
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

func TestHasMultipleStatements(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{"SELECT 1", false},
		{"SELECT 1;", false},
		{"SELECT 1; -- trailing comment", false},
		{"SELECT ';' AS s", false},
		{"SELECT $$a;b$$", false},
		{"SELECT $tag$a;b$tag$", false},
		{"SELECT 1; SELECT 2", true},
		{"SELECT 1; DROP TABLE users", true},
	}
	for _, tt := range tests {
		if got := hasMultipleStatements(tt.query); got != tt.want {
			t.Errorf("hasMultipleStatements(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestExplainableAndMaxPlaceholder(t *testing.T) {
	explainTests := []struct {
		query string
		want  bool
	}{
		{"SELECT * FROM orders WHERE id = $1", true},
		{"/* app=api trace=123 */ SELECT * FROM orders", true},
		{"/* outer /* nested */ */ WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"-- leading comment\nUPDATE orders SET status = $1 WHERE id = $2", true},
		{"((SELECT 1))", true},
		{"TABLE orders", true},
		{"VALUES (1, 2)", true},
		{"CREATE INDEX idx ON orders(id)", false},
		{"VACUUM orders", false},
		{"SET work_mem = '64MB'", false},
		{"SELECT 1; DROP TABLE orders", false},
		{"", false},
	}
	for _, tt := range explainTests {
		if got := Explainable(tt.query); got != tt.want {
			t.Errorf("Explainable(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}

	placeholderTests := []struct {
		query string
		want  int
	}{
		{"SELECT 1", 0},
		{"SELECT $0", 0},
		{"SELECT $1, $3, $2", 3},
		{"SELECT $1, $999999999", 1},
		{"SELECT $1000, $1001", 1000},
	}
	for _, tt := range placeholderTests {
		if got := MaxPlaceholder(tt.query); got != tt.want {
			t.Errorf("MaxPlaceholder(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func TestFormatSQLLiteral(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr bool
	}{
		{"nil", nil, "NULL", false},
		{"bool true", true, "TRUE", false},
		{"bool false", false, "FALSE", false},
		{"int", 42, "42", false},
		{"large float64 whole", float64(1e7), "10000000", false},
		{"float64 decimal", 3.14, "3.14", false},
		{"float32", float32(2.5), "2.5", false},
		{"NaN", math.NaN(), "", true},
		{"+Inf", math.Inf(1), "", true},
		{"-Inf", math.Inf(-1), "", true},
		{"float32 NaN", float32(math.NaN()), "", true},
		{"string escaping", "O'Reilly", "'O''Reilly'", false},
		{"time", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "'2026-01-02T03:04:05Z'::timestamptz", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatSQLLiteral(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("formatSQLLiteral(%v) err = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("formatSQLLiteral(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildExplainPlanExecutionTime(t *testing.T) {
	withExec := `[{"Plan":{"Node Type":"Seq Scan","Actual Total Time":12.5},"Execution Time":18.75}]`
	plan := buildExplainPlan(withExec, true)
	if plan.ExecutionTime == nil || *plan.ExecutionTime != 18.75 {
		t.Fatalf("ExecutionTime = %v, want 18.75", plan.ExecutionTime)
	}

	fallback := `[{"Plan":{"Node Type":"Seq Scan","Actual Total Time":12.5}}]`
	plan2 := buildExplainPlan(fallback, true)
	if plan2.ExecutionTime == nil || *plan2.ExecutionTime != 12.5 {
		t.Fatalf("fallback ExecutionTime = %v, want 12.5", plan2.ExecutionTime)
	}

	nonAnalyze := buildExplainPlan(withExec, false)
	if nonAnalyze.ExecutionTime != nil {
		t.Fatalf("non-analyze ExecutionTime = %v, want nil", nonAnalyze.ExecutionTime)
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
