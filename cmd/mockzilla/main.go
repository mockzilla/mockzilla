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
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/lmittmann/tint"
	"github.com/mockzilla/mockzilla/v2/internal/inspect"
	"github.com/mockzilla/mockzilla/v2/internal/portable"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/loader"
	"github.com/spf13/cobra"

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

	if len(os.Args) >= 2 && (os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help") {
		printUsage()
		return
	}

	for {
		exitCode := run()
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

// run is the top-level dispatcher: routes the invocation to a
// subcommand (info / simplify / pack), portable mode (file/URL/dir/
// package arg), or the codegen-mode app server. Returns the process
// exit code so main can decide whether to re-exec on a hot reload.
func run() int {
	args := os.Args[1:]

	if exit, ok := dispatchSubcommand(args); ok {
		return exit
	}

	if portable.IsPortableMode(args) {
		return portable.Run(args)
	}

	// At this point args[0] (if any) is neither a known subcommand
	// nor a portable input (spec / URL / package / portable dir).
	// App mode only accepts no positional arg (serve CWD) or a
	// directory. A bare unrecognised word — typically a subcommand
	// from a newer CLI that this binary doesn't know — must error
	// rather than silently boot the HTTP server.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if info, err := os.Stat(args[0]); err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: unknown command or invalid path: %q\n\n", args[0])
			printUsage()
			return exitCodeError
		}
	}

	return runAppMode(args)
}

// dispatchSubcommand checks args[0] against the known subcommands and
// runs the matching one if any. Returns (exitCode, true) when a
// subcommand handled the call, otherwise (0, false) so the caller can
// fall through to portable / app mode dispatch.
func dispatchSubcommand(args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "info":
		// `mockzilla info <url-or-file>` prints a JSON summary of a
		// spec or a .mockz manifest and exits. Used by the MCP
		// bridge's peek_openapi tool.
		return inspect.Run(args[1:]), true
	case "simplify":
		return runCobraSubcommand(simplifyCommand(), args), true
	case "pack":
		return runCobraSubcommand(packCommand(), args), true
	}
	return 0, false
}

// runAppMode is the legacy codegen-server entrypoint: watches
// resources/data/, registers services discovered there, and serves
// the HTTP API with hot-reload on disk changes.
func runAppMode(args []string) int {
	appDir := "."
	if v := os.Getenv("APP_DIR"); v != "" {
		appDir = v
	}
	if len(args) > 0 {
		if info, err := os.Stat(args[0]); err == nil && info.IsDir() {
			appDir = args[0]
		}
	}
	_ = godotenv.Load(fmt.Sprintf("%s/.env", appDir), fmt.Sprintf("%s/.env.dist", appDir))

	// Determine log level from environment variable
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
		log.Printf("Starting mockzilla Server on %s", addr)
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

// runCobraSubcommand attaches a cobra subcommand to a stub root and executes
// it with the given args (which should start with the subcommand name).
// Used by dispatchSubcommand to plug cobra-based subcommands into the
// top-level entry point.
func runCobraSubcommand(sub *cobra.Command, args []string) int {
	root := &cobra.Command{Use: "mockzilla", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(sub)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return exitCodeError
	}
	return exitCodeShutdown
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func printUsage() {
	fmt.Print(`mockzilla: generate mock servers from OpenAPI specs

Usage:
  mockzilla <spec.yaml | dir | package.mockz | https://...>  [flags]
                                               Portable mode: serve a spec, directory, or package
  mockzilla info <spec.yaml | https://...>     Print a JSON summary of a spec and exit
  mockzilla simplify <spec.yaml | https://...> Simplify a spec (drop unions, limit optional props)
  mockzilla pack <dir>                         Pack a service directory into a .mockz archive
  mockzilla [<app-dir>]                        App mode: serve a configured mockzilla project
  mockzilla --version                          Print version and exit
  mockzilla --help                             Print this message

Portable-mode flags:
  --port N            Server port (0 = let the OS pick a free port; default 2200)
  --ready-stamp       Emit a single JSON line on stdout once the listener is bound
                      (for programmatic supervisors like the mockzilla MCP bridge)

Single-service convenience flags (only valid when exactly one service is
registered, typically with a single spec file or single folder arg):
  --latency D         Latency for the service (e.g. 100ms, 1s)
  --mount PATH        URL mount path for the service (e.g. pets/v2)
  --errors RULES      Error injection rules: p5=500,p10=503
  --context FILE      Path to a flat context YAML (replacement values)

Layout:
  Point the CLI at one of:
    - A single OpenAPI spec file:    mockzilla petstore.yml
    - A remote spec URL:             mockzilla https://api.example.com/openapi.json
    - A single-service folder:       mockzilla ./pets/
        (folder contains a *.{yml,yaml,json} spec and/or index.<ext>
         response files at <path>/<method>/ or <path>/)
    - A flat root of specs:          mockzilla ./
        (root contains multiple top-level *.{yml,yaml,json} files;
         each spec becomes its own service)
    - A multi-service root:          mockzilla ./
        (root contains a services/<name>/ tree)
    - A .mockz package (file or URL): mockzilla petstore.mockz

  Service folder layout (used both at the CLI and inside .mockz packages):
    services/petstore/
      openapi.yml              # the OpenAPI spec (any *.{yml,yaml,json} name works)
      config.yml               # optional: latency, errors, mount, upstream, cache
      context.yml              # optional: flat replacement values (no service-name wrapper)
      users/index.json         # optional static endpoint -> GET /users
      users/post/index.json    # optional static endpoint -> POST /users
      users/{id}/index.json    # optional static endpoint -> GET /users/{id}
    app.yml                    # optional global: port, history, storage, etc.

Examples:
  mockzilla https://petstore3.swagger.io/api/v3/openapi.json
  mockzilla --port 3000 ./services/
  mockzilla --latency 50ms --mount pets/v2 petstore.yml
  mockzilla petstore.mockz
  mockzilla https://example.com/my-api.mockz
  mockzilla info https://petstore3.swagger.io/api/v3/openapi.json
  mockzilla simplify --output simplified.yml --optional 5 ./openapi.yml
  mockzilla --ready-stamp --port 0 ./openapi.yml

Run 'mockzilla <subcommand> --help' for subcommand-specific flags (e.g. 'mockzilla simplify --help').

Docs:  https://github.com/mockzilla/mockzilla
`)
}
