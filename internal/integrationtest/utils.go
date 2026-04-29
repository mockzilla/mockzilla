package integrationtest

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

var (
	// debugLogger - enabled via DEBUG=1 or DEBUG=true environment variable
	debugLogger *slog.Logger
)

func init() {
	// Initialize debug logger
	debugLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Default to error level (suppresses debug)
	}))

	if isDebugEnabled() {
		debugLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}
}

// isDebugEnabled checks if DEBUG environment variable is set to enable debug logging
func isDebugEnabled() bool {
	debug := strings.ToLower(os.Getenv("DEBUG"))
	return debug == "1" || debug == "true"
}

// CountServiceLOC counts lines of code for a service's generated code (gen.go)
func CountServiceLOC(serviceName, sandboxDir string) int {
	paths := config.NewPaths(sandboxDir)
	serviceDir := filepath.Join(paths.Services, serviceName)
	return countLOC(serviceDir)
}

// countLOC counts non-empty lines of code in all .go files in a directory
func countLOC(dir string) int {
	var totalLines int

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "//") {
				totalLines++
			}
		}

		return scanner.Err()
	})

	if err != nil {
		debugLogger.Debug("Error counting LOC", "dir", dir, "error", err)
		return 0
	}

	return totalLines
}

// formatLOC formats lines of code with k suffix for thousands
func formatLOC(loc int) string {
	if loc >= 1000 {
		return fmt.Sprintf("%.1fk", float64(loc)/1000)
	}
	return fmt.Sprintf("%d", loc)
}

// ShouldCleanSandbox determines whether the sandbox should be cleaned.
// Returns true if:
// - CLEAN_SANDBOX env var is set (forced clean)
// - There are uncommitted git changes (source code modified)
// - Git command fails (assume dirty to be safe)
func ShouldCleanSandbox() bool {
	if os.Getenv("CLEAN_SANDBOX") != "" {
		return true
	}

	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return true // assume dirty if git fails
	}
	return len(strings.TrimSpace(string(output))) > 0
}
