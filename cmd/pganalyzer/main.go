package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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
	)
	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("pganalyzer %s\n", version)
		fmt.Printf("  commit:     %s\n", commit)
		fmt.Printf("  build date: %s\n", buildDate)
		os.Exit(0)
	}

	// Setup structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

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

	// Run the application
	if err := run(ctx, *configPath); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}

	slog.Info("pganalyzer stopped")
}

// run contains the main application logic
func run(ctx context.Context, configPath string) error {
	// TODO: Load configuration
	// TODO: Initialize storage
	// TODO: Initialize PostgreSQL client
	// TODO: Initialize collectors
	// TODO: Initialize analyzer
	// TODO: Initialize suggester
	// TODO: Initialize scheduler
	// TODO: Initialize API server
	// TODO: Start services

	// Wait for shutdown signal
	<-ctx.Done()

	// TODO: Graceful shutdown of all services

	return nil
}
