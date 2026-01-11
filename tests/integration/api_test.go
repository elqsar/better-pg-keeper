//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/api"
	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/collector/query"
	"github.com/user/pganalyzer/internal/collector/resource"
	"github.com/user/pganalyzer/internal/models"
	"github.com/user/pganalyzer/internal/scheduler"
	"github.com/user/pganalyzer/internal/suggester"
	"github.com/user/pganalyzer/internal/suggester/rules"
)

func TestAPIHealthEndpoint(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Create API server
	server, err := api.NewServer(api.ServerConfig{
		Storage:    env.Storage,
		PGClient:   env.PGClient,
		InstanceID: instanceID,
		Config:     env.Config,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test health endpoint
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	server.Echo().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var healthResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if status, ok := healthResp["status"].(string); !ok || status != "ok" {
		t.Errorf("Expected status 'ok', got %v", healthResp["status"])
	}

	t.Logf("Health check response: %v", healthResp)
}

func TestAPIDashboardEndpoint(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Collect some data first
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	})

	baseConfig := collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}

	coord.RegisterCollectors(
		query.NewStatsCollector(baseConfig, nil),
		resource.NewDatabaseStatsCollector(baseConfig, nil),
	)

	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	_, err = coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collection failed: %v", err)
	}

	// Create API server
	server, err := api.NewServer(api.ServerConfig{
		Storage:    env.Storage,
		PGClient:   env.PGClient,
		InstanceID: instanceID,
		Config:     env.Config,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test dashboard endpoint (with auth if configured)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	if env.Config.Server.Username != "" {
		req.SetBasicAuth(env.Config.Server.Username, env.Config.Server.Password)
	}
	rec := httptest.NewRecorder()
	server.Echo().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var dashResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &dashResp); err != nil {
		t.Fatalf("Failed to parse dashboard response: %v", err)
	}

	t.Logf("Dashboard response: cache_hit_ratio=%v, total_queries=%v",
		dashResp["cache_hit_ratio"], dashResp["total_queries"])
}

func TestAPIQueriesEndpoint(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Collect query data
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	})

	coord.RegisterCollector(query.NewStatsCollector(collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}, nil))

	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	_, err = coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collection failed: %v", err)
	}

	// Create API server
	server, err := api.NewServer(api.ServerConfig{
		Storage:    env.Storage,
		PGClient:   env.PGClient,
		InstanceID: instanceID,
		Config:     env.Config,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test queries endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/v1/queries?limit=10&sort=total_time", nil)
	if env.Config.Server.Username != "" {
		req.SetBasicAuth(env.Config.Server.Username, env.Config.Server.Password)
	}
	rec := httptest.NewRecorder()
	server.Echo().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var queriesResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &queriesResp); err != nil {
		t.Fatalf("Failed to parse queries response: %v", err)
	}

	queries, ok := queriesResp["queries"].([]interface{})
	if !ok {
		t.Fatalf("Expected queries array in response")
	}

	t.Logf("Queries endpoint returned %d queries", len(queries))
}

func TestAPISchemaEndpoints(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Collect schema data
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	})

	baseConfig := collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}

	coord.RegisterCollectors(
		resource.NewTableStatsCollector(baseConfig, nil),
		resource.NewIndexStatsCollector(baseConfig, nil),
	)

	_, err = coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collection failed: %v", err)
	}

	// Create API server
	server, err := api.NewServer(api.ServerConfig{
		Storage:    env.Storage,
		PGClient:   env.PGClient,
		InstanceID: instanceID,
		Config:     env.Config,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Test tables endpoint
	t.Run("tables", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/schema/tables", nil)
		if env.Config.Server.Username != "" {
			req.SetBasicAuth(env.Config.Server.Username, env.Config.Server.Password)
		}
		rec := httptest.NewRecorder()
		server.Echo().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		t.Logf("Tables response: %s", rec.Body.String())
	})

	// Test indexes endpoint
	t.Run("indexes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/schema/indexes", nil)
		if env.Config.Server.Username != "" {
			req.SetBasicAuth(env.Config.Server.Username, env.Config.Server.Password)
		}
		rec := httptest.NewRecorder()
		server.Echo().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}

		t.Logf("Indexes response: %s", rec.Body.String())
	})
}

func TestAPISnapshotTrigger(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Cleanup()

	ctx := context.Background()

	// Create instance
	instance := &models.Instance{
		Name:     "test-instance",
		Host:     env.Config.Postgres.Host,
		Port:     env.Config.Postgres.Port,
		Database: env.Config.Postgres.Database,
	}
	instanceID, err := env.Storage.CreateInstance(ctx, instance)
	if err != nil {
		t.Fatalf("Failed to create instance: %v", err)
	}

	// Setup coordinator
	coord := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	})

	baseConfig := collector.CollectorConfig{
		PGClient:   env.PGClient,
		Storage:    env.Storage,
		InstanceID: instanceID,
	}

	coord.RegisterCollector(query.NewStatsCollector(baseConfig, nil))

	// Setup analyzer
	mainAnalyzer := analyzer.NewMainAnalyzer(env.Storage, nil)

	// Setup suggester
	sug := suggester.New(env.Storage, instanceID)
	sug.RegisterRules(rules.NewSlowQueryRule(suggester.DefaultConfig()))

	// Setup scheduler
	sched := scheduler.New(scheduler.Config{
		Coordinator:      coord,
		Analyzer:         mainAnalyzer,
		Suggester:        sug,
		Storage:          env.Storage,
		SnapshotInterval: time.Minute,
		AnalysisInterval: 5 * time.Minute,
	})

	// Create API server with scheduler
	server, err := api.NewServer(api.ServerConfig{
		Storage:    env.Storage,
		PGClient:   env.PGClient,
		InstanceID: instanceID,
		Config:     env.Config,
		Scheduler:  sched,
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Generate activity
	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	// Test snapshot trigger endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", nil)
	if env.Config.Server.Username != "" {
		req.SetBasicAuth(env.Config.Server.Username, env.Config.Server.Password)
	}
	rec := httptest.NewRecorder()
	server.Echo().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Errorf("Expected status 200 or 201, got %d: %s", rec.Code, rec.Body.String())
	}

	t.Logf("Snapshot trigger response: %s", rec.Body.String())
}
