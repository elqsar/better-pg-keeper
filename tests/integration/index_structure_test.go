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

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
)

// TestIndexStructure checks that index definitions collected from a real server
// drive duplicate detection and foreign-key protection correctly.
func TestIndexStructure(t *testing.T) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		t.Skip("set POSTGRES_HOST to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port, err := strconv.Atoi(envOr("POSTGRES_PORT", "5432"))
	if err != nil {
		t.Fatal(err)
	}
	pg, err := postgres.NewClient(postgres.ClientConfig{
		Host: host, Port: port, Database: envOr("POSTGRES_DATABASE", "testdb"),
		User: envOr("POSTGRES_USER", "postgres"), Password: envOr("POSTGRES_PASSWORD", "postgres"),
		SSLMode: envOr("POSTGRES_SSLMODE", "disable"), ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close() })

	const schema = "pgk_index_structure"
	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, envOr("POSTGRES_DATABASE", "testdb"), envOr("POSTGRES_USER", "postgres"),
		envOr("POSTGRES_PASSWORD", "postgres"), envOr("POSTGRES_SSLMODE", "disable")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		conn.Close(context.Background())
	})
	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		"SET search_path TO " + schema,
		"CREATE TABLE users (id int PRIMARY KEY)",
		"CREATE TABLE orders (id int PRIMARY KEY, user_id int REFERENCES users(id), status text, created_at timestamptz, total numeric)",
		"CREATE INDEX o_user ON orders (user_id)",
		"CREATE INDEX o_user_created ON orders (user_id, created_at)",
		"CREATE INDEX o_user_dup ON orders (user_id)",
		"CREATE INDEX o_status ON orders (status)",
		"CREATE INDEX o_status_partial ON orders (status) WHERE status = 'open'",
		"CREATE INDEX o_lower ON orders (lower(status))",
		"CREATE INDEX o_created_inc ON orders (created_at) INCLUDE (total)",
		"CREATE INDEX o_created_desc ON orders (created_at DESC)",
		"CREATE INDEX o_created_status ON orders (created_at, status)",
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	all, err := pg.GetStatIndexes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var stats []models.IndexStat
	byName := map[string]models.IndexStat{}
	for _, s := range all {
		if s.SchemaName == schema {
			stats = append(stats, s)
			byName[s.IndexRelName] = s
		}
	}
	if len(stats) != 11 {
		t.Fatalf("collected %d indexes in %s, want 11", len(stats), schema)
	}
	for _, name := range []string{"o_user", "o_user_created", "o_user_dup"} {
		if !byName[name].BacksForeignKey {
			t.Errorf("%s should back the user_id foreign key", name)
		}
	}
	for _, name := range []string{"o_status", "orders_pkey"} {
		if byName[name].BacksForeignKey {
			t.Errorf("%s does not back a foreign key", name)
		}
	}
	if s := byName["o_user"]; s.StatsSince == nil || s.AccessMethod != "btree" || s.IndexDef == "" {
		t.Errorf("o_user missing structure: %+v", s)
	}

	storage, err := sqlite.NewStorage(t.TempDir() + "/index.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "index-structure", Host: host, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	snapID, err := storage.CreateSnapshot(ctx, &models.Snapshot{InstanceID: instanceID, CapturedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveIndexStats(ctx, snapID, stats); err != nil {
		t.Fatal(err)
	}

	issues, err := analyzer.NewIndexAnalyzer(storage, nil).Analyze(ctx, snapID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]analyzer.IndexIssue{}
	for _, issue := range issues {
		if issue.IssueType == analyzer.IndexIssueDuplicate {
			got[issue.IndexName] = issue
		}
	}

	// o_user and o_user_dup are identical and both prefixes of o_user_created,
	// so both are covered by it. Nothing else is redundant: the partial,
	// expression, DESC and INCLUDE variants each serve different lookups.
	if len(got) != 2 {
		t.Fatalf("duplicates = %v, want o_user and o_user_dup", keys(got))
	}
	for _, name := range []string{"o_user", "o_user_dup"} {
		if issue, ok := got[name]; !ok || issue.DuplicateOf != "o_user_created" {
			t.Errorf("%s: got %+v, want covered by o_user_created", name, issue)
		}
	}
}

func keys(m map[string]analyzer.IndexIssue) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
