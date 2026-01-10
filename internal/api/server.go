package api

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"

	"github.com/user/pganalyzer/internal/api/handlers"
	"github.com/user/pganalyzer/internal/api/middleware"
	"github.com/user/pganalyzer/internal/config"
	"github.com/user/pganalyzer/internal/postgres"
	"github.com/user/pganalyzer/internal/scheduler"
	"github.com/user/pganalyzer/internal/storage/sqlite"
	"github.com/user/pganalyzer/internal/web"
)

// Server represents the HTTP API server.
type Server struct {
	echo       *echo.Echo
	config     *config.ServerConfig
	storage    sqlite.Storage
	pgClient   postgres.Client
	scheduler  *scheduler.Scheduler
	instanceID int64
	logger     *log.Logger
	version    string
}

// ServerConfig holds configuration for creating a Server.
type ServerConfig struct {
	Config     *config.ServerConfig
	Storage    sqlite.Storage
	PGClient   postgres.Client
	Scheduler  *scheduler.Scheduler
	InstanceID int64
	Logger     *log.Logger
	Version    string
}

// NewServer creates a new API server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Config == nil {
		return nil, fmt.Errorf("server config is required")
	}
	if cfg.Storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if cfg.PGClient == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	if cfg.Scheduler == nil {
		return nil, fmt.Errorf("scheduler is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Set up template renderer
	renderer, err := web.NewTemplateRenderer()
	if err != nil {
		return nil, fmt.Errorf("failed to create template renderer: %w", err)
	}
	e.Renderer = renderer

	// Set up static file serving from embedded filesystem
	staticFS, err := fs.Sub(web.StaticFS(), "static")
	if err != nil {
		return nil, fmt.Errorf("failed to create static filesystem: %w", err)
	}
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))))

	// Set custom error handler
	e.HTTPErrorHandler = CustomHTTPErrorHandler

	// Configure middleware
	e.Use(echomiddleware.Recover())
	e.Use(echomiddleware.RequestID())
	e.Use(echomiddleware.LoggerWithConfig(echomiddleware.LoggerConfig{
		Format: "${time_rfc3339} ${method} ${uri} ${status} ${latency_human}\n",
	}))
	e.Use(echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	// Apply Basic Auth middleware
	e.Use(middleware.BasicAuth(cfg.Config.Auth))

	version := cfg.Version
	if version == "" {
		version = "dev"
	}

	server := &Server{
		echo:       e,
		config:     cfg.Config,
		storage:    cfg.Storage,
		pgClient:   cfg.PGClient,
		scheduler:  cfg.Scheduler,
		instanceID: cfg.InstanceID,
		logger:     logger,
		version:    version,
	}

	// Register routes
	server.registerRoutes()

	return server, nil
}

// registerRoutes sets up all API routes.
func (s *Server) registerRoutes() {
	// Create handlers
	healthHandler := handlers.NewHealthHandler(s.storage, s.pgClient, s.scheduler, s.instanceID)
	dashboardHandler := handlers.NewDashboardHandler(s.storage, s.instanceID)
	queriesHandler := handlers.NewQueriesHandler(s.storage, s.pgClient, s.instanceID)
	schemaHandler := handlers.NewSchemaHandler(s.storage, s.instanceID)
	suggestionsHandler := handlers.NewSuggestionsHandler(s.storage, s.instanceID)
	snapshotsHandler := handlers.NewSnapshotsHandler(s.storage, s.scheduler, s.instanceID)
	pageHandler := handlers.NewPageHandler(s.storage, s.instanceID, s.version)

	// Health endpoint (no auth required - handled in middleware)
	s.echo.GET("/health", healthHandler.GetHealth)

	// Web UI routes (HTML pages)
	s.echo.GET("/", pageHandler.Dashboard)
	s.echo.GET("/queries", pageHandler.Queries)
	s.echo.GET("/queries/:id", pageHandler.QueryDetail)
	s.echo.GET("/schema", pageHandler.Schema)
	s.echo.GET("/suggestions", pageHandler.Suggestions)

	// API v1 routes
	apiV1 := s.echo.Group("/api/v1")

	// Dashboard
	apiV1.GET("/dashboard", dashboardHandler.GetDashboard)

	// Queries
	apiV1.GET("/queries", queriesHandler.ListQueries)
	apiV1.GET("/queries/top", queriesHandler.GetTopQueries)
	apiV1.POST("/queries/:id/explain", queriesHandler.ExplainQuery)

	// Schema
	apiV1.GET("/schema/tables", schemaHandler.GetTables)
	apiV1.GET("/schema/indexes", schemaHandler.GetIndexes)
	apiV1.GET("/schema/bloat", schemaHandler.GetBloat)

	// Suggestions
	apiV1.GET("/suggestions", suggestionsHandler.ListSuggestions)
	apiV1.POST("/suggestions/:id/dismiss", suggestionsHandler.DismissSuggestion)

	// Snapshots
	apiV1.GET("/snapshots", snapshotsHandler.ListSnapshots)
	apiV1.POST("/snapshots", snapshotsHandler.TriggerSnapshot)
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.logger.Printf("[api] starting server on %s", addr)
	return s.echo.Start(addr)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Printf("[api] shutting down server...")
	return s.echo.Shutdown(ctx)
}

// ShutdownWithTimeout shuts down the server with a timeout.
func (s *Server) ShutdownWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.Shutdown(ctx)
}

// Echo returns the underlying Echo instance for testing.
func (s *Server) Echo() *echo.Echo {
	return s.echo
}
