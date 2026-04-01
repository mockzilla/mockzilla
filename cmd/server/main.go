package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/lmittmann/tint"
	"github.com/mockzilla/connexions/v2/internal/portable"
	"github.com/mockzilla/connexions/v2/pkg/api"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/loader"

	// Imports to ensure it's vendored for generated code
	_ "github.com/go-playground/validator/v10"
	_ "github.com/google/uuid"
)

var version = "dev"

const (
	exitCodeShutdown = 0
	exitCodeRestart  = 100
	exitCodeError    = 1
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println(version)
		return
	}

	for {
		exitCode := runServer()
		if exitCode == exitCodeRestart {
			log.Println("Restarting server with new binary...")

			// Brief pause before restart
			time.Sleep(100 * time.Millisecond)

			// Get the path to the newly built server binary
			// The watcher builds to ".build/server/server" in the working directory
			appDir := os.Getenv("APP_DIR")
			if appDir == "" {
				_, b, _, _ := runtime.Caller(0)
				appDir = filepath.Dir(filepath.Dir(filepath.Dir(b)))
			}
			newBinary := filepath.Join(appDir, ".build", "server", "server")

			// Make sure the new binary exists
			if _, err := os.Stat(newBinary); err != nil {
				log.Printf("New binary not found at %s: %v", newBinary, err)
				os.Exit(exitCodeError)
			}

			// Get absolute path for exec
			absPath, err := filepath.Abs(newBinary)
			if err != nil {
				log.Printf("Failed to get absolute path: %v", err)
				os.Exit(exitCodeError)
			}

			log.Printf("Exec into new binary: %s", absPath)

			// Exec into the new binary, replacing the current process
			if err := syscall.Exec(absPath, os.Args, os.Environ()); err != nil {
				log.Printf("Failed to exec new binary: %v", err)
				os.Exit(exitCodeError)
			}
			// If exec succeeds, this code never runs
		}
		os.Exit(exitCode)
	}
}

func runServer() int {
	// Check for portable mode: if args contain spec files or directories with spec files
	if len(os.Args) > 1 && portable.IsPortableMode(os.Args[1:]) {
		return portable.Run(os.Args[1:])
	}

	appDir := "."
	if v := os.Getenv("APP_DIR"); v != "" {
		appDir = v
	}
	if len(os.Args) > 1 {
		if info, err := os.Stat(os.Args[1]); err == nil && info.IsDir() {
			appDir = os.Args[1]
		}
	}
	_ = godotenv.Load(fmt.Sprintf("%s/.env", appDir), fmt.Sprintf("%s/.env.dist", appDir))

	// Determine log level from environment variable
	// LOG_LEVEL can be: debug, info, warn, error (default: info)
	logLevel := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	// Use JSON handler by default.
	// Set LOG_FORMAT=text for development colored logs.
	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "text" {
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      logLevel,
			TimeFormat: time.Kitchen,
		})
	} else {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Create the central router with middleware
	router := api.NewRouter()

	_ = api.CreateHealthRoutes(router)
	_ = api.CreateHomeRoutes(router)
	_ = api.CreateServiceRoutes(router)
	_ = api.CreateHistoryRoutes(router)
	_ = api.CreateServiceConfigRoutes(router)

	// Auto-discover and register all services
	// Services are automatically registered via their init() functions
	// Load concurrently for faster startup with large specs
	loader.LoadAll(router)

	// Log discovered services
	services := loader.DefaultRegistry.List()
	if len(services) == 0 {
		log.Println("WARNING: No services discovered!")
	} else {
		log.Printf("Discovered %d service(s): %v", len(services), services)
	}

	// Configure server
	port := getEnv("PORT", "2200")
	addr := fmt.Sprintf(":%s", port)

	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting Connexions Server on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Set up signal handling for graceful shutdown and restart
	quit := make(chan os.Signal, 1)
	restart := make(chan struct{}, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start file watcher for hot reload
	paths := config.NewPaths(appDir)
	watcher, err := newDataWatcher(paths)
	if err != nil {
		log.Printf("WARNING: Failed to create service watcher: %v", err)
		return exitCodeError
	}

	// Set up reload callback to trigger in-process restart
	// Must be set BEFORE processExistingFiles() so restart can be triggered
	watcher.setReloadCallback(func() error {
		slog.Info("File change detected, triggering restart...")
		restart <- struct{}{}
		return nil
	})

	// Process existing OpenAPI specs and static files on startup
	// This enables the "mount and serve" workflow for Docker
	if err := watcher.processExistingFiles(); err != nil {
		log.Printf("WARNING: Failed to process existing files: %v", err)
	}

	log.Printf("File watcher started with auto-restart, monitoring: %s", paths.Data)

	watcher.start()
	defer watcher.stop()

	// Wait for either shutdown signal or restart signal
	var exitCode int
	select {
	case sig := <-quit:
		log.Printf("Received signal %v, shutting down...", sig)
		exitCode = exitCodeShutdown
	case <-restart:
		log.Println("Reloading services...")
		exitCode = exitCodeRestart
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
		return exitCodeError
	}

	if exitCode == exitCodeShutdown {
		log.Println("Server exited")
	}

	return exitCode
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
