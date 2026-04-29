package integrationtest

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"go.yaml.in/yaml/v4"
)

//go:embed templates/service-config.yml
var serviceConfigTemplate []byte

//go:embed templates/codegen.yml
var codegenConfigTemplate []byte

// Note: specsFS will be initialized from the root integration_test.go
// We can't embed from internal/ package due to Go embed restrictions
var specsFS embed.FS

// SetSpecsFS sets the embedded filesystem (called from root test)
func SetSpecsFS(fs embed.FS) {
	specsFS = fs
}

// writeCodegenConfigTemplate writes the embedded codegen template to a temp file
// and returns the path. Returns empty string on error.
func writeCodegenConfigTemplate() string {
	f, err := os.CreateTemp("", "codegen-*.yml")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(codegenConfigTemplate); err != nil {
		return ""
	}
	return f.Name()
}

// readSpecFile reads a spec file with default base directory (for internal use)
func readSpecFile(specPath string) ([]byte, error) {
	return ReadSpecFileWithBaseDir(specPath, "testdata/specs")
}

// ReadSpecFileWithBaseDir reads a spec file from various locations with a custom base directory
// Exported so it can be used from the root integration test
func ReadSpecFileWithBaseDir(specPath, baseDir string) ([]byte, error) {
	// Try as absolute or relative file path first
	content, err := os.ReadFile(specPath)
	if err == nil {
		return content, nil
	}

	// Try with base dir prefix
	baseDirPath := filepath.Join(baseDir, specPath)
	content, err = os.ReadFile(baseDirPath)
	if err == nil {
		return content, nil
	}

	// Fall back to embedded filesystem (relative to specsFS root which is baseDir)
	content, err = fs.ReadFile(specsFS, filepath.Join(baseDir, specPath))
	if err != nil {
		// Try without prefix in embedded FS
		content, err = fs.ReadFile(specsFS, specPath)
		if err != nil {
			return nil, fmt.Errorf("spec file not found: %s\nTried:\n  1. As file path: %s\n  2. With %s prefix: %s\n  3. In embedded specs",
				specPath, specPath, baseDir, baseDirPath)
		}
	}

	return content, nil
}

// CollectSpecs collects all spec files to process based on provided paths
func CollectSpecs(t *testing.T, specPaths []string) []string {
	var specs []string

	if len(specPaths) > 0 {
		// Process each provided path (can be file or directory)
		for _, specPath := range specPaths {
			collected := collectSpecsFromPath(t, specPath)
			specs = append(specs, collected...)
		}
		return specs
	}

	// No paths provided - walk through all testdata/specs
	err := fs.WalkDir(specsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		fileName := d.Name()
		if fileName[0] == '-' || strings.Contains(path, "/stash/") {
			return nil
		}

		if strings.HasSuffix(fileName, ".yml") || strings.HasSuffix(fileName, ".yaml") || strings.HasSuffix(fileName, ".json") {
			specs = append(specs, path)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to walk specs directory: %v", err)
	}

	return specs
}

// FilterSpecsBySize filters out specs larger than maxBytes.
// Returns filtered specs and count of excluded specs.
func FilterSpecsBySize(specs []string, maxBytes int64) ([]string, int) {
	if maxBytes <= 0 {
		return specs, 0
	}

	var filtered []string
	excluded := 0

	for _, spec := range specs {
		size, err := getSpecFileSize(spec)
		if err != nil {
			// Can't determine size, include it
			filtered = append(filtered, spec)
			continue
		}
		if size <= maxBytes {
			filtered = append(filtered, spec)
		} else {
			excluded++
		}
	}

	return filtered, excluded
}

// getSpecFileSize returns the size of a spec file in bytes
func getSpecFileSize(specPath string) (int64, error) {
	// Try as absolute/relative path first
	if info, err := os.Stat(specPath); err == nil {
		return info.Size(), nil
	}

	// Try in testdata/specs
	testdataPath := filepath.Join("testdata", "specs", specPath)
	if info, err := os.Stat(testdataPath); err == nil {
		return info.Size(), nil
	}

	// Try embedded FS
	if f, err := specsFS.Open(specPath); err == nil {
		defer func() { _ = f.Close() }()
		if info, err := f.Stat(); err == nil {
			return info.Size(), nil
		}
	}

	return 0, fmt.Errorf("cannot determine size of %s", specPath)
}

// collectSpecsFromPath collects specs from a single path (file or directory)
func collectSpecsFromPath(t *testing.T, specPath string) []string {
	var specs []string

	// Try as file first
	if _, err := readSpecFile(specPath); err == nil {
		return []string{specPath}
	}

	// Try as absolute directory
	if info, err := os.Stat(specPath); err == nil && info.IsDir() {
		// Walk the directory
		err := filepath.Walk(specPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			fileName := info.Name()
			if fileName[0] == '-' || strings.Contains(path, "/stash/") {
				return nil
			}

			if strings.HasSuffix(fileName, ".yml") || strings.HasSuffix(fileName, ".yaml") || strings.HasSuffix(fileName, ".json") {
				// Use absolute path for absolute input, otherwise use as-is
				specs = append(specs, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Failed to walk directory %s: %v", specPath, err)
		}
		return specs
	}

	// Try as directory in testdata/specs
	testdataPath := filepath.Join("testdata", "specs", specPath)
	if info, err := os.Stat(testdataPath); err == nil && info.IsDir() {
		// Walk the directory
		err := filepath.Walk(testdataPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			fileName := info.Name()
			if fileName[0] == '-' || strings.Contains(path, "/stash/") {
				return nil
			}

			if strings.HasSuffix(fileName, ".yml") || strings.HasSuffix(fileName, ".yaml") || strings.HasSuffix(fileName, ".json") {
				// Make path relative to testdata/specs
				relPath, _ := filepath.Rel(filepath.Join("testdata", "specs"), path)
				specs = append(specs, relPath)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Failed to walk directory %s: %v", testdataPath, err)
		}
		return specs
	}

	// Try as directory in embedded filesystem
	embedPath := filepath.Join("testdata", "specs", specPath)
	err := fs.WalkDir(specsFS, embedPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		fileName := d.Name()
		if fileName[0] == '-' || strings.Contains(path, "/stash/") {
			return nil
		}

		if strings.HasSuffix(fileName, ".yml") || strings.HasSuffix(fileName, ".yaml") || strings.HasSuffix(fileName, ".json") {
			// Make path relative to testdata/specs
			relPath := strings.TrimPrefix(path, "testdata/specs/")
			specs = append(specs, relPath)
		}
		return nil
	})

	if err == nil && len(specs) > 0 {
		return specs
	}

	t.Fatalf("Path not found or invalid: %s", specPath)
	return nil
}

// updateServiceConfig updates the service config for integration tests.
// It merges the embedded template config with the service's generated config,
// and enables simplification for large specs.
func updateServiceConfig(configPath string, specSize, simplifyThreshold int64) error {
	// Parse embedded template
	templateCfg, err := config.NewServiceConfigFromBytes(serviceConfigTemplate)
	if err != nil {
		return fmt.Errorf("parsing template config: %w", err)
	}

	// Read existing service config
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	cfg, err := config.NewServiceConfigFromBytes(configData)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Merge template onto existing config (template values take precedence)
	cfg.OverwriteWith(templateCfg)

	// Enable simplification for large specs (dynamic, not in template)
	if simplifyThreshold > 0 && specSize >= simplifyThreshold {
		if cfg.SpecOptions != nil {
			cfg.SpecOptions.Simplify = true
		}
	}

	// Write updated config
	updatedData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, updatedData, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}
