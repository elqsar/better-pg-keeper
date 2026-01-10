package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	// Check PostgreSQL defaults
	if cfg.Postgres.Port != 5432 {
		t.Errorf("expected postgres port 5432, got %d", cfg.Postgres.Port)
	}
	if cfg.Postgres.SSLMode != "prefer" {
		t.Errorf("expected sslmode prefer, got %s", cfg.Postgres.SSLMode)
	}

	// Check Storage defaults
	if cfg.Storage.Path != "./data/pganalyzer.db" {
		t.Errorf("expected storage path ./data/pganalyzer.db, got %s", cfg.Storage.Path)
	}
	if cfg.Storage.Retention.Snapshots.Duration() != 168*time.Hour {
		t.Errorf("expected snapshots retention 168h, got %s", cfg.Storage.Retention.Snapshots)
	}

	// Check Scheduler defaults
	if cfg.Scheduler.SnapshotInterval.Duration() != 5*time.Minute {
		t.Errorf("expected snapshot_interval 5m, got %s", cfg.Scheduler.SnapshotInterval)
	}

	// Check Server defaults
	if cfg.Server.Port != 8080 {
		t.Errorf("expected server port 8080, got %d", cfg.Server.Port)
	}
	if !cfg.Server.Auth.Enabled {
		t.Error("expected auth enabled by default")
	}

	// Check Thresholds defaults
	if cfg.Thresholds.SlowQueryMs != 1000 {
		t.Errorf("expected slow_query_ms 1000, got %d", cfg.Thresholds.SlowQueryMs)
	}
}

func TestLoadFromString_ValidConfig(t *testing.T) {
	yaml := `
postgres:
  host: localhost
  port: 5432
  database: testdb
  user: testuser
  password: testpass
  sslmode: disable
storage:
  path: /tmp/test.db
  retention:
    snapshots: 24h
    query_stats: 48h
scheduler:
  snapshot_interval: 1m
  analysis_interval: 5m
server:
  host: 127.0.0.1
  port: 9090
  auth:
    enabled: true
    username: admin
    password: secret
thresholds:
  slow_query_ms: 500
  cache_hit_ratio_warning: 0.90
  bloat_percent_warning: 15
  unused_index_days: 14
  seq_scan_ratio_warning: 0.3
  min_table_size_for_index: 5000
`

	cfg, err := LoadFromString(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify PostgreSQL config
	if cfg.Postgres.Host != "localhost" {
		t.Errorf("expected host localhost, got %s", cfg.Postgres.Host)
	}
	if cfg.Postgres.Database != "testdb" {
		t.Errorf("expected database testdb, got %s", cfg.Postgres.Database)
	}
	if cfg.Postgres.SSLMode != "disable" {
		t.Errorf("expected sslmode disable, got %s", cfg.Postgres.SSLMode)
	}

	// Verify Storage config
	if cfg.Storage.Path != "/tmp/test.db" {
		t.Errorf("expected path /tmp/test.db, got %s", cfg.Storage.Path)
	}
	if cfg.Storage.Retention.Snapshots.Duration() != 24*time.Hour {
		t.Errorf("expected snapshots 24h, got %s", cfg.Storage.Retention.Snapshots)
	}

	// Verify Scheduler config
	if cfg.Scheduler.SnapshotInterval.Duration() != 1*time.Minute {
		t.Errorf("expected snapshot_interval 1m, got %s", cfg.Scheduler.SnapshotInterval)
	}

	// Verify Server config
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}

	// Verify Thresholds config
	if cfg.Thresholds.SlowQueryMs != 500 {
		t.Errorf("expected slow_query_ms 500, got %d", cfg.Thresholds.SlowQueryMs)
	}
}

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "simple var",
			input:    "password: ${MY_PASSWORD}",
			envVars:  map[string]string{"MY_PASSWORD": "secret123"},
			expected: "password: secret123",
		},
		{
			name:     "multiple vars",
			input:    "host: ${DB_HOST}\nport: ${DB_PORT}",
			envVars:  map[string]string{"DB_HOST": "localhost", "DB_PORT": "5432"},
			expected: "host: localhost\nport: 5432",
		},
		{
			name:     "var with default - env set",
			input:    "host: ${DB_HOST:-default.local}",
			envVars:  map[string]string{"DB_HOST": "custom.local"},
			expected: "host: custom.local",
		},
		{
			name:     "var with default - env not set",
			input:    "host: ${DB_HOST:-default.local}",
			envVars:  map[string]string{},
			expected: "host: default.local",
		},
		{
			name:     "undefined var without default",
			input:    "password: ${UNDEFINED_VAR}",
			envVars:  map[string]string{},
			expected: "password: ",
		},
		{
			name:     "no vars",
			input:    "host: localhost",
			envVars:  map[string]string{},
			expected: "host: localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result := ExpandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestLoadFromString_EnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_PG_HOST", "db.example.com")
	t.Setenv("TEST_PG_PASSWORD", "supersecret")
	t.Setenv("TEST_ADMIN_PASS", "adminpass")

	yaml := `
postgres:
  host: ${TEST_PG_HOST}
  database: mydb
  user: myuser
  password: ${TEST_PG_PASSWORD}
server:
  auth:
    username: admin
    password: ${TEST_ADMIN_PASS}
`

	cfg, err := LoadFromString(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Postgres.Host != "db.example.com" {
		t.Errorf("expected host db.example.com, got %s", cfg.Postgres.Host)
	}
	if cfg.Postgres.Password != "supersecret" {
		t.Errorf("expected password supersecret, got %s", cfg.Postgres.Password)
	}
	if cfg.Server.Auth.Password != "adminpass" {
		t.Errorf("expected auth password adminpass, got %s", cfg.Server.Auth.Password)
	}
}

func TestValidation_RequiredFields(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		expectedField string
	}{
		{
			name: "missing postgres host",
			yaml: `
postgres:
  database: mydb
  user: myuser
server:
  auth:
    enabled: false
`,
			expectedField: "postgres.host",
		},
		{
			name: "missing postgres database",
			yaml: `
postgres:
  host: localhost
  user: myuser
server:
  auth:
    enabled: false
`,
			expectedField: "postgres.database",
		},
		{
			name: "missing postgres user",
			yaml: `
postgres:
  host: localhost
  database: mydb
server:
  auth:
    enabled: false
`,
			expectedField: "postgres.user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromString(tt.yaml)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			if !containsField(err.Error(), tt.expectedField) {
				t.Errorf("expected error for field %s, got: %v", tt.expectedField, err)
			}
		})
	}
}

func TestValidation_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "postgres port too low",
			yaml: `
postgres:
  host: localhost
  database: mydb
  user: myuser
  port: 0
server:
  auth:
    enabled: false
`,
		},
		{
			name: "postgres port too high",
			yaml: `
postgres:
  host: localhost
  database: mydb
  user: myuser
  port: 70000
server:
  auth:
    enabled: false
`,
		},
		{
			name: "server port too low",
			yaml: `
postgres:
  host: localhost
  database: mydb
  user: myuser
server:
  port: 0
  auth:
    enabled: false
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromString(tt.yaml)
			if err == nil {
				t.Fatal("expected validation error for invalid port, got nil")
			}
			if !containsField(err.Error(), "port") {
				t.Errorf("expected error about port, got: %v", err)
			}
		})
	}
}

func TestValidation_InvalidThresholds(t *testing.T) {
	baseYaml := `
postgres:
  host: localhost
  database: mydb
  user: myuser
server:
  auth:
    enabled: false
thresholds:
`

	tests := []struct {
		name          string
		threshold     string
		expectedField string
	}{
		{
			name:          "negative slow_query_ms",
			threshold:     "  slow_query_ms: -1",
			expectedField: "slow_query_ms",
		},
		{
			name:          "cache_hit_ratio above 1",
			threshold:     "  cache_hit_ratio_warning: 1.5",
			expectedField: "cache_hit_ratio_warning",
		},
		{
			name:          "cache_hit_ratio below 0",
			threshold:     "  cache_hit_ratio_warning: -0.5",
			expectedField: "cache_hit_ratio_warning",
		},
		{
			name:          "bloat_percent above 100",
			threshold:     "  bloat_percent_warning: 150",
			expectedField: "bloat_percent_warning",
		},
		{
			name:          "negative unused_index_days",
			threshold:     "  unused_index_days: -5",
			expectedField: "unused_index_days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := baseYaml + tt.threshold
			_, err := LoadFromString(yaml)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !containsField(err.Error(), tt.expectedField) {
				t.Errorf("expected error for field %s, got: %v", tt.expectedField, err)
			}
		})
	}
}

func TestValidation_AuthEnabled(t *testing.T) {
	// When auth is enabled, username and password are required
	yaml := `
postgres:
  host: localhost
  database: mydb
  user: myuser
server:
  auth:
    enabled: true
    username: ""
    password: ""
`

	_, err := LoadFromString(yaml)
	if err == nil {
		t.Fatal("expected validation error for missing auth credentials, got nil")
	}
	if !containsField(err.Error(), "server.auth") {
		t.Errorf("expected error about server.auth, got: %v", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres:
  host: filehost
  database: filedb
  user: fileuser
server:
  auth:
    enabled: false
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Postgres.Host != "filehost" {
		t.Errorf("expected host filehost, got %s", cfg.Postgres.Host)
	}
	if cfg.Postgres.Database != "filedb" {
		t.Errorf("expected database filedb, got %s", cfg.Postgres.Database)
	}
}

func TestLoadFromEnvConfigPath(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom-config.yaml")

	configContent := `
postgres:
  host: envhost
  database: envdb
  user: envuser
server:
  auth:
    enabled: false
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	// Set the environment variable
	t.Setenv("PGANALYZER_CONFIG", configPath)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Postgres.Host != "envhost" {
		t.Errorf("expected host envhost, got %s", cfg.Postgres.Host)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"1h", 1 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"24h", 24 * time.Hour},
		{"168h", 168 * time.Hour},
		{"500ms", 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDuration(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Duration() != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, d.Duration())
			}
		})
	}
}

func TestDuration_String(t *testing.T) {
	d := Duration(5 * time.Minute)
	if d.String() != "5m0s" {
		t.Errorf("expected 5m0s, got %s", d.String())
	}
}

func TestPostgresConfig_FormatConnectionString(t *testing.T) {
	cfg := PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "mydb",
		User:     "myuser",
		Password: "mypass",
		SSLMode:  "disable",
	}

	connStr := cfg.FormatConnectionString()

	expectedParts := []string{
		"host=localhost",
		"port=5432",
		"dbname=mydb",
		"user=myuser",
		"password=mypass",
		"sslmode=disable",
	}

	for _, part := range expectedParts {
		if !containsField(connStr, part) {
			t.Errorf("connection string missing %q, got: %s", part, connStr)
		}
	}
}

func TestValidation_InvalidSSLMode(t *testing.T) {
	yaml := `
postgres:
  host: localhost
  database: mydb
  user: myuser
  sslmode: invalid
server:
  auth:
    enabled: false
`

	_, err := LoadFromString(yaml)
	if err == nil {
		t.Fatal("expected validation error for invalid sslmode, got nil")
	}
	if !containsField(err.Error(), "sslmode") {
		t.Errorf("expected error about sslmode, got: %v", err)
	}
}

func TestMustLoad_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()

	MustLoad("/nonexistent/config.yaml")
}

func TestValidationErrors_SingleError(t *testing.T) {
	errs := ValidationErrors{
		{Field: "test.field", Message: "is invalid"},
	}
	expected := "test.field: is invalid"
	if errs.Error() != expected {
		t.Errorf("expected %q, got %q", expected, errs.Error())
	}
}

func TestValidationErrors_MultipleErrors(t *testing.T) {
	errs := ValidationErrors{
		{Field: "field1", Message: "error1"},
		{Field: "field2", Message: "error2"},
	}
	result := errs.Error()
	if !containsField(result, "field1") || !containsField(result, "field2") {
		t.Errorf("expected both fields in error, got: %s", result)
	}
}

func TestIsRequired(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "single required error",
			err:      ValidationError{Field: "test", Message: "is required"},
			expected: true,
		},
		{
			name:     "non-required error",
			err:      ValidationError{Field: "test", Message: "is invalid"},
			expected: false,
		},
		{
			name: "multiple errors with required",
			err: ValidationErrors{
				{Field: "field1", Message: "is required"},
				{Field: "field2", Message: "is invalid"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRequired(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsField(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstr(s, substr)))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
