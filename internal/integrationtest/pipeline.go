package integrationtest

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
)

const (
	// MaxErrorMessageLength is the maximum length of error messages in results
	MaxErrorMessageLength = 2000
)

// PreparedBatch represents a batch that's ready to test
type PreparedBatch struct {
	Info        *BatchServerInfo
	ServerProc  *ServerProcess
	ServerURL   string
	Port        int
	Specs       []string      // Original spec paths for this batch
	Error       error         // Error during preparation (only set if entire batch failed)
	FailedSpecs []FailedSpec  // Services that failed during setup/generate (batch continues with rest)
	BuildTime   time.Duration // Time spent building this batch
	BatchID     int
}

// FailedSpec represents a spec that failed during batch preparation
type FailedSpec struct {
	Spec  string
	Error error
}

// PipelineConfig holds configuration for the pipeline
type PipelineConfig struct {
	SandboxDir   string
	RuntimeOpts  *RuntimeOptions
	BasePort     int
	MaxFails     int
	TotalSpecs   int
	TotalBatches int
	OnResult     func(result IntegrationResult, stats *ServiceStats, completed, total int)
	OnBatchStart func(batchID, totalBatches, servicesInBatch int, batchSizeBytes int64)
	OnBatchDone  func(batchID, totalBatches, servicesInBatch int, buildTime, testTime time.Duration)
	// OnBatchPhase is called when a batch phase starts/ends (phase: "setup", "build")
	OnBatchPhase func(batchID int, phase string, done bool, elapsed time.Duration)
	// InterruptCh is an optional channel that signals when to abort and report partial results
	InterruptCh <-chan struct{}
	// Cache is an optional result cache to skip previously passing specs
	Cache *ResultCache
	// BatchPrepTimeout is the maximum time allowed for batch preparation (setup + build + server start)
	// Default: 5 minutes
	BatchPrepTimeout time.Duration
}

// BatchStats holds timing statistics for batches
type BatchStats struct {
	TotalBatches int
	BuildTimes   []time.Duration
	TestTimes    []time.Duration
}

// AvgBuildTime returns average batch build time
func (b *BatchStats) AvgBuildTime() time.Duration {
	if len(b.BuildTimes) == 0 {
		return 0
	}
	var total time.Duration
	for _, t := range b.BuildTimes {
		total += t
	}
	return total / time.Duration(len(b.BuildTimes))
}

// AvgTestTime returns average batch test time
func (b *BatchStats) AvgTestTime() time.Duration {
	if len(b.TestTimes) == 0 {
		return 0
	}
	var total time.Duration
	for _, t := range b.TestTimes {
		total += t
	}
	return total / time.Duration(len(b.TestTimes))
}

// RunPipeline runs batches with parallel builds:
// Multiple batches can be built simultaneously, then tested as they complete
// Returns results, service stats, total build time, and batch stats
func RunPipeline(batches [][]string, cfg *PipelineConfig) ([]IntegrationResult, map[string]*ServiceStats, time.Duration, *BatchStats) {
	var allResults []IntegrationResult
	serviceStatsMap := make(map[string]*ServiceStats)
	var resultsMu sync.Mutex

	var failCount atomic.Int32
	var completed atomic.Int32
	var aborted atomic.Bool

	batchStats := &BatchStats{TotalBatches: len(batches)}

	buildPhaseStart := time.Now()

	// Helper to check if interrupted (external signal or max failures reached)
	isInterrupted := func() bool {
		if aborted.Load() {
			return true
		}
		if cfg.InterruptCh == nil {
			return false
		}
		select {
		case <-cfg.InterruptCh:
			return true
		default:
			return false
		}
	}

	// Channel for prepared batches ready to test (buffered for parallel builds)
	batchConcurrency := cfg.RuntimeOpts.BatchConcurrency
	preparedCh := make(chan *PreparedBatch, batchConcurrency)

	// Semaphore to limit concurrent batch builds
	buildSemaphore := make(chan struct{}, batchConcurrency)

	// Start preparation goroutines - one per batch, limited by semaphore
	var buildWg sync.WaitGroup
	go func() {
		for batchID, specs := range batches {
			if aborted.Load() || isInterrupted() {
				aborted.Store(true)
				break
			}

			buildSemaphore <- struct{}{} // Acquire
			buildWg.Add(1)

			if cfg.OnBatchStart != nil {
				var batchSize int64
				for _, spec := range specs {
					if size, err := getSpecFileSize(spec); err == nil {
						batchSize += size
					}
				}
				cfg.OnBatchStart(batchID+1, cfg.TotalBatches, len(specs), batchSize)
			}

			go func(batchID int, specs []string) {
				defer buildWg.Done()
				defer func() { <-buildSemaphore }() // Release

				// Recover from panics in batch preparation
				defer func() {
					if r := recover(); r != nil {
						prepared := &PreparedBatch{
							Specs:   specs,
							Port:    cfg.BasePort + batchID,
							BatchID: batchID + 1,
							Error:   fmt.Errorf("panic during batch preparation: %v", r),
						}
						preparedCh <- prepared
					}
				}()

				if aborted.Load() || isInterrupted() {
					aborted.Store(true)
					// Still send a prepared batch so the main loop can report skipped services
					prepared := &PreparedBatch{
						Specs:   specs,
						Port:    cfg.BasePort + batchID,
						BatchID: batchID + 1,
						Error:   fmt.Errorf("skipped: test run aborted"),
					}
					preparedCh <- prepared
					return
				}

				buildStart := time.Now()

				// Apply timeout for batch preparation
				timeout := cfg.BatchPrepTimeout
				if timeout == 0 {
					timeout = 5 * time.Minute // Default 5 minutes
				}

				// Run prepareBatch with timeout
				type prepResult struct {
					prepared *PreparedBatch
				}
				resultCh := make(chan prepResult, 1)
				go func() {
					p := prepareBatch(batchID, specs, cfg, isInterrupted)
					resultCh <- prepResult{prepared: p}
				}()

				var prepared *PreparedBatch
				select {
				case res := <-resultCh:
					prepared = res.prepared
				case <-time.After(timeout):
					prepared = &PreparedBatch{
						Specs:   specs,
						Port:    cfg.BasePort + batchID,
						BatchID: batchID + 1,
						Error:   fmt.Errorf("batch preparation timed out after %v", timeout),
					}
				}

				prepared.BuildTime = time.Since(buildStart)
				prepared.BatchID = batchID + 1
				preparedCh <- prepared
			}(batchID, specs)
		}

		// Wait for all builds to complete, then close channel
		buildWg.Wait()
		close(preparedCh)
	}()

	// Process prepared batches with interrupt support
	processBatches := func() {
		for {
			// Use select to allow interruption while waiting for batches
			var prepared *PreparedBatch
			var ok bool

			if cfg.InterruptCh != nil {
				select {
				case <-cfg.InterruptCh:
					aborted.Store(true)
					// Return immediately - don't wait for builds to complete
					return
				case prepared, ok = <-preparedCh:
					if !ok {
						return
					}
				}
			} else {
				prepared, ok = <-preparedCh
				if !ok {
					return
				}
			}

			batchTestStart := time.Now()

			if aborted.Load() {
				// Clean up if we're aborting
				if prepared.ServerProc != nil {
					StopServiceServer(prepared.ServerProc)
				}
				continue
			}

			if prepared.Error != nil {
				// Entire batch failed - record errors for all specs
				log.Printf("❌ Batch %d failed: %s\n", prepared.BatchID, prepared.Error)
				errMsg := truncateError(prepared.Error.Error())
				for _, spec := range prepared.Specs {
					serviceName := api.NormalizeServiceName(spec)
					result := IntegrationResult{
						Spec:        spec,
						Ok:          false,
						GenerateErr: errMsg,
						BatchID:     prepared.BatchID,
					}
					if int(failCount.Add(1)) >= cfg.MaxFails {
						aborted.Store(true)
						log.Printf("⚠️  Max failures (%d) reached!\n", cfg.MaxFails)
					}

					resultsMu.Lock()
					allResults = append(allResults, result)
					serviceStatsMap[serviceName] = &ServiceStats{Name: serviceName, Fails: 1}
					resultsMu.Unlock()

					done := int(completed.Add(1))
					if cfg.OnResult != nil {
						cfg.OnResult(result, serviceStatsMap[serviceName], done, cfg.TotalSpecs)
					}
				}
				testTime := time.Since(batchTestStart)
				resultsMu.Lock()
				batchStats.BuildTimes = append(batchStats.BuildTimes, prepared.BuildTime)
				batchStats.TestTimes = append(batchStats.TestTimes, testTime)
				resultsMu.Unlock()
				if cfg.OnBatchDone != nil {
					cfg.OnBatchDone(prepared.BatchID, cfg.TotalBatches, len(prepared.Specs), prepared.BuildTime, testTime)
				}
				continue
			}

			// Report individual service failures (batch continues with successful services)
			for _, fs := range prepared.FailedSpecs {
				serviceName := api.NormalizeServiceName(fs.Spec)
				result := IntegrationResult{
					Spec:        fs.Spec,
					Ok:          false,
					GenerateErr: truncateError(fs.Error.Error()),
					BatchID:     prepared.BatchID,
				}
				if int(failCount.Add(1)) >= cfg.MaxFails {
					aborted.Store(true)
					log.Printf("⚠️  Max failures (%d) reached!\n", cfg.MaxFails)
				}

				resultsMu.Lock()
				allResults = append(allResults, result)
				serviceStatsMap[serviceName] = &ServiceStats{Name: serviceName, Fails: 1}
				resultsMu.Unlock()

				done := int(completed.Add(1))
				if cfg.OnResult != nil {
					cfg.OnResult(result, serviceStatsMap[serviceName], done, cfg.TotalSpecs)
				}
			}

			// Build set of failed specs to skip during testing
			failedSpecSet := make(map[string]bool)
			for _, fs := range prepared.FailedSpecs {
				failedSpecSet[fs.Spec] = true
			}

			// Test all successful services in this batch
			for _, spec := range prepared.Specs {
				if aborted.Load() || isInterrupted() {
					aborted.Store(true)
					break
				}

				// Skip specs that failed during preparation (already reported above)
				if failedSpecSet[spec] {
					continue
				}

				serviceName := api.NormalizeServiceName(spec)
				log.Printf("  ⏳ %s\n", serviceName)
				startTime := time.Now()

				// Call TestService with panic recovery
				var results []IntegrationResult
				var totalEndpoints, testedEndpoints int
				func() {
					defer func() {
						if r := recover(); r != nil {
							results = []IntegrationResult{{
								Spec:        spec,
								Ok:          false,
								GenerateErr: truncateError(fmt.Sprintf("panic during testing: %v", r)),
								BatchID:     prepared.BatchID,
							}}
						}
					}()
					results, totalEndpoints, testedEndpoints = TestService(spec, prepared.ServerURL)
				}()

				// Set BatchID on all results
				for i := range results {
					results[i].BatchID = prepared.BatchID
				}

				var success, fails int
				for _, res := range results {
					if res.Ok {
						success++
					} else {
						fails++
						if int(failCount.Add(1)) >= cfg.MaxFails {
							aborted.Store(true)
							log.Printf("⚠️  Max failures (%d) reached!\n", cfg.MaxFails)
						}
					}
				}

				stats := &ServiceStats{
					Name:      serviceName,
					Endpoints: totalEndpoints,
					Tested:    testedEndpoints,
					Success:   success,
					Fails:     fails,
					LOC:       CountServiceLOC(serviceName, cfg.SandboxDir),
					TTE:       time.Since(startTime),
				}

				// Update cache immediately (survives timeout/interrupt)
				if cfg.Cache != nil {
					if fails == 0 {
						cfg.Cache.MarkPassed(spec)
					} else {
						cfg.Cache.MarkFailed(spec)
					}
					if err := cfg.Cache.Save(); err != nil {
						log.Printf("⚠️  Failed to save cache after %s: %v\n", serviceName, err)
					}
				}

				resultsMu.Lock()
				allResults = append(allResults, results...)
				serviceStatsMap[serviceName] = stats
				resultsMu.Unlock()

				done := int(completed.Add(1))
				if cfg.OnResult != nil {
					var firstResult IntegrationResult
					if len(results) > 0 {
						firstResult = results[0]
					} else {
						firstResult = IntegrationResult{Spec: spec, Ok: true, BatchID: prepared.BatchID}
					}
					cfg.OnResult(firstResult, stats, done, cfg.TotalSpecs)
				}
			}

			// Stop this batch's server
			StopServiceServer(prepared.ServerProc)

			testTime := time.Since(batchTestStart)
			resultsMu.Lock()
			batchStats.BuildTimes = append(batchStats.BuildTimes, prepared.BuildTime)
			batchStats.TestTimes = append(batchStats.TestTimes, testTime)
			resultsMu.Unlock()
			if cfg.OnBatchDone != nil {
				cfg.OnBatchDone(prepared.BatchID, cfg.TotalBatches, len(prepared.Specs), prepared.BuildTime, testTime)
			}
		}
	}

	processBatches()

	return allResults, serviceStatsMap, time.Since(buildPhaseStart), batchStats
}

// truncateError truncates an error message to MaxErrorMessageLength
func truncateError(err string) string {
	if len(err) <= MaxErrorMessageLength {
		return err
	}
	// Try to find a good break point (newline or space)
	truncated := err[:MaxErrorMessageLength]
	if idx := strings.LastIndex(truncated, "\n"); idx > MaxErrorMessageLength/2 {
		truncated = truncated[:idx]
	}
	return truncated + "... (truncated)"
}

// prepareBatch prepares a single batch: setup, generate, build, start
// isInterrupted is a function that returns true if the test run has been interrupted
func prepareBatch(batchID int, specs []string, cfg *PipelineConfig, isInterrupted func() bool) *PreparedBatch {
	prepared := &PreparedBatch{
		Specs: specs,
		Port:  cfg.BasePort + batchID,
	}

	// Helper to report phase start/end
	reportPhase := func(phase string, done bool, elapsed time.Duration) {
		if cfg.OnBatchPhase != nil {
			cfg.OnBatchPhase(batchID+1, phase, done, elapsed)
		}
	}

	// Check for interruption before starting
	if isInterrupted() {
		prepared.Error = fmt.Errorf("interrupted before batch preparation")
		return prepared
	}

	// Setup and generate all services in this batch concurrently
	// Individual service failures are collected but don't stop the batch
	setupStart := time.Now()
	reportPhase("setup", false, 0)

	maxConcurrency := cfg.RuntimeOpts.MaxConcurrency
	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	serviceNames := make([]string, 0, len(specs))
	var failedSpecs []FailedSpec
	var interrupted bool

	for _, spec := range specs {
		// Check for interruption before starting each service
		if isInterrupted() {
			mu.Lock()
			interrupted = true
			mu.Unlock()
			break
		}

		semaphore <- struct{}{}
		wg.Add(1)

		go func(spec string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// Skip if interrupted
			if isInterrupted() {
				return
			}

			serviceName := api.NormalizeServiceName(spec)

			// Setup
			if !IsServiceSetup(serviceName, cfg.SandboxDir) {
				if isInterrupted() {
					return
				}
				if _, err := SetupService(spec, cfg.SandboxDir, cfg.RuntimeOpts); err != nil {
					mu.Lock()
					failedSpecs = append(failedSpecs, FailedSpec{Spec: spec, Error: fmt.Errorf("setup %s: %w", serviceName, err)})
					mu.Unlock()
					return
				}
			}

			// Generate
			if isInterrupted() {
				return
			}
			if err := RunGoGenerate(cfg.SandboxDir, serviceName, cfg.RuntimeOpts.ServiceGenerateTimeout); err != nil {
				mu.Lock()
				failedSpecs = append(failedSpecs, FailedSpec{Spec: spec, Error: fmt.Errorf("generate %s: %w", serviceName, err)})
				mu.Unlock()
				return
			}

			mu.Lock()
			serviceNames = append(serviceNames, serviceName)
			mu.Unlock()
		}(spec)
	}

	wg.Wait()
	reportPhase("setup", true, time.Since(setupStart))

	// Store failed specs for reporting
	prepared.FailedSpecs = failedSpecs

	if interrupted {
		prepared.Error = fmt.Errorf("interrupted during batch preparation")
		return prepared
	}

	// If ALL services failed, mark the batch as failed
	if len(serviceNames) == 0 {
		prepared.Error = fmt.Errorf("all %d services failed during preparation", len(specs))
		return prepared
	}

	// Check for interruption before building
	if isInterrupted() {
		prepared.Error = fmt.Errorf("interrupted before build")
		return prepared
	}

	// Generate and build batch server
	buildStart := time.Now()
	reportPhase("build", false, 0)

	info, err := GenerateBatchServer(cfg.SandboxDir, batchID, serviceNames)
	if err != nil {
		prepared.Error = fmt.Errorf("generate batch server: %w", err)
		return prepared
	}
	prepared.Info = info

	if err := BuildBatchServer(cfg.SandboxDir, info); err != nil {
		prepared.Error = fmt.Errorf("build batch server: %w", err)
		return prepared
	}
	reportPhase("build", true, time.Since(buildStart))

	// Check for interruption before starting server
	if isInterrupted() {
		prepared.Error = fmt.Errorf("interrupted before server start")
		return prepared
	}

	// Start server
	proc, err := StartServiceServer(info.ServerBin, cfg.SandboxDir, prepared.Port)
	if err != nil {
		prepared.Error = fmt.Errorf("start batch server: %w", err)
		return prepared
	}
	prepared.ServerProc = proc
	prepared.ServerURL = fmt.Sprintf("http://localhost:%d", prepared.Port)

	// Wait for server (pass proc to detect early exit/panic)
	if err := WaitForServer(prepared.ServerURL, ServerReadyTimeout, proc, isInterrupted); err != nil {
		stderr := proc.GetStderr()
		StopServiceServer(proc)
		if stderr != "" {
			prepared.Error = fmt.Errorf("server not ready: %w\nstderr: %s", err, truncateError(stderr))
		} else {
			prepared.Error = fmt.Errorf("server not ready: %w", err)
		}
		return prepared
	}

	return prepared
}
