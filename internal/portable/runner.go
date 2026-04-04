package portable

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/lmittmann/tint"
	"github.com/mockzilla/connexions/v2/pkg/api"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/factory"
)

const (
	exitCodeShutdown = 0
	exitCodeError    = 1
)

// Run starts the server in portable mode - serving mock responses directly from OpenAPI specs.
func Run(args []string) int {
	// Set up colored text logger for portable mode (user-facing tool)
	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.Kitchen,
	}))
	slog.SetDefault(logger)

	fl, positional := parseFlags(args)
	specs := resolveSpecs(positional)
	if len(specs) == 0 {
		log.Println("No OpenAPI spec files found")
		return exitCodeError
	}

	baseDir := filepath.Join(os.TempDir(), "connexions-portable")
	_ = os.MkdirAll(baseDir, 0o755)

	// Load unified config (app + per-service)
	cfg, err := loadPortableConfig(fl.config, baseDir)
	if err != nil {
		log.Printf("Failed to load config: %v", err)
		return exitCodeError
	}

	// Load per-service contexts
	contexts, err := loadContexts(fl.context)
	if err != nil {
		log.Printf("Failed to load contexts: %v", err)
		return exitCodeError
	}

	// Resolve app config: use from file or defaults
	appCfg := cfg.App
	if appCfg == nil {
		appCfg = config.NewDefaultAppConfig(baseDir)
	}

	// Environment variables override file/default values.
	if err := env.Parse(appCfg); err != nil {
		log.Printf("Failed to apply env overrides: %v", err)
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
			log.Printf("Failed to register %s: %v", specPath, err)
			continue
		}
	}

	if len(handlers) == 0 {
		log.Println("No services registered")
		return exitCodeError
	}

	// Log registered services
	for name := range handlers {
		log.Printf("  /%s", name)
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
		log.Printf("Connexions portable mode on http://localhost:%d%s", appCfg.Port, appCfg.HomeURL)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Start file watcher
	go watchSpecs(specs, router, cfg, contexts, handlers)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
		return exitCodeError
	}

	log.Println("Server exited")
	return exitCodeShutdown
}

// RunFS extracts an fs.FS to a temp directory and runs portable mode.
// The FS root should contain OpenAPI spec files (*.yml, *.yaml, *.json),
// and optionally: static/, app.yml, context.yml.
func RunFS(fsys fs.FS, args []string) int {
	dir, err := os.MkdirTemp("", "connexions-portable-fs-*")
	if err != nil {
		log.Printf("Failed to create temp dir: %v", err)
		return exitCodeError
	}

	if err := extractFS(fsys, dir); err != nil {
		log.Printf("Failed to extract FS: %v", err)
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
