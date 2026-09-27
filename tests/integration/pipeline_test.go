//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/api"
	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/collector/query"
	"github.com/elqsar/pganalyzer/internal/collector/resource"
	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/scheduler"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

// TestPipeline uses the same public components as main against a real PostgreSQL
// server. Set POSTGRES_HOST to opt in; once opted in, connection or collection
// failures fail the test instead of silently skipping it.
func TestPipeline(t *testing.T) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		t.Skip("set POSTGRES_HOST to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cfg := config.Default()
	cfg.Postgres.Host = host
	cfg.Postgres.Database = envOr("POSTGRES_DATABASE", "testdb")
	cfg.Postgres.User = envOr("POSTGRES_USER", "postgres")
	cfg.Postgres.Password = envOr("POSTGRES_PASSWORD", "postgres")
	cfg.Postgres.SSLMode = envOr("POSTGRES_SSLMODE", "disable")
	port, err := strconv.Atoi(envOr("POSTGRES_PORT", "5432"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Postgres.Port = port
	cfg.Server.Auth.Enabled = false
	cfg.Logging.Requests = false

	pg, err := postgres.NewClient(postgres.ClientConfig{
		Host: host, Port: port, Database: cfg.Postgres.Database,
		User: cfg.Postgres.User, Password: cfg.Postgres.Password,
		SSLMode: cfg.Postgres.SSLMode, ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pg.Close() })
	if err := pg.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.GetStatStatements(ctx); err != nil {
		t.Fatalf("pg_stat_statements must be available: %v", err)
	}

	storage, err := sqlite.NewStorage(t.TempDir() + "/integration.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { storage.Close() })
	instanceID, err := storage.CreateInstance(ctx, &models.Instance{
		Name: "integration", Host: host, Port: port, Database: cfg.Postgres.Database,
	})
	if err != nil {
		t.Fatal(err)
	}
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient: pg, Storage: storage, InstanceID: instanceID,
		SnapshotWindow: cfg.Scheduler.SnapshotInterval.Duration(),
	})
	coord.RegisterCollectors(
		query.NewStatsCollector(query.StatsCollectorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID}),
		resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID}),
		resource.NewIndexStatsCollector(resource.IndexStatsCollectorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID}),
		resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{PGClient: pg, Storage: storage, InstanceID: instanceID}),
	)
	collected, err := coord.CollectAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if collected.HasErrors() {
		t.Fatal(collected.Error())
	}
	if collected.SnapshotID == 0 {
		t.Fatal("collection did not create a snapshot")
	}
	runs, err := storage.GetSnapshotCollectors(ctx, collected.SnapshotID)
	if err != nil || len(runs) != 4 {
		t.Fatalf("expected four successful collector runs, got %d: %v", len(runs), err)
	}

	analyzerConfig := analyzer.ConfigFromThresholds(cfg.Thresholds)
	analyzerConfig.CollectorIntervals = coord.CollectorIntervals()
	mainAnalyzer := analyzer.NewMainAnalyzer(storage, analyzerConfig)
	analysis, err := mainAnalyzer.Analyze(ctx, collected.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ErrorCount != 0 || !analysis.DomainsUsable(analyzer.DomainQueryStats, analyzer.DomainDatabaseStats) || analysis.CacheStats == nil {
		t.Fatalf("incomplete analysis: errors=%v coverage=%v runs=%+v", analysis.Errors, analysis.Coverage, runs)
	}
	suggestConfig := suggester.DefaultConfig()
	sug := suggester.NewSuggester(storage, suggestConfig, nil)
	sug.RegisterRules(rules.NewSlowQueryRule(suggestConfig), rules.NewCacheRule(suggestConfig))
	if _, err := sug.Suggest(ctx, analysis); err != nil {
		t.Fatal(err)
	}
	sched, err := scheduler.NewScheduler(scheduler.Config{
		SchedulerConfig: &cfg.Scheduler, RetentionConfig: &cfg.Storage.Retention,
		Coordinator: coord, Analyzer: mainAnalyzer, Suggester: sug,
		Storage: storage, InstanceID: instanceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := api.NewServer(api.ServerConfig{
		Config: &cfg.Server, Storage: storage, PGClient: pg,
		Scheduler: sched, InstanceID: instanceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/health", "/api/v1/dashboard", "/api/v1/queries"} {
		response := httptest.NewRecorder()
		server.Echo().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("GET %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	stats, err := storage.GetQueryStats(ctx, collected.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Fatal("pg_stat_statements returned no query samples")
	}
	path := "/api/v1/queries/" + strconv.FormatInt(stats[0].QueryID, 10) + "/history"
	response := httptest.NewRecorder()
	server.Echo().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
	}
	var history struct {
		Samples []json.RawMessage `json:"samples"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil || len(history.Samples) == 0 {
		t.Fatalf("expected query history, got %s: %v", response.Body.String(), err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
