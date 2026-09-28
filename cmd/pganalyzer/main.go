package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elqsar/pganalyzer/internal/analyzer"
	"github.com/elqsar/pganalyzer/internal/api"
	"github.com/elqsar/pganalyzer/internal/collector"
	"github.com/elqsar/pganalyzer/internal/collector/activity"
	"github.com/elqsar/pganalyzer/internal/collector/locks"
	"github.com/elqsar/pganalyzer/internal/collector/plans"
	"github.com/elqsar/pganalyzer/internal/collector/query"
	"github.com/elqsar/pganalyzer/internal/collector/resource"
	"github.com/elqsar/pganalyzer/internal/collector/risk"
	"github.com/elqsar/pganalyzer/internal/collector/schema"
	"github.com/elqsar/pganalyzer/internal/collector/settings"
	"github.com/elqsar/pganalyzer/internal/config"
	"github.com/elqsar/pganalyzer/internal/logging"
	"github.com/elqsar/pganalyzer/internal/metrics"
	"github.com/elqsar/pganalyzer/internal/models"
	"github.com/elqsar/pganalyzer/internal/notifier"
	"github.com/elqsar/pganalyzer/internal/postgres"
	"github.com/elqsar/pganalyzer/internal/scheduler"
	"github.com/elqsar/pganalyzer/internal/storage/sqlite"
	"github.com/elqsar/pganalyzer/internal/suggester"
	"github.com/elqsar/pganalyzer/internal/suggester/rules"
)

// Build-time variables set via ldflags
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// Parse command-line flags
	var (
		showVersion = flag.Bool("version", false, "Print version information and exit")
		configPath  = flag.String("config", "configs/config.yaml", "Path to configuration file")
		notifyTest  = flag.Bool("notify-test", false, "Send a test message to the configured notification channels and exit")
	)
	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("pganalyzer %s\n", version)
		fmt.Printf("  commit:     %s\n", commit)
		fmt.Printf("  build date: %s\n", buildDate)
		os.Exit(0)
	}

	// Setup initial logger (will be reconfigured after loading config)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting pganalyzer",
		"version", version,
		"config", *configPath,
	)

	// Create root context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received shutdown signal", "signal", sig.String())
		cancel()
	}()

	if *notifyTest {
		if err := sendTestNotification(ctx, *configPath); err != nil {
			slog.Error("test notification failed", "error", err)
			os.Exit(1)
		}
		slog.Info("test notification sent")
		return
	}

	// Run the application
	if err := run(ctx, *configPath); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}

	slog.Info("pganalyzer stopped")
}

// run contains the main application logic
func run(ctx context.Context, configPath string) error {
	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Setup structured logging based on configuration
	logging.Setup(cfg.Logging)
	slog.Info("configuration loaded",
		"postgres_host", cfg.Postgres.Host,
		"postgres_db", cfg.Postgres.Database,
		"log_level", cfg.Logging.Level,
		"log_format", cfg.Logging.Format,
	)

	// Record build info for metrics
	metrics.RecordBuildInfo(version, commit, buildDate)

	// Initialize storage
	storage, err := sqlite.NewStorage(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("initializing storage: %w", err)
	}
	defer storage.Close()
	slog.Info("storage initialized", "path", cfg.Storage.Path)

	// Initialize PostgreSQL client
	pgClient, err := postgres.NewClient(postgres.ClientConfig{
		Host:     cfg.Postgres.Host,
		Port:     cfg.Postgres.Port,
		Database: cfg.Postgres.Database,
		User:     cfg.Postgres.User,
		Password: cfg.Postgres.Password,
		SSLMode:  cfg.Postgres.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("creating postgres client: %w", err)
	}
	if err := pgClient.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pgClient.Close()

	// Fail fast on an unsupported server rather than letting every collection cycle
	// die on a missing pg_stat_statements column.
	if err := pgClient.CheckVersionSupported(ctx); err != nil {
		return err
	}

	slog.Info("connected to PostgreSQL",
		"host", cfg.Postgres.Host,
		"database", cfg.Postgres.Database,
	)

	// Get or create instance record
	instanceName := fmt.Sprintf("%s:%d/%s", cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.Database)
	instanceID, err := storage.GetOrCreateInstance(ctx, &models.Instance{
		Name:     instanceName,
		Host:     cfg.Postgres.Host,
		Port:     cfg.Postgres.Port,
		Database: cfg.Postgres.Database,
	})
	if err != nil {
		return fmt.Errorf("getting/creating instance: %w", err)
	}
	slog.Info("instance ready", "id", instanceID, "name", instanceName)

	// Create coordinator and register collectors
	coordinator := collector.NewCoordinator(collector.CoordinatorConfig{
		PGClient:       pgClient,
		Storage:        storage,
		InstanceID:     instanceID,
		SnapshotWindow: cfg.Scheduler.SnapshotInterval.Duration(),
	})

	coordinator.RegisterCollectors(
		query.NewStatsCollector(query.StatsCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewTableStatsCollector(resource.TableStatsCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewIndexStatsCollector(resource.IndexStatsCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		resource.NewDatabaseStatsCollector(resource.DatabaseStatsCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		schema.NewBloatCollector(schema.BloatCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		activity.NewActivityCollector(activity.ActivityCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		locks.NewLocksCollector(locks.LocksCollectorConfig{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		risk.NewCollector(risk.Config{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
		settings.NewCollector(settings.Config{
			PGClient:   pgClient,
			Storage:    storage,
			InstanceID: instanceID,
		}),
	)
	if cfg.IndexAdvisor.Enabled {
		coordinator.RegisterCollector(plans.NewCollector(plans.Config{
			PGClient:     pgClient,
			Planner:      pgClient,
			Storage:      storage,
			InstanceID:   instanceID,
			MaxQueries:   cfg.IndexAdvisor.MaxQueries,
			MinTableRows: int64(cfg.Thresholds.MinTableSizeForIndex),
		}))
	}
	slog.Info("collectors registered", "count", len(coordinator.Collectors()))

	// Create analyzer. It needs the registered collectors' intervals to judge how
	// old a domain's data may get before its absence stops meaning "clean".
	analyzerCfg := analyzer.ConfigFromThresholds(cfg.Thresholds)
	analyzerCfg.CollectorIntervals = coordinator.CollectorIntervals()
	mainAnalyzer := analyzer.NewMainAnalyzer(storage, analyzerCfg)
	slog.Info("analyzer initialized", "staleness_factor", analyzerCfg.StalenessFactor)

	// Create suggester and register rules
	suggesterCfg := suggester.ConfigFromThresholds(cfg.Thresholds)
	mainSuggester := suggester.NewSuggester(storage, suggesterCfg, nil)
	mainSuggester.RegisterRules(
		rules.NewSlowQueryRule(suggesterCfg),
		rules.NewUnusedIndexRule(suggesterCfg),
		rules.NewDuplicateIndexRule(suggesterCfg),
		rules.NewMissingIndexRule(suggesterCfg),
		rules.NewIndexRecommendationRule(suggesterCfg),
		rules.NewBloatRule(suggesterCfg),
		rules.NewVacuumRule(suggesterCfg),
		rules.NewCacheRule(suggesterCfg),
		// Operational state rules
		rules.NewLongRunningQueryRule(suggesterCfg),
		rules.NewIdleInTransactionRule(suggesterCfg),
		rules.NewLockContentionRule(suggesterCfg),
		rules.NewHighTempUsageRule(suggesterCfg),
		rules.NewHighDeadlocksRule(suggesterCfg),
		// Outage-risk rules
		rules.NewXIDWraparoundRule(suggesterCfg),
		rules.NewMultixactWraparoundRule(suggesterCfg),
		rules.NewReplicationSlotRule(suggesterCfg),
		rules.NewSequenceExhaustionRule(suggesterCfg),
		rules.NewPreparedTransactionRule(suggesterCfg),
		rules.NewDiskGrowthRule(suggesterCfg),
		rules.NewConnectionSaturationRule(suggesterCfg),
		// Configuration review rules
		rules.NewConfigurationRule(suggesterCfg),
		rules.NewStatStatementsCapacityRule(suggesterCfg),
	)
	slog.Info("suggester initialized", "rules", len(mainSuggester.Rules()))

	// Create and start scheduler
	sched, err := scheduler.NewScheduler(scheduler.Config{
		SchedulerConfig: &cfg.Scheduler,
		RetentionConfig: &cfg.Storage.Retention,
		Coordinator:     coordinator,
		Analyzer:        mainAnalyzer,
		Suggester:       mainSuggester,
		Storage:         storage,
		InstanceID:      instanceID,
	})
	if err != nil {
		return fmt.Errorf("creating scheduler: %w", err)
	}
	if err := sched.Start(ctx); err != nil {
		return fmt.Errorf("starting scheduler: %w", err)
	}
	defer sched.Stop()
	slog.Info("scheduler started",
		"snapshot_interval", cfg.Scheduler.SnapshotInterval,
		"analysis_interval", cfg.Scheduler.AnalysisInterval,
	)

	if cfg.Notifications.Enabled {
		channels, err := notifier.NewChannels(cfg.Notifications.Channels, nil)
		if err != nil {
			return fmt.Errorf("creating notification channels: %w", err)
		}
		n, err := notifier.New(notifier.Options{
			Config:     cfg.Notifications,
			Storage:    storage,
			Collection: sched,
			Channels:   channels,
			InstanceID: instanceID,
			Instance:   instanceName,
		})
		if err != nil {
			return fmt.Errorf("creating notifier: %w", err)
		}
		go n.Run(ctx)
		slog.Info("notifications enabled",
			"channels", len(channels),
			"min_severity", cfg.Notifications.MinSeverity,
			"digest", cfg.Notifications.Digest.Enabled,
		)
	}

	// Create API server
	server, err := api.NewServer(api.ServerConfig{
		Config:        &cfg.Server,
		LoggingConfig: &cfg.Logging,
		MetricsConfig: &cfg.Metrics,
		Storage:       storage,
		PGClient:      pgClient,
		Scheduler:     sched,
		InstanceID:    instanceID,
		Version:       version,
	})
	if err != nil {
		return fmt.Errorf("creating api server: %w", err)
	}

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("starting HTTP server",
			"host", cfg.Server.Host,
			"port", cfg.Server.Port,
		)
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown
	slog.Info("initiating graceful shutdown")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("server shutdown error", "error", err)
	}

	return nil
}

// sendTestNotification checks the notification setup without connecting to
// PostgreSQL, so channels can be verified before deploying.
func sendTestNotification(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if !cfg.Notifications.Enabled {
		return fmt.Errorf("notifications.enabled is false in %s", configPath)
	}
	channels, err := notifier.NewChannels(cfg.Notifications.Channels, nil)
	if err != nil {
		return err
	}
	instance := fmt.Sprintf("%s:%d/%s", cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.Database)
	return notifier.SendTest(ctx, channels, instance, cfg.Notifications.DashboardURL)
}
