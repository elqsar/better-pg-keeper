//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/collector"
	plancollector "github.com/elqsar/pganalyzer/internal/collector/plans"
	"github.com/elqsar/pganalyzer/internal/collector/query"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

// TestIndexAdvice runs a query that seq-scans a large table and checks the
// advisor proposes the right index, without executing anything it plans.
// If the hypopg extension is available on the server, the proposal is also
// validated against the planner.
func TestIndexAdvice(t *testing.T) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		t.Skip("set POSTGRES_HOST to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	port, err := strconv.Atoi(envOr("POSTGRES_PORT", "5432"))
	if err != nil {
		t.Fatal(err)
	}
	dbName := envOr("POSTGRES_DATABASE", "testdb")
	pg, err := postgres.NewClient(postgres.ClientConfig{
		Host: host, Port: port, Database: dbName,
		User: envOr("POSTGRES_USER", "postgres"), Password: envOr("POSTGRES_PASSWORD", "postgres"),
		SSLMode: envOr("POSTGRES_SSLMODE", "disable"), ConnectTimeout: 5 * time.Second,
		MaxConnections: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close() })

	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, dbName, envOr("POSTGRES_USER", "postgres"),
		envOr("POSTGRES_PASSWORD", "postgres"), envOr("POSTGRES_SSLMODE", "disable")))
	if err != nil {
		t.Fatal(err)
	}
	const schema = "pgk_index_advice"
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		conn.Close(context.Background())
	})
	for _, stmt := range []string{
		"DROP SCHEMA IF EXISTS " + schema + " CASCADE",
		"CREATE SCHEMA " + schema,
		"CREATE TABLE " + schema + ".orders (id bigserial PRIMARY KEY, status text NOT NULL, created_at timestamptz NOT NULL, note text)",
		"INSERT INTO " + schema + ".orders (status, created_at, note) SELECT 's' || (g % 50), now() - (g || ' minutes')::interval, 'x' FROM generate_series(1, 200000) g",
		"ANALYZE " + schema + ".orders",
		"SELECT pg_stat_statements_reset()",
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	const busy = "SELECT id FROM " + schema + ".orders WHERE status = $1 AND created_at > $2"
	for i := 0; i < 30; i++ {
		if _, err := conn.Exec(ctx, busy, fmt.Sprintf("s%d", i%50), time.Now().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	// ExplainGeneric plans normalized text and never executes it.
	var before int64
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+schema+".orders WHERE note = 'x'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ { // more runs than pool connections: nothing may leak between them
		if _, err := pg.ExplainGeneric(ctx, "UPDATE "+schema+".orders SET note = $1 WHERE status = $2"); err != nil {
			t.Fatalf("explaining UPDATE: %v", err)
		}
	}
	var after int64
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+schema+".orders WHERE note = 'x'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("ExplainGeneric executed the UPDATE: %d rows with note 'x', had %d", after, before)
	}
	if _, err := pg.ExplainGeneric(ctx, "VACUUM "+schema+".orders"); err == nil {
		t.Error("utility statements must be refused")
	}

	// hypopg, when the server has it.
	var hypoAvailable bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'hypopg')").Scan(&hypoAvailable); err != nil {
		t.Fatal(err)
	}
	if hypoAvailable {
		if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS hypopg"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = conn.Exec(context.Background(), "DROP EXTENSION IF EXISTS hypopg") })
	} else {
		t.Log("hypopg not available on the server; skipping validation checks")
	}

	storage, err := sqlite.NewStorage(t.TempDir() + "/advice.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{Name: "advice", Host: host, Port: port, Database: dbName})
	if err != nil {
		t.Fatal(err)
	}
	coord := collector.NewCoordinator(collector.CoordinatorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID})
	coord.RegisterCollectors(query.NewStatsCollector(query.StatsCollectorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID}))
	if res, err := coord.CollectAll(ctx); err != nil || res.HasErrors() {
		t.Fatalf("query stats: %v %v", err, res.Error())
	}
	// Plans need the query statistics stored first, as in production where the
	// hourly plan collector runs long after the first query collection.
	coord.RegisterCollector(plancollector.NewCollector(plancollector.Config{PGClient: pg, Planner: pg, Storage: storage, InstanceID: instanceID}))
	collected, err := coord.CollectAll(ctx)
	if err != nil || collected.HasErrors() {
		t.Fatalf("plans: %v %v", err, collected.Error())
	}

	report, err := storage.GetQueryPlans(ctx, collected.SnapshotID)
	if err != nil || report == nil {
		t.Fatalf("report = %v, %v", report, err)
	}
	if report.HypoPG != hypoAvailable {
		t.Errorf("HypoPG = %v, want %v", report.HypoPG, hypoAvailable)
	}
	var finding *models.QueryPlanFinding
	for i, q := range report.Queries {
		if strings.Contains(q.Query, schema+".orders WHERE status") {
			finding = &report.Queries[i]
		}
	}
	if finding == nil {
		t.Fatalf("busy query not planned: %+v", report.Queries)
	}
	if finding.Error != "" || len(finding.SeqScans) != 1 {
		t.Fatalf("finding = %+v", finding)
	}
	scan := finding.SeqScans[0]
	if !reflect.DeepEqual(scan.Columns, []string{"status", "created_at"}) || scan.SkipReason != "" || scan.EstimatedRows < 150000 {
		t.Fatalf("scan = %+v", scan)
	}
	if hypoAvailable {
		v := scan.Validation
		if v == nil || !v.UsesIndex || v.CostAfter >= v.CostBefore {
			t.Errorf("hypopg validation = %+v", v)
		}
		var hypoLeft int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE schemaname = $1 AND indexname <> 'orders_pkey'", schema).Scan(&hypoLeft); err != nil {
			t.Fatal(err)
		}
		if hypoLeft != 0 {
			t.Errorf("validation must not build a real index, found %d", hypoLeft)
		}
	}
	if plan, err := storage.GetExplainPlan(ctx, finding.QueryID); err != nil || plan == nil {
		t.Errorf("plan not kept for the query page: %v", err)
	}

	analyzerConfig := analyzer.DefaultConfig()
	analyzerConfig.CollectorIntervals = coord.CollectorIntervals()
	analysis, err := analyzer.NewMainAnalyzer(storage, analyzerConfig).Analyze(ctx, collected.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	sugCfg := suggester.DefaultConfig()
	sug := suggester.NewSuggester(storage, sugCfg, nil)
	sug.RegisterRules(rules.NewIndexRecommendationRule(sugCfg))
	if _, err := sug.Suggest(ctx, analysis); err != nil {
		t.Fatal(err)
	}
	active, err := storage.GetSuggestionsByStatus(ctx, instanceID, models.StatusActive)
	if err != nil {
		t.Fatal(err)
	}
	var rec *models.Suggestion
	for i := range active {
		if active[i].TargetObject == "index:"+schema+".orders(status,created_at)" {
			rec = &active[i]
		}
	}
	if rec == nil {
		t.Fatalf("no index recommendation; active: %+v", active)
	}
	wantDDL := `CREATE INDEX CONCURRENTLY "idx_orders_status_created_at" ON "` + schema + `"."orders" ("status", "created_at");`
	if !strings.Contains(rec.Description, wantDDL) || !strings.Contains(rec.Description, fmt.Sprintf("/queries/%d", finding.QueryID)) {
		t.Errorf("description:\n%s", rec.Description)
	}

	// The DDL it proposes must be valid SQL that PostgreSQL accepts.
	if _, err := conn.Exec(ctx, strings.TrimSuffix(wantDDL, ";")); err != nil {
		t.Fatalf("proposed DDL failed: %v", err)
	}
	// Once the index exists, the next run retires the proposal even though the
	// plan itself is reused.
	collected, err = coord.CollectAll(ctx)
	if err != nil || collected.HasErrors() {
		t.Fatalf("second run: %v %v", err, collected.Error())
	}
	report, err = storage.GetQueryPlans(ctx, collected.SnapshotID)
	if err != nil || report == nil {
		t.Fatalf("second report = %v, %v", report, err)
	}
	for _, q := range report.Queries {
		if q.QueryID != finding.QueryID {
			continue
		}
		if len(q.SeqScans) != 1 || !strings.Contains(q.SeqScans[0].SkipReason, "idx_orders_status_created_at already covers") {
			t.Errorf("proposal not retired after the index was built: %+v", q.SeqScans)
		}
	}
}
