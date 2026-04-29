package portable

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
)

// flags holds the parsed CLI flags for portable mode.
type flags struct {
	port    int
	config  string // unified app+services config
	context string // per-service contexts
}

// IsPortableMode determines if the CLI args indicate portable mode.
// Returns true if any positional arg is a spec file, a URL, or a directory containing spec files.
func IsPortableMode(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isURL(arg) || isSpecFile(arg) {
			return true
		}
		info, err := os.Stat(arg)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if hasStaticDir(arg) {
				return true
			}
			entries, _ := os.ReadDir(arg)
			for _, e := range entries {
				if !e.IsDir() && isSpecFile(e.Name()) {
					return true
				}
			}
		}
	}
	return false
}

// isSpecFile checks if a filename is an OpenAPI spec file.
func isSpecFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".json")
}

// resolveSpecs examines the positional args and returns spec file paths.
// URL arguments are downloaded to a temp directory and resolved to local paths.
func resolveSpecs(args []string) []string {
	var specs []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isURL(arg) {
			path, err := downloadSpec(arg)
			if err != nil {
				slog.Error("Failed to download spec", "url", arg, "error", err)
				continue
			}
			specs = append(specs, path)
			continue
		}
		info, err := os.Stat(arg)
		if err != nil {
			if isSpecFile(arg) {
				slog.Error("Spec file not found", "path", arg)
			}
			continue
		}
		if info.IsDir() {
			specs = append(specs, resolveStaticSpecs(arg)...)
			entries, err := os.ReadDir(arg)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() && isSpecFile(e.Name()) {
					specs = append(specs, filepath.Join(arg, e.Name()))
				}
			}
		} else if isSpecFile(arg) {
			specs = append(specs, arg)
		}
	}
	return specs
}

// parseFlags parses CLI flags for portable mode, returning flags and remaining positional args.
func parseFlags(args []string) (flags, []string) {
	fs := flag.NewFlagSet("portable", flag.ContinueOnError)
	fl := flags{}
	fs.IntVar(&fl.port, "port", 0, "Server port (default: from app config or 2200)")
	fs.StringVar(&fl.config, "config", "", "Unified config YAML (app settings + per-service config)")
	fs.StringVar(&fl.context, "context", "", "Per-service context YAML for value replacements")

	// Separate positional args from flags
	var positional []string
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flagArgs = append(flagArgs, args[i])
			// If this flag takes a value (not a boolean), consume next arg too
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !strings.Contains(args[i], "=") {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		slog.Warn("Failed to parse flags", "error", err)
	}

	return fl, positional
}

// isURL checks if a string is an HTTP or HTTPS URL.
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// downloadSpec downloads a spec from a URL to a temp file and returns the local path.
// The filename is derived from the URL's last path segment.
func downloadSpec(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	// Derive filename from URL path
	name := filepath.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		name = parsed.Host
	}
	if !isSpecFile(name) {
		name += ".yml"
	}

	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("fetching: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	dir := filepath.Join(os.TempDir(), "mockzilla-portable", "specs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("writing spec: %w", err)
	}

	slog.Info("Downloaded spec", "url", rawURL, "path", path)
	return path, nil
}

// hasStaticDir checks if a directory contains a "static" subdirectory with service dirs.
func hasStaticDir(dir string) bool {
	staticDir := filepath.Join(dir, "static")
	info, err := os.Stat(staticDir)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(staticDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}

// resolveStaticSpecs looks for a "static" subdirectory within dir,
// converts each service directory into a temporary OpenAPI spec, and returns the paths.
func resolveStaticSpecs(dir string) []string {
	staticDir := filepath.Join(dir, "static")
	entries, err := os.ReadDir(staticDir)
	if err != nil {
		return nil
	}

	tmpDir := filepath.Join(os.TempDir(), "mockzilla-portable", "specs")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		slog.Error("Failed to create temp dir for static specs", "error", err)
		return nil
	}

	var specs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		serviceName := e.Name()
		serviceDir := filepath.Join(staticDir, serviceName)

		specBytes, err := cmdapi.GenerateSpecFromStaticDir(serviceDir, serviceName)
		if err != nil {
			slog.Error("Failed to generate spec from static dir", "service", serviceName, "error", err)
			continue
		}

		specPath := filepath.Join(tmpDir, serviceName+".yml")
		if err := os.WriteFile(specPath, specBytes, 0o644); err != nil {
			slog.Error("Failed to write generated spec", "path", specPath, "error", err)
			continue
		}

		slog.Info("Generated spec from static files", "service", serviceName)
		specs = append(specs, specPath)
	}
	return specs
}
