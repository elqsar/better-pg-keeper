//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
)

func TestTableDiagnostics(t *testing.T) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		t.Skip("set POSTGRES_HOST to run PostgreSQL integration tests")
	}
	port, err := strconv.Atoi(envOr("POSTGRES_PORT", "5432"))
	if err != nil {
		t.Fatal(err)
	}
	user, password := envOr("POSTGRES_USER", "postgres"), envOr("POSTGRES_PASSWORD", "postgres")
	ssl := envOr("POSTGRES_SSLMODE", "disable")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s", host, port, envOr("POSTGRES_DATABASE", "testdb"), user, password, ssl))
	if err != nil {
		t.Fatal(err)
	}
	const dbName = "pgk_it_table_diagnostics"
	const plainRole = "pgk_it_table_diagnostics_plain"
	cleanup := func() {
		bg := context.Background()
		_, _ = admin.Exec(bg, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)")
		_, _ = admin.Exec(bg, "DROP ROLE IF EXISTS "+plainRole)
	}
	cleanup()
	t.Cleanup(func() { cleanup(); admin.Close(context.Background()) })
	for _, statement := range []string{
		"CREATE ROLE " + plainRole + " LOGIN PASSWORD 'plain'",
		"CREATE DATABASE " + dbName,
		"GRANT CONNECT ON DATABASE " + dbName + " TO " + plainRole,
	} {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s", host, port, dbName, user, password, ssl))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	for _, statement := range []string{
		"CREATE SCHEMA pgk_it_space",
		`CREATE TABLE pgk_it_space.events (id integer PRIMARY KEY, payload text)
		 WITH (autovacuum_vacuum_threshold=1000, autovacuum_vacuum_scale_factor=0.02,
		       autovacuum_analyze_threshold=500, autovacuum_analyze_scale_factor=0.05)`,
		"INSERT INTO pgk_it_space.events SELECT g, repeat(md5(g::text), 40) FROM generate_series(1, 70000) g",
		"UPDATE pgk_it_space.events SET payload = payload || 'x' WHERE id <= 2000",
		"ANALYZE pgk_it_space.events",
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	clientFor := func(username, pass string) postgres.Client {
		t.Helper()
		client, err := postgres.NewClient(postgres.ClientConfig{Host: host, Port: port, Database: dbName, User: username, Password: pass, SSLMode: ssl})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		return client
	}
	client := clientFor(user, password)
	tables, err := client.GetStatTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var table *models.TableStat
	for i := range tables {
		if tables[i].SchemaName == "pgk_it_space" && tables[i].RelName == "events" {
			table = &tables[i]
			break
		}
	}
	if table == nil || table.HeapBytes < 64<<20 || table.VacuumTrigger == nil || table.AnalyzeTrigger == nil {
		t.Fatalf("table diagnostic missing or too small: %+v", table)
	}
	if want := 1000 + .02*table.RelTuples; *table.VacuumTrigger != want {
		t.Fatalf("vacuum trigger = %v, want %v", *table.VacuumTrigger, want)
	}
	if table.AutovacuumDisabled {
		t.Fatal("events reported with autovacuum disabled")
	}

	// autovacuum_enabled=false is reported, and on PostgreSQL 18+ the
	// table-level autovacuum_vacuum_max_threshold caps the vacuum trigger.
	var versionNum int
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum); err != nil {
		t.Fatal(err)
	}
	options := "autovacuum_enabled=false"
	if versionNum >= 180000 {
		options += ", autovacuum_vacuum_max_threshold=500"
	}
	for _, statement := range []string{
		"CREATE TABLE pgk_it_space.capped (id integer) WITH (" + options + ")",
		"INSERT INTO pgk_it_space.capped SELECT g FROM generate_series(1, 10000) g",
		"ANALYZE pgk_it_space.capped",
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	tables, err = client.GetStatTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var capped *models.TableStat
	for i := range tables {
		if tables[i].SchemaName == "pgk_it_space" && tables[i].RelName == "capped" {
			capped = &tables[i]
		}
	}
	if capped == nil || !capped.AutovacuumDisabled || capped.VacuumTrigger == nil || capped.RelTuples <= 0 {
		t.Fatalf("capped table = %+v", capped)
	}
	wantCapped := 50 + .2*capped.RelTuples // server defaults
	if versionNum >= 180000 {
		wantCapped = 500
	}
	if *capped.VacuumTrigger != wantCapped {
		t.Fatalf("capped vacuum trigger = %v, want %v (server %d)", *capped.VacuumTrigger, wantCapped, versionNum)
	}
	getSpace := func(c postgres.Client) models.BloatInfo {
		t.Helper()
		stats, err := c.GetTableBloat(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range stats {
			if b.SchemaName == "pgk_it_space" && b.RelName == "events" {
				return b
			}
		}
		t.Fatal("events missing from space results")
		return models.BloatInfo{}
	}
	fallback := getSpace(client)
	if fallback.MeasurementSource != "stats_estimate" || fallback.FreeBytes != nil {
		t.Fatalf("without extension = %+v", fallback)
	}
	if _, err := conn.Exec(ctx, "CREATE EXTENSION pgstattuple SCHEMA pgk_it_space"); err != nil {
		t.Fatalf("installing test extension: %v", err)
	}
	measured := getSpace(client)
	if measured.MeasurementSource != "pgstattuple_approx" || measured.DeadTupleBytes == nil || measured.FreeBytes == nil {
		t.Fatalf("with extension = %+v", measured)
	}
	// A blocked measurement must fall back rather than failing the collector.
	lockTx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockTx.Exec(ctx, "LOCK TABLE pgk_it_space.events IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	lockCtx, lockCancel := context.WithTimeout(ctx, 10*time.Second)
	lockedStats, lockedErr := client.GetTableBloat(lockCtx)
	lockCancel()
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if lockedErr != nil {
		t.Fatalf("blocked measurement failed collection: %v", lockedErr)
	}
	var blocked models.BloatInfo
	for _, b := range lockedStats {
		if b.SchemaName == "pgk_it_space" && b.RelName == "events" {
			blocked = b
			break
		}
	}
	if blocked.MeasurementSource != "stats_estimate" || blocked.FreeBytes != nil {
		t.Fatalf("blocked measurement did not fall back: %+v", blocked)
	}
	if _, err := conn.Exec(ctx, "GRANT USAGE ON SCHEMA pgk_it_space TO "+plainRole); err != nil {
		t.Fatal(err)
	}
	plain := clientFor(plainRole, "plain")
	unprivileged := getSpace(plain)
	if unprivileged.MeasurementSource != "stats_estimate" || unprivileged.FreeBytes != nil {
		t.Fatalf("without pg_stat_scan_tables = %+v", unprivileged)
	}
}
