package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
)

func TestResolveConfigPath(t *testing.T) {
	t.Run("explicit flag wins over env", func(t *testing.T) {
		t.Setenv(config.EnvConfigPath, "/from/env.yaml")
		if got := resolveConfigPath("/from/flag.yaml"); got != "/from/flag.yaml" {
			t.Fatalf("got %q, want /from/flag.yaml", got)
		}
	})

	t.Run("env used when flag empty", func(t *testing.T) {
		t.Setenv(config.EnvConfigPath, "/from/env.yaml")
		if got := resolveConfigPath(""); got != "/from/env.yaml" {
			t.Fatalf("got %q, want /from/env.yaml", got)
		}
	})

	t.Run("falls back to configs/config.yaml when present", func(t *testing.T) {
		t.Setenv(config.EnvConfigPath, "")
		dir := t.TempDir()
		origWD, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(origWD) })

		// Without configs/config.yaml -> empty string
		if got := resolveConfigPath(""); got != "" {
			t.Fatalf("got %q, want empty string", got)
		}

		// Create configs/config.yaml -> returns it
		if err := os.MkdirAll(filepath.Join(dir, "configs"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "configs", "config.yaml"), []byte("postgres:\n  host: localhost\n"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if got := resolveConfigPath(""); got != "configs/config.yaml" {
			t.Fatalf("got %q, want configs/config.yaml", got)
		}
	})
}

func TestRunMigrationsOnly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfgPath := filepath.Join(dir, "config.yaml")

	cfgYAML := "postgres:\n  host: localhost\n  port: 5432\n  database: mydb\n  user: monitor\nserver:\n  auth:\n    enabled: false\nstorage:\n  path: " + dbPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := runMigrationsOnly(cfgPath); err != nil {
		t.Fatalf("runMigrationsOnly failed: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected migrated database file at %s: %v", dbPath, err)
	}
}

func TestInstanceRecordAndSetupOptions(t *testing.T) {
	cfg := &config.Config{
		Postgres: config.PostgresConfig{
			Host:     "db.internal",
			Port:     5433,
			Database: "prod",
		},
		Thresholds: config.ThresholdsConfig{
			SlowQueryWindow: config.Duration(12 * time.Hour),
			UnusedIndexDays: 14,
		},
		IndexAdvisor: config.IndexAdvisorConfig{
			Enabled: true,
		},
	}

	inst := instanceRecord(cfg)
	if inst.Name != "db.internal:5433/prod" || inst.Host != "db.internal" || inst.Port != 5433 || inst.Database != "prod" {
		t.Fatalf("unexpected instance record: %+v", inst)
	}

	opts := setupOptions(cfg, nil)
	if opts.SlowQueryWindow != 12*time.Hour {
		t.Fatalf("SlowQueryWindow = %v, want 12h", opts.SlowQueryWindow)
	}
	if opts.UnusedIndexDays != 14 {
		t.Fatalf("UnusedIndexDays = %d, want 14", opts.UnusedIndexDays)
	}
	if !opts.IndexAdvisor {
		t.Fatal("expected IndexAdvisor = true")
	}
}
