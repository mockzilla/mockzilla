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
	// per registered service, etc. Errors still surface.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelError,
	})))

	integrationtest.SetSpecsFS(specsFS)

	var specPaths []string
	if s := os.Getenv("SPEC"); s != "" {
		specPaths = append(specPaths, s)
	}
	specPaths = append(specPaths, integrationtest.ParseSpecsEnv(os.Getenv("SPECS"))...)

	specs := integrationtest.CollectSpecs(t, specPaths)
	specs = excludeMarkedSpecs(specs)

	// Skip oversized specs (default 10MB via MAX_SPEC_SIZE_MB). Matches
	// the codegen integration suite — both pipelines hit libopenapi
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

	t.Cleanup(func() {
		stats.printSummary(totalSpecs)
		if cache != nil {
			if err := cache.Save(); err != nil {
				t.Logf("Failed to save cache: %v", err)
			}
		}
	})

	client := &http.Client{Timeout: requestTimeout}

	// cacheSaveMu serializes incremental cache flushes so we don't write
	// the file from multiple goroutines simultaneously. Save itself is
	// cheap (small JSON), and saving per-spec means a SIGKILL'd run
	// still preserves progress on every spec that completed.
	var cacheSaveMu sync.Mutex

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

			// Pre-flight lint. Specs that hit a known unsatisfiable
			// pattern (see pkg/lint) can never pass response validation
			// regardless of generator quality, so booting them is
			// wasted work. Record as skipped and surface in summary.
			if defects, err := lint.Spec(spec); err == nil && len(defects) > 0 {
				result := specResult{lintDefects: defects}
				stats.record(spec, result, cache)
				stats.printSpecLine(spec, result, totalSpecs)
				return
			}

			result := runOneSpec(ctx, spec, client)
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

func (s *portableStats) printSummary(totalSpecs int) {
	p := s.passedSpecs.Load()
	f := s.failedSpecs.Load()
	l := s.lintSkippedSpecs.Load()
	if p+f+l == 0 {
		return
	}

	totalOps := s.totalRoutesTested.Load()
	totalFails := s.totalRouteFailures.Load()
	totalOK := totalOps - totalFails
	bootTotal := time.Duration(s.totalBootNs.Load())
	testTotal := time.Duration(s.totalTestNs.Load())

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "=== Portable Integration Results ===")
	fmt.Fprintf(os.Stderr, "Total operations tested: %d\n", totalOps)
	fmt.Fprintf(os.Stderr, "OK: %d   Failures: %d\n", totalOK, totalFails)
	fmt.Fprintln(os.Stderr, "========================================")
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

func runOneSpec(ctx context.Context, specPath string, client *http.Client) specResult {
	res := specResult{}

	bootStart := time.Now()

	specBytes, err := integrationtest.ReadSpecFileWithBaseDir(specPath, "testdata/specs")
	if err != nil {
		res.bootErr = fmt.Errorf("read spec: %w", err)
		return res
	}

	root, cleanup, err := materializeSpec(specPath, specBytes)
	if err != nil {
		res.bootErr = fmt.Errorf("materialize: %w", err)
		return res
	}
	defer cleanup()

	setup, err := portable.BuildSetup([]string{root})
	if err != nil {
		res.bootErr = fmt.Errorf("build setup: %w", err)
		return res
	}

	// Any resolved-but-not-registered service is a spec or validator
	// failure the integration suite must surface. The server keeps
	// running in production, but for tests "service silently dropped"
	// is the kind of regression this suite exists to catch.
	if len(setup.Failed) > 0 {
		first := setup.Failed[0]
		res.bootErr = fmt.Errorf("service %q failed to register: %w", first.Name, first.Err)
		return res
	}

	ts := httptest.NewServer(setup.Router)
	defer ts.Close()

	res.bootDuration = time.Since(bootStart)

	testStart := time.Now()
	for _, svc := range setup.Services {
		if ctx.Err() != nil {
			break
		}
		failures, n := testService(ctx, client, ts.URL, svc.Name)
		res.routesTested += n
		res.failures = append(res.failures, failures...)
	}
	res.testDuration = time.Since(testStart)

	return res
}

func materializeSpec(specPath string, specBytes []byte) (root string, cleanup func(), err error) {
	ext := filepath.Ext(specPath)
	name := strings.TrimSuffix(filepath.Base(specPath), ext)
	root, err = os.MkdirTemp("", "portable-int-")
	if err != nil {
		return "", nil, err
	}
	svcDir := filepath.Join(root, "services", name)
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		_ = os.RemoveAll(root)
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(svcDir, "openapi"+ext), specBytes, 0o644); err != nil {
		_ = os.RemoveAll(root)
		return "", nil, err
	}

	// Eager parsing: the suite exercises every endpoint of every spec,
	// so on-demand parsing would just defer the same work behind first
	// requests. Eager surfaces spec-parse failures at boot — easier to
	// attribute to a spec, and faster overall.
	cfgBody := "spec:\n  lazyLoad: false\nvalidate:\n  request: true\n  response: true\n"
	if err := os.WriteFile(filepath.Join(svcDir, "config.yml"), []byte(cfgBody), 0o644); err != nil {
		_ = os.RemoveAll(root)
		return "", nil, err
	}
	return root, func() { _ = os.RemoveAll(root) }, nil
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
	defer func() { _ = resp.Body.Close() }()

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
