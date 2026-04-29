package integrationtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

const (
	// ServiceGenerateFile is the name of the file that contains go:generate directive
	ServiceGenerateFile = "generate.go"
)

// IsServiceSetup checks if a service has been set up (has generate.go in service root)
func IsServiceSetup(serviceName, sandboxDir string) bool {
	paths := config.NewPaths(sandboxDir)
	generateFile := filepath.Join(paths.Services, serviceName, ServiceGenerateFile)
	_, err := os.Stat(generateFile)
	return err == nil
}

// SetupService runs gen-service for a single spec (creates setup dir and generates service)
// Returns the service name
func SetupService(specFile, sandboxDir string, opts *RuntimeOptions) (string, error) {
	startTime := time.Now()

	// Determine base directory for specs
	baseDir := "testdata/specs"
	if opts != nil && opts.SpecsBaseDir != "" {
		baseDir = opts.SpecsBaseDir
	}

	// Read spec file
	t1 := time.Now()
	specContent, err := ReadSpecFileWithBaseDir(specFile, baseDir)
	if err != nil {
		return "", err
	}
	debugLogger.Debug("Read spec file", "duration", time.Since(t1))

	// Generate service name from spec file
	serviceName := api.NormalizeServiceName(specFile)

	// Step 1: Write spec to a temp file so we can pass it to gen-service
	// Use service name to make temp file unique (avoid race conditions with concurrent generation)
	t2 := time.Now()
	tempSpecFile := filepath.Join(sandboxDir, fmt.Sprintf("temp-spec-%s.yml", serviceName))
	if err := os.WriteFile(tempSpecFile, specContent, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp spec: %w", err)
	}
	defer func() { _ = os.Remove(tempSpecFile) }()
	debugLogger.Debug("Write temp spec", "duration", time.Since(t2))

	// Step 2: Run gen-service command
	// The service command does both setup and generation in one step
	paths := config.NewPaths(sandboxDir)
	serviceOutputDir := filepath.Join(paths.Services, serviceName)

	// Use pre-built binary (built once in SetupSandbox)
	genServiceBin := filepath.Join(sandboxDir, GenServiceBinaryPath)
	args := []string{genServiceBin, "-name", serviceName, "-output", serviceOutputDir}

	// Add custom codegen config if provided
	if opts != nil && opts.CodegenConfigPath != "" {
		// Convert to absolute path if relative
		configPath := opts.CodegenConfigPath
		if !filepath.IsAbs(configPath) {
			absPath, err := filepath.Abs(configPath)
			if err != nil {
				return "", fmt.Errorf("failed to resolve codegen config path: %w", err)
			}
			configPath = absPath
		}
		args = append(args, "-codegen-config", configPath)
	}

	// Add custom service config if provided
	if opts != nil && opts.ServiceConfigPath != "" {
		// Convert to absolute path if relative
		configPath := opts.ServiceConfigPath
		if !filepath.IsAbs(configPath) {
			absPath, err := filepath.Abs(configPath)
			if err != nil {
				return "", fmt.Errorf("failed to resolve service config path: %w", err)
			}
			configPath = absPath
		}
		args = append(args, "-service-config", configPath)
	}

	// Add spec file as positional argument (must be last)
	args = append(args, tempSpecFile)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = sandboxDir

	var stdout, stderr bytes.Buffer
	if isDebugEnabled() {
		cmd.Stdout = &prefixedWriter{prefix: fmt.Sprintf("[SERVICE:%s]", serviceName), isStderr: false}
		cmd.Stderr = &prefixedWriter{prefix: fmt.Sprintf("[SERVICE:%s]", serviceName), isStderr: true}
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	t3 := time.Now()
	if err := cmd.Run(); err != nil {
		if !isDebugEnabled() {
			return "", fmt.Errorf("gen-service failed: %w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		}
		return "", fmt.Errorf("gen-service failed: %w", err)
	}
	debugLogger.Debug("Run gen-service command", "duration", time.Since(t3))

	// Setup directory is at resources/data/services/<service-name>/setup
	setupDir := filepath.Join(serviceOutputDir, "setup")

	// Step 3: Enable response validation in config.yml (only if not using custom config)
	t4 := time.Now()
	if opts == nil || opts.ServiceConfigPath == "" {
		configPath := filepath.Join(setupDir, "config.yml")
		if err := updateServiceConfig(configPath, int64(len(specContent)), opts.SimplifyThresholdBytes); err != nil {
			return "", fmt.Errorf("failed to enable response validation: %w", err)
		}
	}
	debugLogger.Debug("Update service config", "duration", time.Since(t4))

	debugLogger.Debug("SetupService total", "service", serviceName, "duration", time.Since(startTime))
	return serviceName, nil
}

// RunGoGenerate runs go generate for a service with a timeout.
// This regenerates the service code including server/main.go.
func RunGoGenerate(sandboxDir, serviceName string, timeout time.Duration) error {
	paths := config.NewPaths(sandboxDir)
	generateFile := filepath.Join(paths.Services, serviceName, ServiceGenerateFile)

	debugLogger.Debug("Running go generate for service", "service", serviceName, "file", generateFile)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "generate", generateFile)
	cmd.Dir = sandboxDir

	var stdout, stderr bytes.Buffer
	if isDebugEnabled() {
		cmd.Stdout = &prefixedWriter{prefix: fmt.Sprintf("[SERVICE:%s]", serviceName), isStderr: false}
		cmd.Stderr = &prefixedWriter{prefix: fmt.Sprintf("[SERVICE:%s]", serviceName), isStderr: true}
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("go generate timed out after %s", timeout)
		}
		if !isDebugEnabled() {
			return fmt.Errorf("go generate failed: %w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
		}
		return fmt.Errorf("go generate failed: %w", err)
	}

	return nil
}
