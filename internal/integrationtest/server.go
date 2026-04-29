package integrationtest

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mockzilla/mockzilla/v2/internal/files"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

const (
	ServerPort         = 18123
	ServerReadyTimeout = 90 * time.Second // 90s for large batches on slower machines

	// GenServiceBinaryPath is the relative path to the pre-built gen-service binary within sandbox
	GenServiceBinaryPath = ".build/bin/gen-service"
)

var (
	// HTTPClient is shared client for all requests.
	// 10s timeout to handle complex schemas that take longer to generate.
	HTTPClient = &http.Client{
		Timeout: 30 * time.Second,
	}
	HealthEndpoint = "/healthz"
)

// SetupSandbox copies necessary files to the sandbox directory
func SetupSandbox(sandboxDir string) error {
	// Get current working directory (project root)
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Directories to copy
	dirsToCopy := []string{"cmd", "pkg", "internal", "resources"}
	for _, dir := range dirsToCopy {
		src := filepath.Join(cwd, dir)
		dst := filepath.Join(sandboxDir, dir)
		if err := files.CopyDirectory(src, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", dir, err)
		}
	}

	// Files to copy
	filesToCopy := []string{"go.mod", "go.sum"}
	for _, file := range filesToCopy {
		src := filepath.Join(cwd, file)
		dst := filepath.Join(sandboxDir, file)
		if err := files.CopyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	// Build gen-service binary once (avoids recompiling for each service generation)
	if err := buildGenService(sandboxDir); err != nil {
		return fmt.Errorf("failed to build gen-service: %w", err)
	}

	// Clean up resources/data directory to ensure clean state
	dataDir := filepath.Join(sandboxDir, "resources", "data")
	if err := os.RemoveAll(dataDir); err != nil {
		return fmt.Errorf("failed to clean data directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to recreate data directory: %w", err)
	}

	// Add replace directive to go.mod so Go uses local code instead of fetching from internet
	// This is needed because generated handlers import packages like:
	// github.com/mockzilla/mockzilla/v2/resources/data/services/stripe_spec3/types
	// Without the replace directive, Go tries to fetch this from the internet and fails
	goModPath := filepath.Join(sandboxDir, "go.mod")
	goModContent, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("failed to read go.mod: %w", err)
	}

	// Check if replace directive already exists
	goModStr := string(goModContent)
	if !strings.Contains(goModStr, "replace github.com/mockzilla/mockzilla/v2") {
		// Add replace directive with absolute path to sandbox directory
		// sandboxDir is already an absolute path from CreateSandbox()
		goModStr += fmt.Sprintf("\n// Use local code for sandbox testing\nreplace github.com/mockzilla/mockzilla/v2 => %s\n", sandboxDir)
		if err := os.WriteFile(goModPath, []byte(goModStr), 0644); err != nil {
			return fmt.Errorf("failed to write go.mod: %w", err)
		}
	}

	return nil
}

// WaitForServer waits for the server to be ready.
// If proc is provided, it will fail fast if the process exits.
func WaitForServer(serverURL string, maxWait time.Duration, proc *ServerProcess, isInterrupted ...func() bool) error {
	deadline := time.Now().Add(maxWait)

	// Channel to detect process exit
	procExited := make(chan error, 1)
	if proc != nil && proc.Cmd != nil {
		go func() {
			err := proc.Cmd.Wait()
			proc.MarkWaited()
			procExited <- err
		}()
	}

	for time.Now().Before(deadline) {
		// Check for interruption
		if len(isInterrupted) > 0 && isInterrupted[0] != nil && isInterrupted[0]() {
			return fmt.Errorf("interrupted while waiting for server")
		}

		// Check if process exited (non-blocking)
		select {
		case err := <-procExited:
			if err != nil {
				return fmt.Errorf("server process exited: %w", err)
			}
			return fmt.Errorf("server process exited unexpectedly")
		default:
			// Process still running, continue
		}

		resp, err := HTTPClient.Get(serverURL + HealthEndpoint)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("server did not become ready within %v", maxWait)
}

// prefixedWriter wraps output with a prefix for debugging
type prefixedWriter struct {
	prefix   string
	isStderr bool
}

func (w *prefixedWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for i, line := range lines {
		if line != "" || i < len(lines)-1 {
			prefixed := fmt.Sprintf("%s %s\n", w.prefix, line)
			if w.isStderr {
				fmt.Fprint(os.Stderr, prefixed)
			} else {
				fmt.Print(prefixed)
			}
		}
	}
	return len(p), nil
}

// buildGenService builds the gen-service binary once for use in all service generations.
func buildGenService(sandboxDir string) error {
	genServiceBin := filepath.Join(sandboxDir, GenServiceBinaryPath)
	binDir := filepath.Dir(genServiceBin)

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	cmd := exec.Command("go", "build", "-o", genServiceBin, "./cmd/gen/service")
	cmd.Dir = sandboxDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w\nstderr: %s", err, stderr.String())
	}

	return nil
}

// BuildServiceServer builds the server binary for a single service
func BuildServiceServer(sandboxDir, serviceName string) (string, error) {
	paths := config.NewPaths(sandboxDir)
	serverDir := filepath.Join(paths.Services, serviceName, "server")
	serverBin := filepath.Join(sandboxDir, fmt.Sprintf("server-%s", serviceName))

	cmd := exec.Command("go", "build", "-o", serverBin, ".")
	cmd.Dir = serverDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build failed: %w\nstderr: %s", err, stderr.String())
	}

	return serverBin, nil
}

// StartServiceServer starts a service's server on the given port
// ServerProcess wraps exec.Cmd with captured output
type ServerProcess struct {
	Cmd    *exec.Cmd
	Stdout *bytes.Buffer
	Stderr *bytes.Buffer
	waited atomic.Bool // true if Wait() was already called
}

// GetStderr returns captured stderr (useful for debugging failed servers)
func (s *ServerProcess) GetStderr() string {
	if s.Stderr == nil {
		return ""
	}
	return s.Stderr.String()
}

// MarkWaited marks the process as already waited on
func (s *ServerProcess) MarkWaited() {
	s.waited.Store(true)
}

// WasWaited returns true if Wait() was already called
func (s *ServerProcess) WasWaited() bool {
	return s.waited.Load()
}

func StartServiceServer(serverBin, sandboxDir string, port int) (*ServerProcess, error) {
	cmd := exec.Command(serverBin)
	cmd.Dir = sandboxDir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", port),
		"LOG_LEVEL=warn", // Suppress INFO logs during tests
	)

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start: %w", err)
	}

	return &ServerProcess{Cmd: cmd, Stdout: &stdout, Stderr: &stderr}, nil
}

// StopServiceServer stops a service's server
func StopServiceServer(proc *ServerProcess) {
	if proc == nil || proc.Cmd == nil || proc.Cmd.Process == nil {
		return
	}
	_ = proc.Cmd.Process.Kill()
	// Wait with timeout - the WaitForServer goroutine may also be waiting
	// Use a goroutine to avoid blocking forever
	done := make(chan struct{})
	go func() {
		_ = proc.Cmd.Wait()
		proc.MarkWaited()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// Process didn't exit cleanly, but we killed it so move on
	}
}
