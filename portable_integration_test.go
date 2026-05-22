package mockzilla

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/internal/integrationtest"
	"github.com/mockzilla/mockzilla/v2/internal/portable"
	"github.com/mockzilla/mockzilla/v2/internal/portableintegration"
	"github.com/mockzilla/mockzilla/v2/pkg/lint"
)

const (
	// requestTimeout bounds any single HTTP roundtrip against the
	// in-process portable server. Without it a hung handler would
	// freeze the whole suite (default client has no timeout).
	requestTimeout = 30 * time.Second

	// validatorWaitTimeout bounds Setup.WaitForValidators so a single
	// runaway validator construction doesn't deadlock the whole batch.
	// libopenapi-validator's warmSchemaCaches recurses through inline
	// schema rendering and can stall on pathological specs (deep $ref
	// cycles, exotic discriminator shapes). Per-spec validators that
	// don't finish in time get the same treatment they got pre-wait
	// (validation skipped); the test still runs and the rest of the
	// batch isn't held hostage.
	validatorWaitTimeout = 30 * time.Second

	// portableCacheFileName is the on-disk cache of passing specs for
	// this suite. Kept separate from the codegen cache - the two suites
	// exercise different code paths and a spec can pass one while
	// failing the other.
	portableCacheFileName = ".portable-integration-cache.json"
)

// TestPortableIntegration runs the spec corpus through portable mode
// in-process. Per spec it builds an HTTP handler equivalent to
// `mockzilla <spec>`, hits /.services for the registered service,
// generates a payload per route, calls the endpoint with it, and
// asserts the server didn't 5xx.
//
// Env vars (same UX as TestIntegration):
//   - SPEC: single spec path (relative to testdata/specs or absolute)
//   - SPECS: space-separated list of spec paths
//   - MAX_FAILS: total endpoint failures across all specs before
//     remaining and in-flight subtests abort (default 200)
//   - NO_CACHE: bypass the on-disk pass cache without wiping it
//   - CLEAR_CACHE: wipe the cache, then run
//
// Concurrency is controlled by go test's -parallel flag.
func TestPortableIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping portable integration test in short mode")
	}

	// Orchestrator mode: when invoked at the top level (no PORTABLE_BATCH
	// guard set), hand off to the orchestrator which re-execs this test
	// once per size-bounded batch with PORTABLE_BATCH=1.
	if os.Getenv("PORTABLE_BATCH") != "1" {
		portableintegration.Run(t, portableintegration.Config{
			SpecsBaseDir:   specsBaseDir,
			CacheFileName:  portableCacheFileName,
			TestRunPattern: "^TestPortableIntegration$",
			BatchEnvVar:    "PORTABLE_BATCH",
		})
		return
	}

	// Silence portable runtime's INFO logs - one line per HTTP request,
	// per registered service, etc. Errors still surface. The handler
	// also scans Warn messages for validation-middleware skip/panic/
	// timeout decisions so the final summary can report what the
	// validator actually did across the run, without adding counters
	// to production code.
	resetValidationCounters()
	slog.SetDefault(slog.New(&countingHandler{
		base: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}),
	}))

	integrationtest.SetSpecsFS(specsFS)

	var specPaths []string
	if s := os.Getenv("SPEC"); s != "" {
		specPaths = append(specPaths, s)
	}
	specPaths = append(specPaths, integrationtest.ParseSpecsEnv(os.Getenv("SPECS"))...)

	specs := integrationtest.CollectSpecs(t, specPaths)
	specs = excludeMarkedSpecs(specs)

	// Skip oversized specs (default 10MB via MAX_SPEC_SIZE_MB). Matches
	// the codegen integration suite; both pipelines hit libopenapi
	// memory pressure on the multi-megabyte specs (stripe, clarifai,
	// AWS), and the marginal coverage isn't worth the run-time cost.
	runtimeOpts := integrationtest.NewRuntimeOptionsFromEnv()
	if filtered, excluded := integrationtest.FilterSpecsBySize(specs, runtimeOpts.MaxSpecSizeBytes); excluded > 0 {
		t.Logf("Excluded %d spec(s) larger than %dMB", excluded, runtimeOpts.MaxSpecSizeMB)
		specs = filtered
	}

	if len(specs) == 0 {
		t.Skip("No specs to process")
	}

	maxFailsAllowed := 200
	if v := os.Getenv("MAX_FAILS"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &maxFailsAllowed); err != nil {
			t.Logf("Ignoring unparseable MAX_FAILS=%q", v)
		}
	}

	cache := loadPortableCache(t)
	// When the operator names specific specs (SPEC or SPECS), bypass the
	// pass-cache. They asked for those specs explicitly; silently
	// short-circuiting on "already passed last time" is surprising and
	// makes targeted re-runs require CLEAR_CACHE=1 every time. The cache
	// still records the result; it just doesn't skip the run.
	explicitTargets := len(specPaths) > 0
	if cache != nil && cache.Size() > 0 && !explicitTargets && os.Getenv("CLEAR_CACHE") == "" {
		before := len(specs)
		specs = cache.FilterUncached(specs)
		if skipped := before - len(specs); skipped > 0 {
			t.Logf("Skipping %d cached passing spec(s)", skipped)
		}
	}
	if len(specs) == 0 {
		t.Logf("All specs cached as passing - use CLEAR_CACHE=1 to retest")
		return
	}

	// Shared cancel so in-flight subtests abort when MAX_FAILS hits or
	// the test framework times out. Each request uses this context so
	// pending HTTP roundtrips fail fast instead of running to timeout.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stats := &portableStats{}
	totalSpecs := len(specs)
	t.Logf("Running portable integration on %d spec(s)", totalSpecs)

	// Declared early so the cleanup closure below can capture it; the
	// batch-setup block further down populates it.
	var batchMat *batchMaterialization

	t.Cleanup(func() {
		// Capture batchMat once at cleanup time; it's the same pointer
		// the orchestrator-driven test loop populated. svcToSpec is the
		// reverse of specToSvc so VALIDATION_INCIDENT lines can carry
		// the spec they belong to.
		svcToSpec := map[string]string{}
		if batchMat != nil {
			for spec, svc := range batchMat.specToSvc {
				svcToSpec[svc] = spec
			}
		}
		stats.printSummary(totalSpecs, svcToSpec)
		if cache != nil {
			if err := cache.Save(); err != nil {
				t.Logf("Failed to save cache: %v", err)
			}
		}
	})

	// All parallel goroutines in a batch share one httptest.Server and
	// therefore one host:port. The default Transport's
	// MaxIdleConnsPerHost=2 is smaller than MAX_CONCURRENCY (default 4),
	// so the extra parallel goroutines open and then evict connections
	// repeatedly. Each eviction churns a client-side ephemeral port
	// through TIME_WAIT and over a 2000+ spec corpus exhausts the
	// kernel's port pool. Pool of 16 is comfortable headroom over the
	// usual MAX_CONCURRENCY range.
	transport := &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	}
	client := &http.Client{Timeout: requestTimeout, Transport: transport}
	t.Cleanup(transport.CloseIdleConnections)

	// cacheSaveMu serializes incremental cache flushes so we don't write
	// the file from multiple goroutines simultaneously. Save itself is
	// cheap (small JSON), and saving per-spec means a SIGKILL'd run
	// still preserves progress on every spec that completed.
	var cacheSaveMu sync.Mutex

	// Pre-flight lint everything in parallel. Specs flagged as
	// known-unsatisfiable never make it onto the shared server below.
	lintSkipped := preflightLint(specs)

	// Specs that survived lint go into one shared portable Setup so
	// the whole batch runs against a single httptest.Server. Previous
	// approach (fresh server per spec) consumed an ephemeral port per
	// spec; over a 2000+ spec corpus that exhausted macOS's TIME_WAIT
	// pool. One server per batch reduces port pressure ~10x.
	var (
		toTest     []string
		batchBoot  time.Duration
		batchSetup *portable.Setup
		batchURL   string
		setupErr   error
		failByName = map[string]error{}
	)
	for _, spec := range specs {
		if _, ok := lintSkipped[spec]; !ok {
			toTest = append(toTest, spec)
		}
	}
	if len(toTest) > 0 {
		bootStart := time.Now()
		mat, err := materializeBatch(toTest)
		if err != nil {
			setupErr = fmt.Errorf("materialize batch: %w", err)
		} else {
			batchMat = mat
			t.Cleanup(mat.cleanup)
			setupBuilt := time.Now()
			batchSetup, err = portable.BuildSetup([]string{mat.root})
			if err != nil {
				setupErr = fmt.Errorf("build setup: %w", err)
			} else {
				buildElapsed := time.Since(setupBuilt)

				// Validators are built in background goroutines per service.
				// Without this wait, the per-spec test loop races them: a
				// request that arrives before the validator is ready skips
				// validation entirely (middleware no-ops on nil validator),
				// hiding real response-schema failures and producing flaky
				// pass/fail per spec across runs.
				//
				// Bounded: libopenapi-validator's schema-render path can go
				// into runaway recursion on some specs (paypal, etc) and
				// never return. We yield after validatorWaitTimeout and let
				// the per-spec tests run; specs whose validator didn't
				// finish exit the batch the same way they did pre-fix,
				// with validation skipped.
				waitStart := time.Now()
				waitCtx, waitCancel := context.WithTimeout(ctx, validatorWaitTimeout)
				ready := batchSetup.WaitForValidators(waitCtx)

				waitCancel()

				waitElapsed := time.Since(waitStart)
				status := "ready"
				if !ready {
					status = "TIMED OUT"
				}

				fmt.Fprintf(os.Stderr, "  batch: %d service(s), spec parse=%s, validator wait=%s (%s)\n",
					len(toTest), formatShortDuration(buildElapsed), formatShortDuration(waitElapsed), status)

				for _, f := range batchSetup.Failed {
					failByName[f.Name] = f.Err
				}

				ts := httptest.NewServer(batchSetup.Router)
				batchURL = ts.URL
				t.Cleanup(ts.Close)
			}
		}
		batchBoot = time.Since(bootStart)
	}

	// Amortise the batch boot across every spec that participated so
	// the per-spec "boot=Xms" column on the output line stays
	// meaningful and the Boot total still equals real time spent.
	bootPerSpec := time.Duration(0)
	if len(toTest) > 0 {
		bootPerSpec = batchBoot / time.Duration(len(toTest))
	}

	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			t.Parallel()
			if stats.totalRouteFailures.Load() >= int64(maxFailsAllowed) {
				t.SkipNow()
				return
			}

			// Announce before any expensive work so a SIGKILL'd batch
			// leaves a "start" line without a matching completion,
			// pointing at the spec that was in flight.
			fmt.Fprintf(os.Stderr, "  start %s\n", spec)

			if defects, ok := lintSkipped[spec]; ok {
				result := specResult{lintDefects: defects}
				stats.record(spec, result, cache)
				stats.printSpecLine(spec, result, totalSpecs)
				return
			}

			var result specResult
			switch {
			case setupErr != nil:
				result = specResult{bootErr: setupErr, bootDuration: bootPerSpec}
			default:
				svcName := batchMat.specToSvc[spec]
				if regErr, ok := failByName[svcName]; ok {
					result = specResult{
						bootErr:      fmt.Errorf("service failed to register: %w", regErr),
						bootDuration: bootPerSpec,
					}
				} else {
					result = runOneSpecOnServer(ctx, client, batchURL, svcName)
					result.bootDuration = bootPerSpec
				}
			}

			stats.record(spec, result, cache)
			stats.printSpecLine(spec, result, totalSpecs)

			if cache != nil {
				cacheSaveMu.Lock()
				_ = cache.Save() // best-effort - next spec will retry
				cacheSaveMu.Unlock()
			}

			if result.failed() {
				if int(stats.totalRouteFailures.Load()) >= maxFailsAllowed {
					cancel()
				}
				t.Fail() // status already shown in the live spec line; no message needed
			}
		})
	}
}

type specResult struct {
	bootDuration time.Duration
	testDuration time.Duration
	routesTested int
	failures     []routeFailure
	bootErr      error
	// lintDefects, when populated, indicates the spec was skipped by the
	// pre-flight lint pass and never booted. Reported as a separate
	// outcome from pass/fail so flaky-defect specs surface clearly.
	lintDefects []lint.Defect
}

type routeFailure struct {
	method string
	path   string
	phase  string // "generate" or "endpoint"
	detail string
}

func (r specResult) failed() bool {
	return r.bootErr != nil || len(r.failures) > 0
}

func (r specResult) lintSkipped() bool {
	return len(r.lintDefects) > 0
}

type portableStats struct {
	passedSpecs        atomic.Int64
	failedSpecs        atomic.Int64
	lintSkippedSpecs   atomic.Int64
	totalRoutesTested  atomic.Int64
	totalRouteFailures atomic.Int64
	totalBootNs        atomic.Int64
	totalTestNs        atomic.Int64
	completedSpecs     atomic.Int64

	mu      sync.Mutex
	results []specRow // appended under mu
}

type specRow struct {
	spec   string
	result specResult
}

func (s *portableStats) record(spec string, r specResult, cache *integrationtest.ResultCache) {
	s.totalRoutesTested.Add(int64(r.routesTested))
	s.totalRouteFailures.Add(int64(len(r.failures)))
	s.totalBootNs.Add(int64(r.bootDuration))
	s.totalTestNs.Add(int64(r.testDuration))

	s.mu.Lock()
	s.results = append(s.results, specRow{spec: spec, result: r})
	s.mu.Unlock()

	switch {
	case r.lintSkipped():
		s.lintSkippedSpecs.Add(1)
		if cache != nil {
			cache.MarkPassed(spec)
		}
	case r.failed():
		s.failedSpecs.Add(1)
		if cache != nil {
			cache.MarkFailed(spec)
		}
	default:
		s.passedSpecs.Add(1)
		if cache != nil {
			cache.MarkPassed(spec)
		}
	}
}

func (s *portableStats) printSpecLine(spec string, r specResult, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.completedSpecs.Add(1)
	if r.lintSkipped() {
		fmt.Fprintf(os.Stderr, "  [%d/%d] LINT %s (%d defects, %s)\n",
			i, total, spec, len(r.lintDefects), lintRuleSummary(r.lintDefects))
		return
	}
	status := "ok"
	if r.failed() {
		status = "FAIL"
	}
	if r.bootErr != nil {
		fmt.Fprintf(os.Stderr, "  [%d/%d] %s %s (boot error: %v)\n", i, total, status, spec, r.bootErr)
		return
	}
	fmt.Fprintf(os.Stderr, "  [%d/%d] %s %s (boot=%s test=%s, %d routes, %d failures)\n",
		i, total, status, spec,
		formatShortDuration(r.bootDuration), formatShortDuration(r.testDuration),
		r.routesTested, len(r.failures))
}

func lintRuleSummary(defects []lint.Defect) string {
	counts := map[string]int{}
	for _, d := range defects {
		counts[d.Rule]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, " ")
}

func (s *portableStats) printSummary(totalSpecs int, svcToSpec map[string]string) {
	p := s.passedSpecs.Load()
	f := s.failedSpecs.Load()
	l := s.lintSkippedSpecs.Load()
	if p+f+l == 0 {
		return
	}

	totalOps := s.totalRoutesTested.Load()
	totalFails := s.totalRouteFailures.Load()
	// totalOK is the count of operations that passed. Boot/list failures
	// increment totalFails without contributing to totalOps, so clamp to
	// zero; the previous arithmetic could produce negative counts in
	// the per-batch summary when many specs failed before any route was
	// exercised.
	totalOK := totalOps - totalFails
	if totalOK < 0 {
		totalOK = 0
	}
	bootTotal := time.Duration(s.totalBootNs.Load())
	testTotal := time.Duration(s.totalTestNs.Load())

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "=== Portable Integration Results ===")
	fmt.Fprintf(os.Stderr, "Total operations tested: %d\n", totalOps)
	fmt.Fprintf(os.Stderr, "OK: %d   Failures: %d\n", totalOK, totalFails)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, validationSummary())
	// Machine-parseable single-line variants the orchestrator picks up
	// to aggregate validation activity across every batch. Kept
	// separate from the human summary above so the per-batch output
	// stays readable. Stats line lists per-bucket totals; incident
	// lines name the individual panicked/timed-out routes.
	if line := validationStatsLine(); line != "" {
		fmt.Fprintln(os.Stderr, line)
	}
	fmt.Fprint(os.Stderr, validationIncidentLines(svcToSpec))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "=== Services Summary ===")

	s.mu.Lock()
	rows := make([]specRow, len(s.results))
	copy(rows, s.results)
	s.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].spec < rows[j].spec })

	for _, row := range rows {
		if row.result.lintSkipped() {
			fmt.Fprintf(os.Stderr, "  LINT %-60s defects=%-3d (%s)\n",
				row.spec, len(row.result.lintDefects), lintRuleSummary(row.result.lintDefects))
			continue
		}
		status := "OK  "
		if row.result.failed() {
			status = "FAIL"
		}
		fails := len(row.result.failures)
		fmt.Fprintf(os.Stderr, "  %s %-60s ops=%-4d fails=%-3d boot=%6s test=%7s\n",
			status, row.spec, row.result.routesTested, fails,
			formatShortDuration(row.result.bootDuration),
			formatShortDuration(row.result.testDuration))
	}

	skipped := int64(totalSpecs) - (p + f + l)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  Total: %d specs (passed: %d, failed: %d, lint-skipped: %d, runtime-skipped: %d), %d operations tested\n",
		totalSpecs, p, f, l, skipped, totalOps)
	fmt.Fprintf(os.Stderr, "         OK: %d   Failures: %d\n", totalOK, totalFails)
	fmt.Fprintf(os.Stderr, "         Boot: %s   Test: %s   Total: %s\n",
		formatDuration(bootTotal), formatDuration(testTotal), formatDuration(bootTotal+testTotal))

	if l > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Lint-skipped specs (spec contains constructs that strict validators reject):")
		for _, row := range rows {
			if !row.result.lintSkipped() {
				continue
			}
			first := row.result.lintDefects[0]
			fmt.Fprintf(os.Stderr, "  %s\n    %s at %s: %s\n",
				row.spec, first.Rule, first.Path, first.Detail)
		}
	}

	if f > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Failing specs (first failure per spec):")
		for _, row := range rows {
			if !row.result.failed() {
				continue
			}
			if row.result.bootErr != nil {
				fmt.Fprintf(os.Stderr, "  %s\n    boot: %s\n",
					row.spec, truncate([]byte(row.result.bootErr.Error()), 200))
				continue
			}
			if len(row.result.failures) == 0 {
				continue
			}
			first := row.result.failures[0]
			fmt.Fprintf(os.Stderr, "  %s\n    %s %s [%s]: %s\n",
				row.spec, first.method, first.path, first.phase,
				truncate([]byte(first.detail), 8000))
		}
	}
	fmt.Fprintln(os.Stderr, "========================================")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d.Minutes())
	secs := d.Seconds() - float64(mins*60)
	return fmt.Sprintf("%dm%.0fs", mins, secs)
}

// formatShortDuration renders sub-second values in ms so small specs
// don't all collapse to "0.0s" in the output - small specs are real
// work, just fast. Anything >= 1s uses seconds with one decimal.
func formatShortDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func loadPortableCache(t *testing.T) *integrationtest.ResultCache {
	if os.Getenv("NO_CACHE") != "" {
		return nil
	}
	cache, err := integrationtest.NewResultCacheNamed(".", portableCacheFileName)
	if err != nil {
		t.Logf("Failed to load portable cache: %v", err)
		return nil
	}
	if os.Getenv("CLEAR_CACHE") != "" {
		if err := cache.Clear(); err != nil {
			t.Logf("Failed to clear cache: %v", err)
		} else {
			t.Logf("Portable cache cleared")
		}
	}
	return cache
}

// runOneSpecOnServer drives the per-spec test phase (list routes, then
// generate + call each endpoint) against a server that's already up
// for the whole batch. Boot is recorded separately by the caller.
func runOneSpecOnServer(ctx context.Context, client *http.Client, baseURL, svcName string) specResult {
	res := specResult{}
	testStart := time.Now()
	failures, n := testService(ctx, client, baseURL, svcName)
	res.testDuration = time.Since(testStart)
	res.routesTested = n
	res.failures = failures
	return res
}

// batchMaterialization writes a batch of specs into one shared
// services/<name>/ tree that portable.BuildSetup discovers in a single
// pass. The service names are index-based so two specs with the same
// basename in the batch don't collide.
type batchMaterialization struct {
	root      string
	specToSvc map[string]string // original spec path -> service name
	cleanup   func()
}

func materializeBatch(specs []string) (*batchMaterialization, error) {
	root, err := os.MkdirTemp("", "portable-int-batch-")
	if err != nil {
		return nil, err
	}
	servicesRoot := filepath.Join(root, "services")
	if err := os.MkdirAll(servicesRoot, 0o755); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}

	// Eager parsing: the suite exercises every endpoint of every spec,
	// so on-demand parsing would just defer the same work behind first
	// requests. Eager also surfaces spec-parse failures at boot, which
	// the orchestrator wants visible as a per-spec failure.
	// 5s validation timeout (vs the 1s default) gives complex POST
	// bodies in real-world specs (sinao, invoicing APIs with deeply
	// nested allOf) enough budget to evaluate instead of timing out
	// and skipping; without this the suite reports legitimate timeouts
	// as if they were spec defects.
	cfgBody := []byte("spec:\n  lazyLoad: false\nvalidate:\n  request: true\n  response: true\n  verbose: true\n  timeout: 5s\n")

	specToSvc := make(map[string]string, len(specs))
	for i, spec := range specs {
		specBytes, err := integrationtest.ReadSpecFileWithBaseDir(spec, "testdata/specs")
		if err != nil {
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("read %s: %w", spec, err)
		}
		svcName := fmt.Sprintf("s%04d", i)
		svcDir := filepath.Join(servicesRoot, svcName)
		if err := os.MkdirAll(svcDir, 0o755); err != nil {
			_ = os.RemoveAll(root)
			return nil, err
		}
		ext := filepath.Ext(spec)
		if err := os.WriteFile(filepath.Join(svcDir, "openapi"+ext), specBytes, 0o644); err != nil {
			_ = os.RemoveAll(root)
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(svcDir, "config.yml"), cfgBody, 0o644); err != nil {
			_ = os.RemoveAll(root)
			return nil, err
		}
		specToSvc[spec] = svcName
	}

	return &batchMaterialization{
		root:      root,
		specToSvc: specToSvc,
		cleanup:   func() { _ = os.RemoveAll(root) },
	}, nil
}

// preflightLint runs lint.Spec for every spec with bounded parallelism
// and returns the map of spec path -> defects for specs that should be
// skipped. The work was previously inline in each t.Run; doing it
// upfront lets the batch materialiser skip lint-failed specs entirely
// (saves writing them to disk and registering services for them).
func preflightLint(specs []string) map[string][]lint.Defect {
	out := make(map[string][]lint.Defect)
	var mu sync.Mutex

	const workers = 8
	jobs := make(chan string, len(specs))
	for _, s := range specs {
		jobs <- s
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for spec := range jobs {
				defects, err := lint.Spec(spec)
				if err != nil || len(defects) == 0 {
					continue
				}
				mu.Lock()
				out[spec] = defects
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

func testService(ctx context.Context, client *http.Client, baseURL, serviceName string) ([]routeFailure, int) {
	urlName := serviceName
	if urlName == "" {
		urlName = ".root"
	}

	routes, err := listRoutes(ctx, client, baseURL, urlName)
	if err != nil {
		return []routeFailure{{phase: "list", detail: err.Error()}}, 0
	}

	mountPrefix := "/" + serviceName
	if serviceName == "" {
		mountPrefix = ""
	}

	conc := routeConcurrency()
	if conc <= 1 || len(routes) <= 1 {
		var failures []routeFailure
		for _, route := range routes {
			if ctx.Err() != nil {
				break
			}
			if f, ok := testOneRoute(ctx, client, baseURL, urlName, mountPrefix, route); !ok {
				failures = append(failures, f)
			}
		}
		return failures, len(routes)
	}

	// Fan routes out to conc workers. The mock handler is stateless per
	// request, so the in-process httptest.Server handles them in parallel
	// fine; the win is amortising response-generation latency on huge
	// specs (docusign et al) where the spec is parsed once but the
	// per-route factory.Response is non-trivial.
	type result struct {
		failure routeFailure
		failed  bool
	}
	jobs := make(chan routeInfo)
	results := make(chan result, len(routes))
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for route := range jobs {
				if ctx.Err() != nil {
					return
				}
				if f, ok := testOneRoute(ctx, client, baseURL, urlName, mountPrefix, route); !ok {
					results <- result{failure: f, failed: true}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, route := range routes {
			if ctx.Err() != nil {
				return
			}
			jobs <- route
		}
	}()
	wg.Wait()
	close(results)

	var failures []routeFailure
	for r := range results {
		if r.failed {
			failures = append(failures, r.failure)
		}
	}
	return failures, len(routes)
}

// routeConcurrency reads the ROUTE_CONCURRENCY env var. Default 4 is
// the empirical sweet spot from docusign (4m15s sequential → 1m40s at
// 4); past 8 hits HTTP transport defaults and starts flaking without
// further speedup. Independent of MAX_CONCURRENCY (which sets
// `-test.parallel`, i.e. how many specs run at once); on a single big
// spec running in its own batch, the spec parallelism doesn't help, so
// this knob fans out routes within a spec.
const defaultRouteConcurrency = 4

func routeConcurrency() int {
	v := os.Getenv("ROUTE_CONCURRENCY")
	if v == "" {
		return defaultRouteConcurrency
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return defaultRouteConcurrency
	}
	return n
}

type routeInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type generatedResponse struct {
	Body        any    `json:"body"`
	ContentType string `json:"contentType"`
	Path        string `json:"path"`
}

func listRoutes(ctx context.Context, client *http.Client, baseURL, urlName string) ([]routeInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/.services/"+urlName, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /.services/%s: %w", urlName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /.services/%s: status %d body=%s", urlName, resp.StatusCode, truncate(body, 200))
	}

	var out struct {
		Endpoints []routeInfo `json:"endpoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode routes: %w", err)
	}
	return out.Endpoints, nil
}

func testOneRoute(ctx context.Context, client *http.Client, baseURL, urlName, mountPrefix string, route routeInfo) (routeFailure, bool) {
	gen, err := generatePayload(ctx, client, baseURL, urlName, route)
	if err != nil {
		return routeFailure{method: route.Method, path: route.Path, phase: "generate", detail: err.Error()}, false
	}

	targetPath := gen.Path
	if targetPath == "" {
		targetPath = route.Path
	}
	if strings.HasPrefix(targetPath, "{") && strings.HasSuffix(targetPath, "}") && !strings.HasPrefix(targetPath, "{{") {
		return routeFailure{method: route.Method, path: route.Path, phase: "generate", detail: "unreplaced placeholders: " + targetPath}, false
	}

	if err := callEndpoint(ctx, client, baseURL+mountPrefix+targetPath, route.Method, gen); err != nil {
		return routeFailure{method: route.Method, path: route.Path, phase: "endpoint", detail: err.Error()}, false
	}
	return routeFailure{}, true
}

func generatePayload(ctx context.Context, client *http.Client, baseURL, urlName string, route routeInfo) (generatedResponse, error) {
	body, _ := json.Marshal(map[string]string{"path": route.Path, "method": route.Method})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/.services/"+urlName, bytes.NewReader(body))
	if err != nil {
		return generatedResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return generatedResponse{}, errors.New("connection closed (server panic?)")
		}
		return generatedResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return generatedResponse{}, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(respBody, 200))
	}

	var gen generatedResponse
	if err := json.Unmarshal(respBody, &gen); err != nil {
		return generatedResponse{}, fmt.Errorf("decode payload: %w", err)
	}
	return gen, nil
}

func callEndpoint(ctx context.Context, client *http.Client, url, method string, gen generatedResponse) error {
	var body io.Reader
	if gen.Body != nil {
		b, _ := json.Marshal(gen.Body)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if gen.ContentType != "" {
		req.Header.Set("Content-Type", gen.ContentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("connection closed (server panic?)")
		}
		return err
	}
	defer func() {
		// Drain before close so the underlying TCP connection goes back
		// to the keep-alive pool. Closing without reading abandons the
		// conn (per net/http docs), and over a 2000+ spec corpus that
		// churns enough ephemeral ports through TIME_WAIT to exhaust
		// macOS's port pool and produce EADDRNOTAVAIL on later dials.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, truncate(respBody, 8000))
	}
	return nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

// excludeMarkedSpecs filters out specs the corpus explicitly marks as
// "do not test": a leading `-` in the basename (the convention used
// across testdata/specs to flag broken/intentionally-skipped specs)
// or anything under a `/stash/` directory. integrationtest.CollectSpecs
// already enforces this on directory walks; when callers pass an
// explicit list of paths (as the corpus Makefile target does), the
// filter has to be re-applied here.
func excludeMarkedSpecs(specs []string) []string {
	out := specs[:0]
	for _, s := range specs {
		base := filepath.Base(s)
		if strings.HasPrefix(base, "-") || strings.Contains(s, "/stash/") {
			continue
		}
		out = append(out, s)
	}
	return out
}

// Validation-middleware activity counters. Populated by countingHandler
// from the slog Warn messages the middleware emits on every skip /
// panic / timeout. Only buckets the middleware actually logs are
// represented (request-side allPathMissing and 400-failures are silent
// in the middleware, so they have no counters here).
var (
	vcReqPanic   atomic.Int64
	vcReqTimeout atomic.Int64

	vcRespPanic           atomic.Int64
	vcRespTimeout         atomic.Int64
	vcRespSkipSchemaRend  atomic.Int64
	vcRespSkipAmbigOneOf  atomic.Int64
	vcRespSkipPathMissing atomic.Int64
	vcRespSkipJSRegex     atomic.Int64
	vcRespSkipPattern     atomic.Int64
	vcRespSkipUnsat       atomic.Int64
	vcRespSkipConfTypes   atomic.Int64
	vcRespSkipCTParams    atomic.Int64
	vcRespSkipWildcardCT  atomic.Int64
	vcRespSkipStatusCode  atomic.Int64
	vcRespSkipRouterPath  atomic.Int64
	vcRespFailed          atomic.Int64
)

// Captured panic/timeout incidents, pre-formatted at capture time as
// `VALIDATION_INCIDENT kind=... method=... path=... reason=...`. Stored
// without `spec=` (which the capture site doesn't know); the emitter
// resolves the leading /<svc>/ segment to a spec name and appends it.
var (
	validationIncidentsMu sync.Mutex
	validationIncidents   []string
)

func resetValidationCounters() {
	for _, c := range []*atomic.Int64{
		&vcReqPanic, &vcReqTimeout,
		&vcRespPanic, &vcRespTimeout, &vcRespSkipSchemaRend, &vcRespSkipAmbigOneOf,
		&vcRespSkipPathMissing, &vcRespSkipJSRegex, &vcRespSkipPattern, &vcRespSkipUnsat,
		&vcRespSkipConfTypes, &vcRespSkipCTParams, &vcRespSkipWildcardCT,
		&vcRespSkipStatusCode, &vcRespSkipRouterPath, &vcRespFailed,
	} {
		c.Store(0)
	}
	validationIncidentsMu.Lock()
	validationIncidents = nil
	validationIncidentsMu.Unlock()
}

func recordValidationIncident(kind string, r slog.Record) {
	var method, path, reason string
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "method":
			method = a.Value.String()
		case "path":
			path = a.Value.String()
		case "reason":
			reason = a.Value.String()
		}
		return true
	})
	// First line of the panic message is enough to distinguish "nil
	// pointer" vs "index out of range" vs spec defect; the full stack
	// would balloon the orchestrator output.
	if i := strings.IndexByte(reason, '\n'); i >= 0 {
		reason = reason[:i]
	}
	if len(reason) > 200 {
		reason = reason[:200] + "..."
	}
	validationIncidentsMu.Lock()
	validationIncidents = append(validationIncidents,
		fmt.Sprintf("VALIDATION_INCIDENT kind=%s method=%s path=%s reason=%s",
			kind, method, path, reason))
	validationIncidentsMu.Unlock()
}

// countingHandler bumps validation counters by pattern-matching the
// middleware's Warn message prefixes, then forwards to the base handler
// (which is Discard in tests). Keeps prod code untouched; the messages
// it matches are the ones the middleware already logs for operators.
type countingHandler struct {
	base slog.Handler
}

func (h *countingHandler) Enabled(_ context.Context, level slog.Level) bool {
	// Accept Warn so we see middleware decisions; pass everything else
	// through to the base handler's own level decision.
	if level >= slog.LevelWarn {
		return true
	}
	return h.base.Enabled(context.Background(), level)
}

func (h *countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		switch {
		case strings.HasPrefix(r.Message, "Request validator panicked"):
			vcReqPanic.Add(1)
			recordValidationIncident("req_panic", r)
		case strings.HasPrefix(r.Message, "Request validator exceeded timeout"):
			vcReqTimeout.Add(1)
			recordValidationIncident("req_timeout", r)
		case strings.HasPrefix(r.Message, "Response validator panicked"):
			vcRespPanic.Add(1)
			recordValidationIncident("resp_panic", r)
		case strings.HasPrefix(r.Message, "Response validator exceeded timeout"):
			vcRespTimeout.Add(1)
			recordValidationIncident("resp_timeout", r)
		case strings.HasPrefix(r.Message, "Validator schema render failed"):
			vcRespSkipSchemaRend.Add(1)
		case strings.HasPrefix(r.Message, "oneOf variants overlap"):
			vcRespSkipAmbigOneOf.Add(1)
		case strings.HasPrefix(r.Message, "Spec path not found by validator"):
			vcRespSkipPathMissing.Add(1)
		case strings.HasPrefix(r.Message, "Spec pattern uses JS regex literal"):
			vcRespSkipJSRegex.Add(1)
		case strings.HasPrefix(r.Message, "Spec pattern is prose"):
			vcRespSkipPattern.Add(1)
		case strings.HasPrefix(r.Message, "Spec schema is unsatisfiable"):
			vcRespSkipUnsat.Add(1)
		case strings.HasPrefix(r.Message, "Spec allOf has conflicting branch types"):
			vcRespSkipConfTypes.Add(1)
		case strings.HasPrefix(r.Message, "Spec content type only differs"):
			vcRespSkipCTParams.Add(1)
		case strings.HasPrefix(r.Message, "Spec declares wildcard"):
			vcRespSkipWildcardCT.Add(1)
		case strings.HasPrefix(r.Message, "Response status code not declared"):
			vcRespSkipStatusCode.Add(1)
		case strings.HasPrefix(r.Message, "libopenapi-validator matched a different spec path"):
			vcRespSkipRouterPath.Add(1)
		case strings.HasPrefix(r.Message, "Response validation failed"):
			vcRespFailed.Add(1)
		}
	}
	return h.base.Handle(ctx, r)
}

func (h *countingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &countingHandler{base: h.base.WithAttrs(attrs)}
}

func (h *countingHandler) WithGroup(name string) slog.Handler {
	return &countingHandler{base: h.base.WithGroup(name)}
}

// validationSummary returns a multi-line block of validation-middleware
// activity totals. Zero-everything when validation never ran (e.g.
// config disabled for the run).
func validationSummary() string {
	respSkipTotal := vcRespSkipSchemaRend.Load() + vcRespSkipAmbigOneOf.Load() +
		vcRespSkipPathMissing.Load() + vcRespSkipJSRegex.Load() + vcRespSkipPattern.Load() +
		vcRespSkipUnsat.Load() + vcRespSkipConfTypes.Load() + vcRespSkipCTParams.Load() +
		vcRespSkipWildcardCT.Load() + vcRespSkipStatusCode.Load() + vcRespSkipRouterPath.Load()

	var b strings.Builder
	fmt.Fprintln(&b, "=== Validation Activity ===")
	fmt.Fprintf(&b, "  request:  panic=%-3d  timeout=%-3d\n",
		vcReqPanic.Load(), vcReqTimeout.Load())
	fmt.Fprintf(&b, "  response: failed(500)=%-5d  panic=%-3d  timeout=%-3d  skip-heuristic=%-3d\n",
		vcRespFailed.Load(), vcRespPanic.Load(), vcRespTimeout.Load(), respSkipTotal)
	if respSkipTotal > 0 {
		fmt.Fprintln(&b, "  response skip breakdown:")
		printIfNonzero(&b, "    schema-render-fail   ", vcRespSkipSchemaRend.Load())
		printIfNonzero(&b, "    ambiguous-oneOf      ", vcRespSkipAmbigOneOf.Load())
		printIfNonzero(&b, "    path-missing         ", vcRespSkipPathMissing.Load())
		printIfNonzero(&b, "    js-regex-literal     ", vcRespSkipJSRegex.Load())
		printIfNonzero(&b, "    prose-pattern        ", vcRespSkipPattern.Load())
		printIfNonzero(&b, "    unsatisfiable-schema ", vcRespSkipUnsat.Load())
		printIfNonzero(&b, "    conflicting-allOf    ", vcRespSkipConfTypes.Load())
		printIfNonzero(&b, "    content-type-params  ", vcRespSkipCTParams.Load())
		printIfNonzero(&b, "    wildcard-content-type", vcRespSkipWildcardCT.Load())
		printIfNonzero(&b, "    status-code-missing  ", vcRespSkipStatusCode.Load())
		printIfNonzero(&b, "    router-path-mismatch ", vcRespSkipRouterPath.Load())
	}
	return b.String()
}

func printIfNonzero(b *strings.Builder, label string, v int64) {
	if v > 0 {
		fmt.Fprintf(b, "%s %d\n", label, v)
	}
}

// validationStatsLine emits one tagged line the orchestrator can parse
// to aggregate validation activity across batches. Zero-valued tokens
// are omitted so quiet batches produce a near-empty line instead of a
// long row of zeros; the orchestrator's parser tolerates missing keys.
// The line always starts with `VALIDATION_STATS` so the regex matches
// even when no fields follow.
func validationStatsLine() string {
	pairs := []struct {
		key   string
		value int64
	}{
		{"req_panic", vcReqPanic.Load()},
		{"req_timeout", vcReqTimeout.Load()},
		{"resp_failed", vcRespFailed.Load()},
		{"resp_panic", vcRespPanic.Load()},
		{"resp_timeout", vcRespTimeout.Load()},
		{"resp_skip_schema", vcRespSkipSchemaRend.Load()},
		{"resp_skip_ambig_oneof", vcRespSkipAmbigOneOf.Load()},
		{"resp_skip_path_missing", vcRespSkipPathMissing.Load()},
		{"resp_skip_js_regex", vcRespSkipJSRegex.Load()},
		{"resp_skip_pattern", vcRespSkipPattern.Load()},
		{"resp_skip_unsat", vcRespSkipUnsat.Load()},
		{"resp_skip_conflicting_allof", vcRespSkipConfTypes.Load()},
		{"resp_skip_ct_params", vcRespSkipCTParams.Load()},
		{"resp_skip_wildcard_ct", vcRespSkipWildcardCT.Load()},
		{"resp_skip_status_code", vcRespSkipStatusCode.Load()},
		{"resp_skip_router_path", vcRespSkipRouterPath.Load()},
	}
	var b strings.Builder
	for _, p := range pairs {
		if p.value == 0 {
			continue
		}
		fmt.Fprintf(&b, " %s=%d", p.key, p.value)
	}
	if b.Len() == 0 {
		return ""
	}
	return "VALIDATION_STATS" + b.String()
}

// validationIncidentLines returns one tagged line per captured panic /
// timeout. The svcToSpec map (built from the batch materialisation) is
// applied here so each line carries the spec it belongs to; the
// orchestrator groups on that to summarise multi-spec runs.
func validationIncidentLines(svcToSpec map[string]string) string {
	validationIncidentsMu.Lock()
	defer validationIncidentsMu.Unlock()
	if len(validationIncidents) == 0 {
		return ""
	}
	var b strings.Builder
	for _, line := range validationIncidents {
		b.WriteString(line)
		if spec := specForIncidentLine(line, svcToSpec); spec != "" {
			fmt.Fprintf(&b, " spec=%s", spec)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// specForIncidentLine pulls the `/sNNNN/` service prefix out of an
// incident's path= token and resolves it to the spec file via the
// batch's svc→spec map. Returns "" when the path doesn't have a
// resolvable service segment.
func specForIncidentLine(line string, svcToSpec map[string]string) string {
	const tag = "path="
	i := strings.Index(line, tag)
	if i < 0 {
		return ""
	}
	rest := line[i+len(tag):]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	rest = rest[1:]
	if k := strings.IndexByte(rest, '/'); k >= 0 {
		rest = rest[:k]
	}
	return svcToSpec[rest]
}

