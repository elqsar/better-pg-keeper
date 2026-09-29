//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

// TestSetupChecks runs the setup checklist as a superuser, as a role without
// pg_monitor, and against a database without pg_stat_statements.
func TestSetupChecks(t *testing.T) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		t.Skip("set POSTGRES_HOST to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port, err := strconv.Atoi(envOr("POSTGRES_PORT", "5432"))
	if err != nil {
		t.Fatal(err)
	}
	dbName := envOr("POSTGRES_DATABASE", "testdb")
	user, password := envOr("POSTGRES_USER", "postgres"), envOr("POSTGRES_PASSWORD", "postgres")
	checks := func(db, user, password string) map[string]models.SetupCheck {
		t.Helper()
		pg, err := postgres.NewClient(postgres.ClientConfig{
			Host: host, Port: port, Database: db, User: user, Password: password,
			SSLMode: envOr("POSTGRES_SSLMODE", "disable"), ConnectTimeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := pg.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		defer pg.Close()
		report, err := pg.CheckSetup(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if report.StatsSince == nil || time.Since(*report.StatsSince) < 0 {
			t.Errorf("stats since = %v", report.StatsSince)
		}
		out := map[string]models.SetupCheck{}
		for _, c := range report.Checks {
			out[c.Name] = c
		}
		return out
	}

	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, dbName, user, password, envOr("POSTGRES_SSLMODE", "disable")))
	if err != nil {
		t.Fatal(err)
	}
	const (
		plainRole = "pgk_it_setup_plain"
		bareDB    = "pgk_it_setup_bare"
		schemaDB  = "pgk_it_setup_schema"
	)
	cleanup := func() {
		bg := context.Background()
		_, _ = conn.Exec(bg, "DROP DATABASE IF EXISTS "+bareDB+" WITH (FORCE)")
		_, _ = conn.Exec(bg, "DROP DATABASE IF EXISTS "+schemaDB+" WITH (FORCE)")
		_, _ = conn.Exec(bg, "DROP ROLE IF EXISTS "+plainRole)
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		conn.Close(context.Background())
	})
	for _, stmt := range []string{
		"CREATE ROLE " + plainRole + " LOGIN PASSWORD 'plain'",
		"CREATE DATABASE " + bareDB,
		"CREATE DATABASE " + schemaDB,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	super := checks(dbName, user, password)
	for _, name := range []string{"version", "pg_stat_statements", "privileges"} {
		if super[name].Status != models.SetupOK {
			t.Errorf("superuser %s = %+v, want ok", name, super[name])
		}
	}
	if h := super["hypopg"]; h.Status != models.SetupOK && h.Status != models.SetupInfo {
		t.Errorf("hypopg = %+v, want ok or info", h)
	}

	plain := checks(dbName, plainRole, "plain")
	if p := plain["privileges"]; p.Status != models.SetupWarn || !strings.Contains(p.Fix, `GRANT pg_monitor TO "`+plainRole+`";`) {
		t.Errorf("plain role privileges = %+v", p)
	}
	if s := plain["pg_stat_statements"]; s.Status != models.SetupOK {
		t.Errorf("plain role pg_stat_statements = %+v, want ok", s)
	}

	// The extension in a schema the role doesn't search, as on Supabase.
	schemaConn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, schemaDB, user, password, envOr("POSTGRES_SSLMODE", "disable")))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{"CREATE SCHEMA extensions", "CREATE EXTENSION pg_stat_statements SCHEMA extensions"} {
		if _, err := schemaConn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	schemaConn.Close(ctx)
	if s := checks(schemaDB, user, password)["pg_stat_statements"]; s.Status != models.SetupFail ||
		!strings.Contains(s.Fix, `SET search_path = "$user", public, "extensions";`) {
		t.Errorf("extension outside search_path = %+v", s)
	}

	bare := checks(bareDB, user, password)
	if s := bare["pg_stat_statements"]; s.Status != models.SetupFail || !strings.Contains(s.Fix, "CREATE EXTENSION pg_stat_statements;") {
		t.Errorf("database without the extension = %+v", s)
	}
}
