package portable

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/lmittmann/tint"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/factory"
)

const (
	exitCodeShutdown = 0
	exitCodeError    = 1
)

// Run starts the server in portable mode - serving mock responses directly from OpenAPI specs.
func Run(args []string) int {
	// LOG_LEVEL can be: debug, info, warn, error, none (default: info)
	logLevel := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	case "none":
		logLevel = slog.Level(99)
	}

	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.Kitchen,
	}))
	slog.SetDefault(logger)

	fl, positional := parseFlags(args)
	specs := resolveSpecs(positional)
	if len(specs) == 0 {
		slog.Error("No OpenAPI spec files found")
		return exitCodeError
	}

	baseDir := filepath.Join(os.TempDir(), "mockzilla-portable")
	_ = os.MkdirAll(baseDir, 0o755)

	// Load unified config (app + per-service)
	cfg, err := loadPortableConfig(fl.config, baseDir)
	if err != nil {
		slog.Error("Failed to load config, using defaults", "error", err)
		cfg = &portableConfig{}
	}

	// Load per-service contexts
	contexts, err := loadContexts(fl.context)
	if err != nil {
		slog.Error("Failed to load contexts, continuing without contexts", "error", err)
		contexts = nil
	}

	// Resolve app config: use from file or defaults
	appCfg := cfg.App
	if appCfg == nil {
		appCfg = config.NewDefaultAppConfig(baseDir)
	}

	// Environment variables override file/default values.
	if err := env.Parse(appCfg); err != nil {
		slog.Error("Failed to apply env overrides", "error", err)
	}

	// --port flag wins over everything
	if fl.port > 0 {
		appCfg.Port = fl.port
	}
	if appCfg.Port == 0 {
		appCfg.Port = 2200
	}

	// Create router
	router := api.NewRouter(api.WithConfigOption(appCfg))
	_ = api.CreateHealthRoutes(router)
	_ = api.CreateHomeRoutes(router)
	_ = api.CreateServiceRoutes(router)
	_ = api.CreateHistoryRoutes(router)
	_ = api.CreateServiceConfigRoutes(router)

	// Track swappable handlers for hot reload
	handlers := make(map[string]*swappableHandler)

	// Register each spec as a service
	for _, specPath := range specs {
		name := api.NormalizeServiceName(specPath)
		svcCfg := cfg.Services[name]
		ctxBytes := contexts[name]

		if err := registerService(router, specPath, svcCfg, ctxBytes, handlers); err != nil {
			slog.Error("Failed to register service", "spec", specPath, "error", err)
			continue
		}
	}

	if len(handlers) == 0 {
		slog.Error("No services registered")
		return exitCodeError
	}

	// Log registered services
	for name := range handlers {
		slog.Info("Registered service", "path", "/"+name)
	}

	// Start server
	addr := fmt.Sprintf(":%d", appCfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info(fmt.Sprintf("Connexions portable mode on http://localhost:%d%s", appCfg.Port, appCfg.HomeURL))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Start file watcher
	go watchSpecs(specs, router, cfg, contexts, handlers)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		return exitCodeError
	}

	slog.Info("Server exited")
	return exitCodeShutdown
}

// RunFS extracts an fs.FS to a temp directory and runs portable mode.
// The FS root should contain OpenAPI spec files (*.yml, *.yaml, *.json),
// and optionally: static/, app.yml, context.yml.
func RunFS(fsys fs.FS, args []string) int {
	dir, err := os.MkdirTemp("", "connexions-portable-fs-*")
	if err != nil {
		slog.Error("Failed to create temp dir", "error", err)
		return exitCodeError
	}

	if err := extractFS(fsys, dir); err != nil {
		slog.Error("Failed to extract FS", "error", err)
		return exitCodeError
	}

	// Capture config/context paths before moving them out of the way.
	configPath := filepath.Join(dir, "app.yml")
	contextPath := filepath.Join(dir, "context.yml")

	// Move config files so resolveSpecs doesn't treat them as OpenAPI specs.
	for _, p := range []string{configPath, contextPath} {
		if fileExists(p) {
			_ = os.Rename(p, p+".cfg")
		}
	}

	var runArgs []string
	if openapiDir := filepath.Join(dir, "openapi"); fileExists(openapiDir) {
		runArgs = append(runArgs, openapiDir)
	}
	runArgs = append(runArgs, dir)
	if fileExists(configPath + ".cfg") {
		runArgs = append(runArgs, "--config", configPath+".cfg")
	}
	if fileExists(contextPath + ".cfg") {
		runArgs = append(runArgs, "--context", contextPath+".cfg")
	}
	runArgs = append(runArgs, args...)

	return Run(runArgs)
}

func extractFS(fsys fs.FS, dest string) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// registerService creates and registers a handler for a single spec file.
func registerService(
	router *api.Router,
	specPath string,
	svcCfg *config.ServiceConfig,
	contextBytes []byte,
	handlers map[string]*swappableHandler,
) error {
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	name := api.NormalizeServiceName(specPath)

	// Build factory options
	var opts []factory.FactoryOption
	if contextBytes != nil {
		opts = append(opts, factory.WithServiceContext(contextBytes))
	}
	// Enable lazy loading for large specs
	opts = append(opts, factory.WithSpecOptions(&config.SpecOptions{LazyLoad: true}))

	h, err := newHandler(specBytes, opts...)
	if err != nil {
		return fmt.Errorf("creating handler: %w", err)
	}

	// Build service config: start with defaults, overlay per-service if provided
	serviceCfg := config.NewServiceConfig()
	serviceCfg.Name = name
	if svcCfg != nil {
		serviceCfg.OverwriteWith(svcCfg)
		serviceCfg.Name = name // Ensure name is always the spec-derived name
	}

	// Wrap in swappable handler
	sw := &swappableHandler{handler: h}
	handlers[name] = sw

	router.RegisterService(serviceCfg, sw)
	return nil
}
