// Package portableintegration orchestrates TestPortableIntegration
// across the full spec corpus. It packs specs into size-bounded batches
// via integrationtest.SplitIntoBatches and re-execs the test binary
// once per batch (with cfg.BatchEnvVar=1 set on the child) so each
// batch runs in a fresh OS process. The per-batch process boundary
// isolates libopenapi parser state between batches and bounds peak
// memory.
//
// The test entry point in the root package stays minimal: it forwards
// to Run when the batch env guard is unset, and runs the in-process
// per-spec loop when the child process picks it up.
package portableintegration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/internal/integrationtest"
)

// Config wires the orchestrator to a specific test entry point.
type Config struct {
	// SpecsBaseDir is the directory walked when no SPEC/SPECS env is set.
	SpecsBaseDir string
	// CacheFileName is the on-disk cache file (e.g. ".portable-integration-cache.json").
	CacheFileName string
	// TestRunPattern is the -run regex passed to the child go test.
	TestRunPattern string
	// BatchEnvVar is the env var set to "1" on the child to make it
	// take the per-batch in-process path instead of recursing into Run.
	BatchEnvVar string
}

const defaultBatchTimeout = 5 * time.Minute

// Run is the orchestrator entry point. The caller's test function
// invokes it when its batch-guard env var is unset.
func Run(t *testing.T, cfg Config) {
	wallStart := time.Now()
	opts := integrationtest.NewRuntimeOptionsFromEnv()

	var requested []string
	if s := os.Getenv("SPEC"); s != "" {
		requested = append(requested, s)
	}
	requested = append(requested, integrationtest.ParseSpecsEnv(os.Getenv("SPECS"))...)
	explicit := len(requested) > 0

	specs, err := collectSpecs(requested, cfg.SpecsBaseDir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(specs)

	if filtered, excluded := integrationtest.FilterSpecsBySize(specs, opts.MaxSpecSizeBytes); excluded > 0 {
		t.Logf("Excluded %d spec(s) larger than %dMB", excluded, opts.MaxSpecSizeMB)
		specs = filtered
	}
	totalAll := len(specs)

	cache, err := integrationtest.NewResultCacheNamed(".", cfg.CacheFileName)
	if err != nil {
		t.Logf("Failed to load cache: %v", err)
		cache = nil
	}
	if cache != nil && opts.ClearCache {
		if err := cache.Clear(); err != nil {
			t.Logf("Failed to clear cache: %v", err)
		} else {
			t.Logf("Cache cleared")
		}
	}

	cachedSkipped := 0
	if !explicit && !opts.NoCache && !opts.ClearCache && cache != nil && cache.Size() > 0 {
		before := len(specs)
		specs = cache.FilterUncached(specs)
		cachedSkipped = before - len(specs)
	}
	if len(specs) == 0 {
		t.Logf("All %d specs cached as passing - use CLEAR_CACHE=1 to retest", totalAll)
		return
	}

	batches := integrationtest.SplitIntoBatches(specs, opts.BatchSizeBytes)
	if cachedSkipped > 0 {
		fmt.Fprintf(os.Stderr, "=== %d uncached (%d cached, %d total) in %d batches (<= %dMB each) ===\n",
			len(specs), cachedSkipped, totalAll, len(batches), opts.BatchSizeMB)
	} else {
		fmt.Fprintf(os.Stderr, "=== %d specs in %d batches (<= %dMB each) ===\n",
			len(specs), len(batches), opts.BatchSizeMB)
	}

	// Build the test binary once and re-exec it per batch. Skipping
	// `go test`'s per-invocation build graph hashing saves roughly a
	// second per batch (4+ minutes across the full corpus).
	binaryPath, cleanup, buildErr := buildTestBinary(t.Context())
	if buildErr != nil {
		t.Fatalf("pre-build test binary: %v", buildErr)
	}
	defer cleanup()

	var agg aggregate
	failedBatches := 0
	for i, batch := range batches {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "=== Batch %d/%d: %d spec(s) ===\n", i+1, len(batches), len(batch))
		summary, err := runBatchChild(t.Context(), binaryPath, batch, cfg)
		agg.merge(summary)
		if err != nil {
			failedBatches++
			fmt.Fprintf(os.Stderr, "Batch %d/%d: %v\n", i+1, len(batches), err)
			if len(summary.inFlight) > 0 {
				fmt.Fprintf(os.Stderr, "Batch %d/%d: in-flight at kill: %s\n",
					i+1, len(batches), strings.Join(summary.inFlight, ", "))
			}
		}
	}

	agg.print(len(batches), failedBatches, time.Since(wallStart))

	if agg.failSpecs > 0 || failedBatches > 0 {
		t.Fail()
	}
}

func collectSpecs(requested []string, baseDir string) ([]string, error) {
	if len(requested) == 0 {
		return integrationtest.WalkSpecsDir(baseDir)
	}
	var out []string
	for _, p := range requested {
		resolved := resolveSpecPath(p, baseDir)
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("path not found: %s", p)
		}
		if info.IsDir() {
			sub, err := integrationtest.WalkSpecsDir(resolved)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		out = append(out, resolved)
	}
	return out, nil
}

func resolveSpecPath(p, baseDir string) string {
	if _, err := os.Stat(p); err == nil {
		return p
	}
	alt := filepath.Join(baseDir, p)
	if _, err := os.Stat(alt); err == nil {
		return alt
	}
	return p
}

// buildTestBinary compiles the current package's test binary once so
// the per-batch invocations skip `go test`'s build graph hashing. The
// returned cleanup removes the binary on exit.
func buildTestBinary(ctx context.Context) (string, func(), error) {
	binPath := filepath.Join(os.TempDir(), fmt.Sprintf("mockzilla-portable.%d.test", os.Getpid()))
	cmd := exec.CommandContext(ctx, "go", "test", "-c", "-o", binPath, ".")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", func() {}, fmt.Errorf("go test -c: %w", err)
	}
	cleanup := func() { _ = os.Remove(binPath) }
	return binPath, cleanup, nil
}

// runBatchChild re-execs the pre-built test binary on the given specs
// with the batch env var set so the child takes the in-process loop.
func runBatchChild(ctx context.Context, binPath string, specs []string, cfg Config) (batchSummary, error) {
	deadline := defaultBatchTimeout
	if v := os.Getenv("BATCH_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			deadline = d
		}
	}
	// Slight backstop so the child's own -timeout fires first.
	batchCtx, cancel := context.WithTimeout(ctx, deadline+30*time.Second)
	defer cancel()

	mc := os.Getenv("MAX_CONCURRENCY")
	if mc == "" {
		mc = "4"
	}
	args := []string{
		"-test.v",
		"-test.run=" + cfg.TestRunPattern,
		"-test.timeout=" + deadline.String(),
		"-test.count=1",
		"-test.parallel=" + mc,
	}

	cmd := exec.CommandContext(batchCtx, binPath, args...)
	// SPECS carries the orchestrator's per-batch list; unset SPEC and any
	// inherited SPECS so the child doesn't re-add the operator's
	// original single-spec arg on top of what we already packed for it.
	cmd.Env = append(os.Environ(),
		cfg.BatchEnvVar+"=1",
		"SPEC=",
		"SPECS="+strings.Join(specs, "\n"),
	)

	pr, pw, err := os.Pipe()
	if err != nil {
		return batchSummary{}, fmt.Errorf("pipe: %w", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return batchSummary{}, fmt.Errorf("start go test: %w", err)
	}
	_ = pw.Close()

	summary := scanStream(pr, os.Stderr)
	_ = pr.Close()

	if err := cmd.Wait(); err != nil {
		return summary, err
	}
	return summary, nil
}

type batchSummary struct {
	okSpecs   int
	failSpecs int
	lintSpecs int
	routes    int
	failures  int
	bootMs    float64
	testMs    float64
	failed    []string
	// inFlight: specs that printed a "start" line but never a completion.
	// On SIGKILL these are the candidates for what caused it.
	inFlight []string
}

var (
	// Drops go-test framework noise so live output reads like per-spec progress.
	noiseRx = regexp.MustCompile(`^(=== (RUN|PAUSE|CONT|NAME)|    --- (PASS|FAIL):)`)

	// Matches lines emitted by the per-spec test's printSpecLine
	// (ok/FAIL with timings or boot-error tail).
	specRx = regexp.MustCompile(`^\s*\[\d+/\d+\] (ok|FAIL) (.+?) \((?:boot=([\d.]+)(ms|s) test=([\d.]+)(ms|s), (\d+) routes, (\d+) failures|boot error: .+)\)\s*$`)

	lintRx = regexp.MustCompile(`^\s*\[\d+/\d+\] LINT (.+?) \(\d+ defects.*\)\s*$`)

	// Matches the per-subtest start announcement.
	startRx = regexp.MustCompile(`^\s*start (.+)$`)
)

func scanStream(r io.Reader, tee io.Writer) batchSummary {
	var s batchSummary
	started := map[string]struct{}{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if noiseRx.MatchString(line) {
			continue
		}
		_, _ = fmt.Fprintln(tee, line)

		if m := startRx.FindStringSubmatch(line); m != nil {
			started[m[1]] = struct{}{}
			continue
		}
		if m := specRx.FindStringSubmatch(line); m != nil {
			delete(started, m[2])
			if m[1] == "ok" {
				s.okSpecs++
			} else {
				s.failSpecs++
				s.failed = append(s.failed, m[2])
			}
			if m[3] != "" {
				s.bootMs += toMs(m[3], m[4])
				s.testMs += toMs(m[5], m[6])
				routes, _ := strconv.Atoi(m[7])
				fails, _ := strconv.Atoi(m[8])
				s.routes += routes
				s.failures += fails
			}
			continue
		}
		if m := lintRx.FindStringSubmatch(line); m != nil {
			delete(started, m[1])
			s.lintSpecs++
		}
	}
	for spec := range started {
		s.inFlight = append(s.inFlight, spec)
	}
	sort.Strings(s.inFlight)
	return s
}

func toMs(v, unit string) float64 {
	f, _ := strconv.ParseFloat(v, 64)
	if unit == "s" {
		return f * 1000
	}
	return f
}

type aggregate struct {
	okSpecs    int
	failSpecs  int
	lintSpecs  int
	routes     int
	failures   int
	bootMs     float64
	testMs     float64
	failed     []string
	seenFailed map[string]struct{}
}

func (a *aggregate) merge(b batchSummary) {
	a.okSpecs += b.okSpecs
	a.failSpecs += b.failSpecs
	a.lintSpecs += b.lintSpecs
	a.routes += b.routes
	a.failures += b.failures
	a.bootMs += b.bootMs
	a.testMs += b.testMs
	if a.seenFailed == nil {
		a.seenFailed = map[string]struct{}{}
	}
	for _, spec := range b.failed {
		if _, ok := a.seenFailed[spec]; ok {
			continue
		}
		a.seenFailed[spec] = struct{}{}
		a.failed = append(a.failed, spec)
	}
}

func (a *aggregate) print(totalBatches, failedBatches int, wall time.Duration) {
	total := a.okSpecs + a.failSpecs
	inProcessMs := a.bootMs + a.testMs
	overhead := wall - time.Duration(inProcessMs)*time.Millisecond
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "=== Portable Integration Results (all batches) ===")
	fmt.Fprintf(os.Stderr, "Total specs:        %d (ok: %d, failed: %d, lint-skipped: %d)\n",
		total+a.lintSpecs, a.okSpecs, a.failSpecs, a.lintSpecs)
	fmt.Fprintf(os.Stderr, "Total operations:   %d (failures: %d)\n", a.routes, a.failures)
	fmt.Fprintf(os.Stderr, "Boot time:          %s\n", fmtMs(a.bootMs))
	fmt.Fprintf(os.Stderr, "Test time:          %s\n", fmtMs(a.testMs))
	fmt.Fprintf(os.Stderr, "In-process total:   %s\n", fmtMs(inProcessMs))
	fmt.Fprintf(os.Stderr, "Batch overhead:     %s (%s/batch, mostly go-test startup)\n",
		fmtDur(overhead), fmtDur(overhead/time.Duration(max(totalBatches, 1))))
	fmt.Fprintf(os.Stderr, "Wall clock:         %s\n", fmtDur(wall))
	if total > 0 {
		fmt.Fprintf(os.Stderr, "Avg per spec:       %s in-process / %s wall\n",
			fmtMs(inProcessMs/float64(total)), fmtDur(wall/time.Duration(total)))
	}
	fmt.Fprintf(os.Stderr, "Batches:            %d (failing: %d)\n", totalBatches, failedBatches)
	fmt.Fprintln(os.Stderr, strings.Repeat("=", 50))

	if len(a.failed) > 0 {
		sorted := append([]string(nil), a.failed...)
		sort.Strings(sorted)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "=== Failed specs (%d) ===\n", len(sorted))
		for _, spec := range sorted {
			fmt.Fprintf(os.Stderr, "  %s\n", spec)
		}
	}
}

func fmtMs(ms float64) string {
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	m := int(secs / 60)
	return fmt.Sprintf("%dm%.0fs", m, secs-float64(m*60))
}

func fmtDur(d time.Duration) string {
	return fmtMs(float64(d.Milliseconds()))
}
