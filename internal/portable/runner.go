package portable

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
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

// Setup is a fully-configured portable mode ready to be served:
// router with all routes registered (including resolved services),
// the resolved AppConfig, and the underlying service list. Run uses
// this for the normal CLI flow (then binds a listener and serves);
// in-process tests use the same function and wrap Router with
// httptest.NewServer so the test exercises the real production path.
type Setup struct {
	Router   *api.Router
	AppCfg   *config.AppConfig
	Services []Service

	// Internals needed by Run for serving + hot-reload. Tests don't
	// need to touch these.
	handlers map[string]*swappableHandler
	flags    flags
}

// BuildSetup runs the portable startup pipeline up to (but not
// including) port binding. Same code path Run uses.
//
// Does NOT call configureLogger - that's a CLI-process concern (takes
// over slog's default). Run sets it; in-process callers (tests) keep
// their own slog config.
func BuildSetup(args []string) (*Setup, error) {
	fl, positional := parseFlags(args)

	services, err := resolveServices(positional)
	if err != nil {
		return nil, fmt.Errorf("resolving services: %w", err)
	}

	baseDir := filepath.Join(os.TempDir(), "mockzilla-portable")
	_ = os.MkdirAll(baseDir, 0o755)

	appCfg, err := loadAppConfig(rootFromArgs(positional), baseDir)
	if err != nil {
		slog.Warn("Failed to load app.yml, using defaults", "error", err)
		appCfg = config.NewDefaultAppConfig(baseDir)
	}

	// Env vars override the file/default values via struct tags.
	if err := env.Parse(appCfg); err != nil {
		slog.Error("Failed to apply env overrides", "error", err)
	}
	if appCfg.History != nil && appCfg.History.Enabled != nil && !*appCfg.History.Enabled {
		appCfg.History.URL = ""
	}

	// --port flag wins. -1 = unset; 0 = let the kernel pick.
	if fl.port >= 0 {
		appCfg.Port = fl.port
	}
	if appCfg.Port == 0 && fl.port < 0 {
		appCfg.Port = 2200
	}

	// If any service wants the root mount (empty Name, no explicit
	// Mount, or `mount: /`), move the UI off `/` so chi can host the
	// service there. The dotted `/.ui` aligns with the other internal
	// routes (`/.services`, `/.history`). Skip the relocation when
	// the UI is disabled — nothing is mounted at `/` to conflict with.
	if !appCfg.DisableUI && (appCfg.HomeURL == "" || appCfg.HomeURL == "/") {
		if anyServiceClaimsRoot(services, fl) {
			slog.Info("Service requested root mount; relocating UI from / to /.ui")
			appCfg.HomeURL = "/.ui"
		}
	}

	router := api.NewRouter(api.WithConfigOption(appCfg))
	_ = api.CreateHealthRoutes(router)
	_ = api.CreateHomeRoutes(router)
	_ = api.CreateServiceRoutes(router)
	_ = api.CreateHistoryRoutes(router)
	_ = api.CreateServiceConfigRoutes(router)

	overrides, err := buildOverrides(fl)
	if err != nil {
		return nil, fmt.Errorf("invalid convenience flag: %w", err)
	}
	if overrides != nil && len(services) != 1 {
		return nil, fmt.Errorf("convenience flags (--latency/--mount/--errors/--context) require exactly one service, got %d", len(services))
	}

	handlers := make(map[string]*swappableHandler)
	for _, svc := range services {
		if err := registerService(router, svc, overrides, handlers); err != nil {
			slog.Error("Failed to register service", "name", svc.Name, "error", err)
			continue
		}
	}

	if len(handlers) == 0 {
		return nil, fmt.Errorf("no services registered")
	}

	return &Setup{
		Router:   router,
		AppCfg:   appCfg,
		Services: services,
		handlers: handlers,
		flags:    fl,
	}, nil
}

// Run starts the server in portable mode. The args are positional
// inputs (files, URLs, dirs, packages); per-service configuration is
// discovered alongside each service.
func Run(args []string) int {
	configureLogger()

	setup, err := BuildSetup(args)
	if err != nil {
		slog.Error("Portable setup failed", "error", err)
		return exitCodeError
	}

	listener, err := bindListener(setup.AppCfg)
	if err != nil {
		return exitCodeError
	}

	if setup.flags.readyStamp {
		emitReadyStamp(setup.AppCfg, setup.Router)
	}

	server := &http.Server{
		Handler:      setup.Router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info(fmt.Sprintf("Mockzilla portable mode on http://localhost:%d%s", setup.AppCfg.Port, setup.AppCfg.HomeURL))
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	go watchServices(setup.Services, setup.Router, setup.handlers)

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
// The FS root must follow the new shape: services/<name>/... or a flat
// single-service layout with openapi.{yml,yaml,json} at the root.
func RunFS(fsys fs.FS, args []string) int {
	dir, err := os.MkdirTemp("", "mockzilla-portable-fs-*")
	if err != nil {
		slog.Error("Failed to create temp dir", "error", err)
		return exitCodeError
	}
	if err := extractFS(fsys, dir); err != nil {
		slog.Error("Failed to extract FS", "error", err)
		return exitCodeError
	}
	return Run(append([]string{dir}, args...))
}

// flags holds the parsed CLI flags for portable mode. Most per-service
// settings live in each service's config.yml/context.yml; the ad-hoc
// flags below are convenience knobs that apply only when a single
// service is registered.
type flags struct {
	port       int
	readyStamp bool
	// Single-service convenience knobs. Each applies only when exactly
	// one service is registered; with multiple services the runner
	// errors out so the user has to express the intent per-service.
	latency string
	mount   string
	errors  string // "p5=500,p10=503"
	context string // path to a flat context YAML
}

// cliOverrides captures the parsed convenience flags. Only meaningful
// when a single service is registered; the runner enforces that
// invariant before applying.
type cliOverrides struct {
	latency      time.Duration
	mount        string
	errors       map[string]int
	contextBytes []byte
}

func (o *cliOverrides) applyTo(cfg *config.ServiceConfig) {
	if o == nil {
		return
	}
	if o.latency > 0 {
		cfg.Latency = o.latency
	}
	if o.mount != "" {
		cfg.Mount = o.mount
	}
	if len(o.errors) > 0 {
		if cfg.Errors == nil {
			cfg.Errors = make(map[string]int)
		}
		for k, v := range o.errors {
			cfg.Errors[k] = v
		}
	}
}

// boolFlags lists flag names that don't take a value. The hand-rolled
// splitter below needs to know these so it doesn't greedily eat the
// next positional arg as a value (e.g. `mockzilla ./dir --ready-stamp`
// would otherwise lose `./dir`).
var boolFlags = map[string]bool{
	"ready-stamp": true,
}

func configureLogger() {
	level := slog.LevelInfo
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "none":
		level = slog.Level(99)
	}
	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      level,
		TimeFormat: time.Kitchen,
	}))
	slog.SetDefault(logger)
}

func bindListener(appCfg *config.AppConfig) (net.Listener, error) {
	addr := fmt.Sprintf(":%d", appCfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("Failed to bind listener", "addr", addr, "error", err)
		return nil, err
	}
	if tcp, ok := listener.Addr().(*net.TCPAddr); ok {
		appCfg.Port = tcp.Port
	}
	return listener, nil
}

// flagName strips the leading dashes from a flag arg ("--port" → "port").
func flagName(arg string) string {
	return strings.TrimLeft(arg, "-")
}

// parseFlags splits the CLI args into typed flags and positional args.
// Per-service settings normally come from per-service files; the
// single-service convenience flags allow quick ad-hoc tweaks for the
// common "I just want to serve one spec with a bit of customisation"
// case without forcing the user to create a folder layout.
func parseFlags(args []string) (flags, []string) {
	fset := flag.NewFlagSet("portable", flag.ContinueOnError)
	fl := flags{}
	fset.IntVar(&fl.port, "port", -1, "Server port (0 = kernel picks; default: from app.yml or 2200)")
	fset.BoolVar(&fl.readyStamp, "ready-stamp", false, "Emit a single JSON line on stdout once the listener is bound")
	fset.StringVar(&fl.latency, "latency", "", "Latency for the single registered service (e.g. 100ms, 1s)")
	fset.StringVar(&fl.mount, "mount", "", "Mount path for the single registered service (e.g. pets/v2)")
	fset.StringVar(&fl.errors, "errors", "", "Error injection for the single service: p5=500,p10=503")
	fset.StringVar(&fl.context, "context", "", "Path to a flat context.yml applied to the single service")

	var positional, flagArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		// Skip the value-consumption step for bool flags and for
		// `--key=value` forms (already self-contained).
		if strings.Contains(a, "=") || boolFlags[flagName(a)] {
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flagArgs = append(flagArgs, args[i+1])
			i++
		}
	}

	if err := fset.Parse(flagArgs); err != nil {
		slog.Warn("Failed to parse flags", "error", err)
	}
	return fl, positional
}

// parseErrorsFlag parses "p5=500,p10=503" into {"p5":500,"p10":503}.
// Each rule's key follows the existing percentile convention (px) and
// the value is the HTTP status.
func parseErrorsFlag(raw string) (map[string]int, error) {
	out := make(map[string]int)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("expected key=value, got %q", pair)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("status for %q: %w", k, err)
		}
		out[k] = n
	}
	return out, nil
}

// buildOverrides parses the raw CLI flag strings into a cliOverrides.
// Returns (nil, nil) when none of the flags were supplied.
func buildOverrides(fl flags) (*cliOverrides, error) {
	if fl.latency == "" && fl.mount == "" && fl.errors == "" && fl.context == "" {
		return nil, nil
	}
	o := &cliOverrides{}
	if fl.latency != "" {
		d, err := time.ParseDuration(fl.latency)
		if err != nil {
			return nil, fmt.Errorf("--latency: %w", err)
		}
		o.latency = d
	}
	if fl.mount != "" {
		o.mount = fl.mount
	}
	if fl.errors != "" {
		parsed, err := parseErrorsFlag(fl.errors)
		if err != nil {
			return nil, fmt.Errorf("--errors: %w", err)
		}
		o.errors = parsed
	}
	if fl.context != "" {
		bts, err := os.ReadFile(fl.context)
		if err != nil {
			return nil, fmt.Errorf("--context: %w", err)
		}
		o.contextBytes = bts
	}
	return o, nil
}

// anyServiceClaimsRoot reports whether any discovered service will end
// up mounted at "/" once flag overrides and per-folder configs are
// applied. We can't fully resolve mounts here without re-reading every
// config.yml, so we approximate with the signals available at this
// point: an empty Name (no inside-the-folder identity signal) means
// the service will mount at "/" by default, and a CLI --mount="/"
// override forces that mount regardless of folder identity.
func anyServiceClaimsRoot(services []Service, fl flags) bool {
	if fl.mount == "/" {
		return true
	}
	for _, s := range services {
		if s.Name == "" {
			return true
		}
	}
	return false
}

// registerService wires one discovered Service into the router. The
// service's per-folder config.yml drives latency/errors/mount/etc; its
// context.yml drives replacement values inside the factory. CLI
// overrides (when present) win over per-folder files.
func registerService(
	router *api.Router,
	svc Service,
	overrides *cliOverrides,
	handlers map[string]*swappableHandler,
) error {
	specBytes, err := os.ReadFile(svc.SpecPath)
	if err != nil {
		return fmt.Errorf("reading spec: %w", err)
	}

	svcCfg, err := loadServiceConfig(svc)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctxBytes, err := loadServiceContext(svc)
	if err != nil {
		return fmt.Errorf("loading context: %w", err)
	}

	if overrides != nil {
		overrides.applyTo(svcCfg)
		if overrides.contextBytes != nil {
			ctxBytes = overrides.contextBytes
		}
	}

	var opts []factory.FactoryOption
	if ctxBytes != nil {
		opts = append(opts, factory.WithServiceContext(ctxBytes))
	}
	opts = append(opts, factory.WithSpecOptions(&config.SpecOptions{LazyLoad: true}))

	h, err := newHandler(specBytes, opts...)
	if err != nil {
		return fmt.Errorf("creating handler: %w", err)
	}

	sw := &swappableHandler{handler: h}
	handlers[svc.Name] = sw
	router.RegisterService(svcCfg, sw)
	slog.Info("Registered service", "name", svc.Name, "mount", api.ServicePrefix(svcCfg))
	return nil
}

func emitReadyStamp(appCfg *config.AppConfig, router *api.Router) {
	registered := router.GetServices()
	rs := make([]readyService, 0, len(registered))
	for name, item := range registered {
		rs = append(rs, readyService{Name: name, Path: api.ServicePrefix(item.Config)})
	}
	stamp, err := buildReadyStamp(appCfg.Port, appCfg.HomeURL, rs)
	if err != nil {
		slog.Error("Failed to build ready stamp", "error", err)
		return
	}
	fmt.Println(stamp)
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
