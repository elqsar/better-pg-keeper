//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/user/pganalyzer/internal/analyzer"
	"github.com/user/pganalyzer/internal/collector"
	"github.com/user/pganalyzer/internal/collector/query"
	"github.com/user/pganalyzer/internal/collector/resource"
	"github.com/user/pganalyzer/internal/models"
	"github.com/user/pganalyzer/internal/suggester"
	"github.com/user/pganalyzer/internal/suggester/rules"
)

func TestAnalysisAfterCollection(t *testing.T) {
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

	// Create coordinator and register collectors
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
		resource.NewTableStatsCollector(baseConfig, nil),
		resource.NewIndexStatsCollector(baseConfig, nil),
		resource.NewDatabaseStatsCollector(baseConfig, nil),
	)

	// Generate activity and collect
	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	result, err := coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collection failed: %v", err)
	}

	// Create analyzer
	mainAnalyzer := analyzer.NewMainAnalyzer(env.Storage, analyzer.ConfigFromThresholds(&env.Config.Thresholds))

	// Run analysis
	analysisResult, err := mainAnalyzer.Analyze(ctx, result.SnapshotID)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Verify analysis completed
	if analysisResult.SnapshotID != result.SnapshotID {
		t.Errorf("Expected snapshot ID %d, got %d", result.SnapshotID, analysisResult.SnapshotID)
	}

	t.Logf("Analysis completed: slow_queries=%d, cache_stats=%v, table_issues=%d, index_issues=%d, errors=%d",
		len(analysisResult.SlowQueries),
		analysisResult.CacheStats != nil,
		len(analysisResult.TableIssues),
		len(analysisResult.IndexIssues),
		analysisResult.ErrorCount,
	)
}

func TestSuggestionGeneration(t *testing.T) {
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

	// Create coordinator and collect
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
		resource.NewTableStatsCollector(baseConfig, nil),
		resource.NewIndexStatsCollector(baseConfig, nil),
	)

	if err := generateQueryActivity(ctx, env.PGClient); err != nil {
		t.Fatalf("Failed to generate query activity: %v", err)
	}

	collectResult, err := coord.Collect(ctx)
	if err != nil {
		t.Fatalf("Collection failed: %v", err)
	}

	// Analyze
	mainAnalyzer := analyzer.NewMainAnalyzer(env.Storage, analyzer.ConfigFromThresholds(&env.Config.Thresholds))
	analysisResult, err := mainAnalyzer.Analyze(ctx, collectResult.SnapshotID)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Create suggester with all rules
	sug := suggester.New(env.Storage, instanceID)
	sug.RegisterRules(
		rules.NewSlowQueryRule(suggester.DefaultConfig()),
		rules.NewUnusedIndexRule(suggester.DefaultConfig()),
		rules.NewMissingIndexRule(suggester.DefaultConfig()),
		rules.NewBloatRule(suggester.DefaultConfig()),
		rules.NewVacuumRule(suggester.DefaultConfig()),
		rules.NewCacheRule(suggester.DefaultConfig()),
	)

	// Generate suggestions
	suggestResult, err := sug.Suggest(ctx, analysisResult)
	if err != nil {
		t.Fatalf("Suggestion generation failed: %v", err)
	}

	t.Logf("Suggestions: total=%d, new=%d, updated=%d, resolved=%d",
		suggestResult.Total,
		suggestResult.New,
		suggestResult.Updated,
		suggestResult.Resolved,
	)

	// Get suggestion stats
	stats, err := sug.GetSuggestionStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get suggestion stats: %v", err)
	}

	t.Logf("Suggestion stats: critical=%d, warning=%d, info=%d",
		stats["critical"],
		stats["warning"],
		stats["info"],
	)
}

func TestFullPipeline(t *testing.T) {
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

	// Setup full pipeline
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
		resource.NewTableStatsCollector(baseConfig, nil),
		resource.NewIndexStatsCollector(baseConfig, nil),
		resource.NewDatabaseStatsCollector(baseConfig, nil),
	)

	mainAnalyzer := analyzer.NewMainAnalyzer(env.Storage, analyzer.ConfigFromThresholds(&env.Config.Thresholds))

	sug := suggester.New(env.Storage, instanceID)
	sug.RegisterRules(
		rules.NewSlowQueryRule(suggester.DefaultConfig()),
		rules.NewUnusedIndexRule(suggester.DefaultConfig()),
		rules.NewMissingIndexRule(suggester.DefaultConfig()),
		rules.NewBloatRule(suggester.DefaultConfig()),
		rules.NewVacuumRule(suggester.DefaultConfig()),
		rules.NewCacheRule(suggester.DefaultConfig()),
	)

	// Run multiple cycles
	for i := 0; i < 2; i++ {
		// Generate activity
		if err := generateQueryActivity(ctx, env.PGClient); err != nil {
			t.Fatalf("Cycle %d: failed to generate activity: %v", i, err)
		}

		// Collect
		collectResult, err := coord.Collect(ctx)
		if err != nil {
			t.Fatalf("Cycle %d: collection failed: %v", i, err)
		}

		// Analyze
		analysisResult, err := mainAnalyzer.Analyze(ctx, collectResult.SnapshotID)
		if err != nil {
			t.Fatalf("Cycle %d: analysis failed: %v", i, err)
		}

		// Generate suggestions
		_, err = sug.Suggest(ctx, analysisResult)
		if err != nil {
			t.Fatalf("Cycle %d: suggestion failed: %v", i, err)
		}

		t.Logf("Cycle %d completed: snapshot=%d, issues=%d",
			i, collectResult.SnapshotID, analysisResult.GetIssueCount())

		time.Sleep(100 * time.Millisecond)
	}

	// Verify data is stored
	snapshots, err := env.Storage.ListSnapshots(ctx, instanceID, 10)
	if err != nil {
		t.Fatalf("Failed to list snapshots: %v", err)
	}
	if len(snapshots) < 2 {
		t.Errorf("Expected at least 2 snapshots, got %d", len(snapshots))
	}

	// Verify suggestions can be retrieved
	suggestions, err := env.Storage.GetActiveSuggestions(ctx, instanceID)
	if err != nil {
		t.Fatalf("Failed to get suggestions: %v", err)
	}
	t.Logf("Total active suggestions: %d", len(suggestions))
}
