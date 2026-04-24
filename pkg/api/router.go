package api

import (
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-chi/chi/v5"
	chiMw "github.com/go-chi/chi/v5/middleware"
	"github.com/mockzilla/connexions/v2/internal/contexts"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/db"
	"github.com/mockzilla/connexions/v2/pkg/middleware"
	"github.com/mockzilla/connexions/v2/resources"
)

// Router is the central HTTP router for all services
type Router struct {
	chi.Router

	config   *config.AppConfig
	contexts []map[string]map[string]any
	storage  db.Storage

	mu        sync.RWMutex
	services  map[string]*ServiceItem
	databases map[string]db.DB
}

// Handler defines a minimal interface for request handlers.
type Handler interface {
	Routes() RouteDescriptions
	RegisterRoutes(router chi.Router)
	Generate(w http.ResponseWriter, r *http.Request)
}

// NewRouter creates a new central router with default middleware
func NewRouter(options ...RouterOption) *Router {
	r := chi.NewRouter()

	// create default replacement context
	ctxs := contexts.Load(map[string][]byte{
		"common": resources.CommonContextYAMLContents,
		"fake":   resources.FakeContextYAMLContents,
		"words":  resources.WordsContextYAMLContents,
	}, nil)
	defaultContexts := []map[string]map[string]any{
		{"common": ctxs["common"]},
		{"fake": ctxs["fake"]},
		{"words": ctxs["words"]},
	}

	// Apply default middleware
	r.Use(chiMw.RequestID)
	r.Use(chiMw.RealIP)
	r.Use(middleware.LoggerMiddleware)
	r.Use(middleware.StartTimeMiddleware)
	r.Use(chiMw.Recoverer)
	r.Use(chiMw.Timeout(60 * time.Second))

	cfg := loadAppConfig(".")

	// Create shared storage backend
	storage := db.NewStorage(cfg.Storage)

	res := &Router{
		Router:    r,
		config:    cfg,
		contexts:  defaultContexts,
		storage:   storage,
		services:  make(map[string]*ServiceItem),
		databases: make(map[string]db.DB),
	}

	for _, opt := range options {
		opt(res)
	}

	if err := createUIFileStructure(res.config); err != nil {
		slog.Error("Failed to create ui file structure", "error", err)
	}

	return res
}

// RegisterService registers a service with the router.
// The service config must have a Name field set.
// The service will be registered at the route "/{cfg.Name}".
// If a service with the same Name is already registered, the call is
// ignored with a warning - without this guard, chi.Mount would panic
// and crash the process.
func (r *Router) RegisterService(
	cfg *config.ServiceConfig,
	handler Handler,
	opts ...HandlerOption,
) {
	if r.isServiceRegistered(cfg.Name) {
		slog.Warn("Service already registered, skipping", "name", cfg.Name)
		return
	}

	options := &handlerOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Get service-scoped DB from shared storage
	serviceDB := r.storage.NewDB(cfg.Name, r.config.History.Duration)
	mwParams := middleware.NewParams(cfg, serviceDB)

	// Use cfg.Name as the route prefix (ensure it starts with /)
	prefix := "/" + cfg.Name
	r.Route(prefix, func(subRouter chi.Router) {
		// Resource resolver (must be before other middleware that read the resource path)
		subRouter.Use(middleware.CreateResourceResolverMiddleware(mwParams))

		// Config override middleware (must be before other middlewares to override config)
		subRouter.Use(middleware.CreateConfigOverrideMiddleware(mwParams))

		// Custom middleware
		for _, createMw := range options.middleware {
			subRouter.Use(createMw(mwParams))
		}

		// Standard middleware (always applied)
		subRouter.Use(middleware.CreateLatencyAndErrorMiddleware(mwParams))
		subRouter.Use(middleware.CreateReplayReadMiddleware(mwParams))
		subRouter.Use(middleware.CreateReplayWriteMiddleware(mwParams))
		subRouter.Use(middleware.CreateCacheReadMiddleware(mwParams))
		subRouter.Use(middleware.CreateUpstreamRequestMiddleware(mwParams))
		subRouter.Use(middleware.CreateCacheWriteMiddleware(mwParams))

		handler.RegisterRoutes(subRouter)
		mwParams.SetRouter(subRouter)
	})

	// Skip logging for services with history disabled
	if cfg.Name != "" && !cfg.HistoryEnabled() {
		middleware.AddSkipPrefix("/" + cfg.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.services[cfg.Name] = &ServiceItem{
		Name:    cfg.Name,
		Handler: handler,
		Config:  cfg,
	}
	r.databases[cfg.Name] = serviceDB
}

// HandlerOption configures service registration behavior.
type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	middleware []func(*middleware.Params) func(http.Handler) http.Handler
}

// WithMiddleware prepends middleware before the built-in middleware chain.
func WithMiddleware(mw []func(*middleware.Params) func(http.Handler) http.Handler) HandlerOption {
	return func(o *handlerOptions) {
		o.middleware = mw
	}
}

// RegisterHTTPHandler registers a Handler as a service.
// The handlerFactory receives the service DB and returns the handler.
// The service will be registered at the route "/{cfg.Name}".
// If a service with the same Name is already registered, the call is
// ignored with a warning - without this guard, chi.Mount would panic
// and crash the process.
func (r *Router) RegisterHTTPHandler(
	cfg *config.ServiceConfig,
	handlerFactory func(db.DB) Handler,
	opts ...HandlerOption,
) {
	if r.isServiceRegistered(cfg.Name) {
		slog.Warn("Service already registered, skipping", "name", cfg.Name)
		return
	}

	options := &handlerOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// Get service-scoped DB from shared storage
	serviceDB := r.storage.NewDB(cfg.Name, r.config.History.Duration)

	// Create the handler with access to the DB
	handler := handlerFactory(serviceDB)

	mwParams := middleware.NewParams(cfg, serviceDB)

	// Use cfg.Name as the route prefix (ensure it starts with /)
	prefix := "/" + cfg.Name
	r.Route(prefix, func(subRouter chi.Router) {
		// Resource resolver (must be before other middleware that read the resource path)
		subRouter.Use(middleware.CreateResourceResolverMiddleware(mwParams))

		// Config override middleware (must be before other middlewares to override config)
		subRouter.Use(middleware.CreateConfigOverrideMiddleware(mwParams))

		// Custom middleware
		for _, createMw := range options.middleware {
			subRouter.Use(createMw(mwParams))
		}

		// Standard middleware (always applied)
		subRouter.Use(middleware.CreateLatencyAndErrorMiddleware(mwParams))
		subRouter.Use(middleware.CreateReplayReadMiddleware(mwParams))
		subRouter.Use(middleware.CreateReplayWriteMiddleware(mwParams))
		subRouter.Use(middleware.CreateCacheReadMiddleware(mwParams))
		subRouter.Use(middleware.CreateUpstreamRequestMiddleware(mwParams))
		subRouter.Use(middleware.CreateCacheWriteMiddleware(mwParams))

		handler.RegisterRoutes(subRouter)
		mwParams.SetRouter(subRouter)
	})

	// Skip logging for services with history disabled
	if cfg.Name != "" && !cfg.HistoryEnabled() {
		middleware.AddSkipPrefix("/" + cfg.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.services[cfg.Name] = &ServiceItem{
		Name:    cfg.Name,
		Handler: handler,
		Config:  cfg,
	}
	r.databases[cfg.Name] = serviceDB
}

// Config returns the app configuration
func (r *Router) Config() *config.AppConfig {
	return r.config
}

func (r *Router) GetServices() map[string]*ServiceItem {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make(map[string]*ServiceItem, len(r.services))
	for k, v := range r.services {
		res[k] = v
	}

	return res
}

func (r *Router) isServiceRegistered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.services[name]
	return ok
}

// GetDB returns the database for a specific service.
// Returns nil if the service is not registered.
func (r *Router) GetDB(serviceName string) db.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.databases[serviceName]
}

// GetContexts returns the list of contexts
func (r *Router) GetContexts() []map[string]map[string]any {
	return r.contexts
}

type RouterOption func(*Router)

func WithConfigOption(cfg *config.AppConfig) RouterOption {
	return func(r *Router) {
		r.config = cfg
	}
}

// createUIFileStructure creates the necessary directories and files
func createUIFileStructure(config *config.AppConfig) error {
	if config.DisableUI {
		return nil
	}

	paths := config.Paths
	dirs := []string{paths.Resources, paths.UI}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.Mkdir(dir, os.ModePerm); err != nil {
				return err
			}
		}
	}

	return nil
}

// loadAppConfig loads app config from resources/data/app.yml if it exists,
// falling back to defaults. Environment variables override file values.
func loadAppConfig(baseDir string) *config.AppConfig {
	paths := config.NewPaths(baseDir)
	appConfigPath := paths.Data + "/app.yml"

	data, err := os.ReadFile(appConfigPath)
	if err != nil {
		// File doesn't exist or can't be read - use defaults
		cfg := config.NewDefaultAppConfig(baseDir)
		if err := env.Parse(cfg); err != nil {
			slog.Error("Failed to parse env", "error", err)
		}
		return cfg
	}

	cfg, err := config.NewAppConfigFromBytes(data, baseDir)
	if err != nil {
		slog.Error("Failed to parse app config, using defaults", "error", err, "path", appConfigPath)
		cfg = config.NewDefaultAppConfig(baseDir)
	}

	// Environment variables override file values
	if err := env.Parse(cfg); err != nil {
		slog.Error("Failed to parse env", "error", err)
	}

	return cfg
}
