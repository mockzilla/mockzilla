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
	"sync"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/lmittmann/tint"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/factory"
	"github.com/mockzilla/mockzilla/v2/pkg/middleware"
	validator "github.com/pb33f/libopenapi-validator"
	validatorconfig "github.com/pb33f/libopenapi-validator/config"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	exitCodeShutdown = 0
	exitCodeError    = 1
)

// Setup is a fully-configured portable mode ready to be served. Run uses it for
// the normal CLI flow; in-process tests wrap Router with httptest.NewServer.
type Setup struct {
	Router   *api.Router
	AppCfg   *config.AppConfig
	Services []Service

	// Failed lists services that resolved but couldn't be registered (parse or
	// validator failure). Run logs and continues so the server still boots;
	// strict callers (integration tests) inspect this to fail loudly.
	Failed []FailedService

	handlers    map[string]*swappableHandler
	flags       flags
	validatorWG sync.WaitGroup
}

// WaitForValidators blocks until every background validator goroutine
// spawned during BuildSetup has finished, or ctx is cancelled. Returns
// true on success, false when ctx fired first (validators still running
// in the background; service-level validation stays off for them, same
// as the pre-call window). Production callers don't need this; tests
// use it so response-validation coverage is deterministic instead of
// racing the goroutines, and bound the wait because some pathological
// specs send libopenapi-validator's schema-render path into runaway
// recursion that never returns.
func (s *Setup) WaitForValidators(ctx context.Context) bool {
	start := time.Now()
	done := make(chan struct{})
	go func() {
		s.validatorWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("All validators ready", "elapsed", time.Since(start))
		return true
	case <-ctx.Done():
		slog.Warn("WaitForValidators ctx fired before all validators ready; proceeding with whatever built",
			"elapsed", time.Since(start),
			"reason", ctx.Err())
		return false
	}
}

type FailedService struct {
	Name string
	Err  error
}

// BuildSetup runs the portable startup pipeline up to port binding. Does NOT
// call configureLogger; Run does that, but in-process callers keep their own
// slog config.
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

	// If any service wants the root mount, move the UI off `/` so chi can host
	// the service there. The dotted `/.ui` aligns with `/.services`, `/.history`.
	// Skip when the UI is disabled (nothing mounted at `/` to conflict with).
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
	setup := &Setup{
		Router:   router,
		AppCfg:   appCfg,
		Services: services,
		handlers: handlers,
		flags:    fl,
	}
	for _, svc := range services {
		if err := registerService(router, svc, overrides, handlers, &setup.validatorWG); err != nil {
			slog.Error("Failed to register service", "name", svc.Name, "error", err)
			setup.Failed = append(setup.Failed, FailedService{Name: svc.Name, Err: err})
			continue
		}
	}

	// Server boots even when zero services registered. The UI and internal
	// routes are still useful, and operators can fix specs without
	// restarting the process.
	return setup, nil
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

// flagName strips the leading dashes from a flag arg ("--port" -> "port").
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

// anyServiceClaimsRoot approximates whether any service will end up mounted at "/".
// Without re-reading every config.yml here, we use the signals at hand: an empty
// Name defaults to "/", and a CLI --mount (single-service only) overrides everything.
func anyServiceClaimsRoot(services []Service, fl flags) bool {
	if fl.mount != "" {
		return fl.mount == "/"
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
	validatorWG *sync.WaitGroup,
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

	// Honor the per-service `spec:` block (lazyLoad, simplify, etc.).
	// Tests can force eager parsing via `spec: { lazyLoad: false }` in
	// config.yml to surface spec-parse failures at startup rather than
	// on first request.
	opts = append(opts, factory.WithSpecOptions(svcCfg.SpecOptions))

	// Tag libopenapi's spec-parse warnings with the service name so
	// parallel runs (integration tests) can attribute interleaved logs,
	// and so the host logger's level/format settings apply to them.
	opts = append(opts, factory.WithLogger(slog.Default().With("service", svc.Name)))

	h, err := newHandler(specBytes, opts...)
	if err != nil {
		return fmt.Errorf("creating handler: %w", err)
	}

	var regOpts []api.HandlerOption
	validationOn := svcCfg.Validate.RequestEnabled() || svcCfg.Validate.ResponseEnabled()

	sw := &swappableHandler{
		handler:   h,
		buildFn:   func() (validator.Validator, error) { return buildValidator(h) },
		buildDone: make(chan struct{}),
	}
	handlers[svc.Name] = sw

	// Attach the validation middleware unconditionally. It's a cheap
	// no-op when no per-request side wants validation, but staying
	// attached lets X-Mockzilla-Validate-* headers opt into validation
	// for services that booted with it disabled. The validator itself
	// is built lazily by EnsureValidator on the first such request.
	mw := func(p *middleware.Params) func(http.Handler) http.Handler {
		return middleware.CreateValidationMiddleware(p, sw.ensureValidator, sw.MatchPath)
	}
	regOpts = append(regOpts, api.WithMiddleware(
		[]func(*middleware.Params) func(http.Handler) http.Handler{mw},
	))

	if validationOn {
		// Eager background build so services that boot with validation
		// on don't pay the build cost on the first request. Uses the
		// blocking WaitForValidator so validatorWG.Done fires only when
		// the build actually finishes; that's what Setup.WaitForValidators
		// hangs on. Request-path callers use EnsureValidator instead so a
		// long build doesn't pin the handler goroutine.
		validatorWG.Add(1)
		go func() {
			defer validatorWG.Done()
			start := time.Now()
			built := sw.WaitForValidator()
			if built == nil {
				slog.Warn("validator construction failed; service will run without validation",
					"service", svc.Name,
					"elapsed", time.Since(start))
				return
			}
			slog.Info("Validator ready", "service", svc.Name, "elapsed", time.Since(start))
		}()
	}

	router.RegisterService(svcCfg, sw, regOpts...)

	// The router silently skips on name/mount collisions; check the
	// registry to avoid logging a successful registration for a
	// service that was actually dropped.
	if _, ok := router.GetServices()[svc.Name]; ok {
		slog.Info("Registered service", "name", svc.Name, "mount", api.ServicePrefix(svcCfg))
	}
	return nil
}

// buildValidator returns a validator built from the handler's parsed
// spec document. Errors and panics are surfaced to the caller: when the
// service config opts into validation, the caller refuses to register
// the service rather than silently dropping validation. Spec
// problems should be visible at startup, not papered over.
//
// Patterns are intentionally not enforced. The mock generator can't
// satisfy arbitrary regexes (JS regex literals, prose-as-regex,
// adversarial lookarounds, format-vs-pattern conflicts), so checking
// them surfaces false positives instead of real bugs. The no-op
// RegexpEngine plugs into the validator's compile slot and makes every
// MatchString return true; type/required/format/etc. validation is
// untouched.
func buildValidator(h *handler) (v validator.Validator, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("validator construction panicked: %v", r)
			v = nil
		}
	}()

	doc, docErr := h.factory.Document()
	if docErr != nil {
		return nil, fmt.Errorf("parsing spec: %w", docErr)
	}

	built, vErrs := validator.NewValidator(doc, validatorconfig.WithRegexEngine(noopRegexpEngine))
	if len(vErrs) > 0 {
		return nil, fmt.Errorf("validator construction: %w", errors.Join(vErrs...))
	}
	return built, nil
}

// noopRegexpEngine compiles every pattern to a Regexp whose MatchString
// always returns true. See buildValidator for why patterns aren't
// enforced.
func noopRegexpEngine(string) (jsonschema.Regexp, error) {
	return matchAllRegexp{}, nil
}

type matchAllRegexp struct{}

func (matchAllRegexp) MatchString(string) bool { return true }
func (matchAllRegexp) String() string          { return ".*" }

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
