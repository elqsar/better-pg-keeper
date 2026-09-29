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

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/collector/settings"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

// TestServerSettings checks the configuration review against a real server and
// runs it through collection, analysis and the rules.
//
// It changes server configuration (ALTER SYSTEM, reset on cleanup), so run it
// against a throwaway container. The pg_stat_statements eviction check needs a
// small table: start the server with -c pg_stat_statements.max=100.
func TestServerSettings(t *testing.T) {
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
	newClient := func(user, password string) *postgres.PgxClient {
		pg, err := postgres.NewClient(postgres.ClientConfig{
			Host: host, Port: port, Database: dbName, User: user, Password: password,
			SSLMode: envOr("POSTGRES_SSLMODE", "disable"), ConnectTimeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := pg.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pg.Close() })
		return pg
	}
	pg := newClient(user, password)

	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, dbName, user, password, envOr("POSTGRES_SSLMODE", "disable")))
	if err != nil {
		t.Fatal(err)
	}

	const (
		schema    = "pgk_settings"
		plainRole = "pgk_it_plain"
	)
	cleanup := func() {
		bg := context.Background()
		_, _ = conn.Exec(bg, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		_, _ = conn.Exec(bg, "DROP ROLE IF EXISTS "+plainRole)
		_, _ = conn.Exec(bg, "ALTER SYSTEM RESET shared_buffers")
		_, _ = conn.Exec(bg, "SELECT pg_reload_conf()")
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		conn.Close(context.Background())
	})

	var sbSetting string
	if err := conn.QueryRow(ctx, "SELECT setting FROM pg_settings WHERE name = 'shared_buffers'").Scan(&sbSetting); err != nil {
		t.Fatal(err)
	}
	sbPages, _ := strconv.Atoi(sbSetting)
	for _, stmt := range []string{
		"CREATE SCHEMA " + schema,
		"CREATE TABLE " + schema + ".bulk (id int)",
		"ALTER TABLE " + schema + ".bulk SET (autovacuum_enabled = off)",
		"CREATE TABLE " + schema + ".normal (id int)",
		// A changed postmaster setting shows as pending until a restart.
		fmt.Sprintf("ALTER SYSTEM SET shared_buffers = '%dkB'", (sbPages+128)*8),
		"SELECT pg_reload_conf()",
		"CREATE ROLE " + plainRole + " LOGIN PASSWORD 'plain'",
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	// pg_reload_conf only signals; give the postmaster a moment to apply it.
	time.Sleep(500 * time.Millisecond)

	var pgssMax int64
	if err := conn.QueryRow(ctx, "SELECT COALESCE(current_setting('pg_stat_statements.max', true), '0')::bigint").Scan(&pgssMax); err != nil {
		t.Fatal(err)
	}
	evicting := pgssMax > 0 && pgssMax <= 500
	if evicting {
		// Constants and aliases are normalized away, so vary the number of
		// target columns to get a distinct entry per statement.
		for i := int64(1); i <= pgssMax+50; i++ {
			if _, err := conn.Exec(ctx, "SELECT 1"+strings.Repeat(", 1", int(i))); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		t.Logf("pg_stat_statements.max = %d; start the server with -c pg_stat_statements.max=100 to check evictions", pgssMax)
	}

	got, err := pg.GetServerSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unavailable) != 0 {
		t.Errorf("checks unavailable as superuser: %v", got.Unavailable)
	}
	for _, name := range []string{"shared_buffers", "work_mem", "autovacuum", "statement_timeout", "random_page_cost", "pg_stat_statements.max"} {
		if _, ok := got.Get(name); !ok {
			t.Errorf("setting %s not collected", name)
		}
	}
	if sb, _ := got.Get("shared_buffers"); !sb.PendingRestart || !sb.NeedsRestart() {
		t.Errorf("shared_buffers = %+v, want pending restart", sb)
	}
	if wm, _ := got.Get("work_mem"); func() bool { b, ok := wm.Bytes(); return !ok || b <= 0 }() {
		t.Errorf("work_mem not parsed: %+v", wm)
	}
	var disabled []string
	for _, tbl := range got.AutovacuumDisabled {
		if tbl.SchemaName == schema {
			disabled = append(disabled, tbl.RelName)
		}
	}
	if len(disabled) != 1 || disabled[0] != "bulk" {
		t.Errorf("autovacuum disabled = %v, want [bulk]", disabled)
	}
	u := got.StatStatements
	if u == nil || u.Max != pgssMax || u.Entries <= 0 {
		t.Fatalf("stat statements = %+v", u)
	}
	if evicting && (u.Dealloc == 0 || float64(u.Entries) < 0.9*float64(u.Max)) {
		t.Errorf("expected evictions with max %d: %+v", pgssMax, u)
	}

	// A role without extra privileges can still run every check.
	plain, err := newClient(plainRole, "plain").GetServerSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Unavailable) != 0 || plain.StatStatements == nil {
		t.Errorf("unprivileged role: unavailable %v, stat statements %+v", plain.Unavailable, plain.StatStatements)
	}

	// Collection -> analysis -> rules.
	storage, err := sqlite.NewStorage(t.TempDir() + "/settings.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "settings", Host: host, Port: port, Database: dbName})
	if err != nil {
		t.Fatal(err)
	}
	coord := collector.NewCoordinator(collector.CoordinatorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID})
	coord.RegisterCollectors(settings.NewCollector(settings.Config{PGClient: pg, Storage: storage, InstanceID: instanceID}))
	collected, err := coord.CollectAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if collected.HasErrors() {
		t.Fatal(collected.Error())
	}

	analyzerConfig := analyzer.DefaultConfig()
	analyzerConfig.CollectorIntervals = coord.CollectorIntervals()
	analysis, err := analyzer.NewMainAnalyzer(storage, analyzerConfig).Analyze(ctx, collected.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.DomainsUsable(analyzer.DomainSettings) || analysis.Settings == nil {
		t.Fatalf("settings not analysed: errors=%v coverage=%+v", analysis.Errors, analysis.Coverage)
	}

	suggestConfig := suggester.DefaultConfig()
	sug := suggester.NewSuggester(storage, suggestConfig, nil)
	sug.RegisterRules(
		rules.NewConfigurationRule(suggestConfig),
		rules.NewStatStatementsCapacityRule(suggestConfig),
	)
	res, err := sug.Suggest(ctx, analysis)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("suggest errors: %v", res.Errors)
	}
	active, err := storage.GetSuggestionsByStatus(ctx, instanceID, models.StatusActive)
	if err != nil {
		t.Fatal(err)
	}
	bySeverity := make(map[string]string)
	for _, s := range active {
		bySeverity[s.RuleID+" "+s.TargetObject] = s.Severity
	}
	want := map[string]string{
		"configuration autovacuum_disabled:" + schema + ".bulk": models.SeverityWarning,
		"configuration setting:pending_restart":                 models.SeverityInfo,
		// The postgres image leaves these at their defaults.
		"configuration setting:statement_timeout": models.SeverityInfo,
		"configuration setting:random_page_cost":  models.SeverityInfo,
		"configuration setting:observability":     models.SeverityInfo,
	}
	if evicting {
		want["stat_statements_capacity setting:pg_stat_statements.max"] = models.SeverityWarning
	}
	for key, sev := range want {
		if bySeverity[key] != sev {
			t.Errorf("%s = %q, want %q; active: %v", key, bySeverity[key], sev, bySeverity)
		}
	}
	if _, ok := bySeverity["configuration setting:autovacuum"]; ok {
		t.Errorf("autovacuum is on, but was reported: %v", bySeverity)
	}
}
