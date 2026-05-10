package portable

import (
	"archive/tar"
	"compress/gzip"
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
	port       int
	config     string // unified app+services config
	context    string // per-service contexts
	readyStamp bool   // emit a single JSON readiness line on stdout
}

// IsPortableMode determines if the CLI args indicate portable mode.
// Returns true if any positional arg is a spec file, a URL, or a directory containing spec files.
func IsPortableMode(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isURL(arg) || isSpecFile(arg) || isPackageFile(arg) {
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

// isPackageFile checks if a filename is a mockzilla package (.mock or .tar.gz).
func isPackageFile(name string) bool {
	return strings.HasSuffix(name, ".mock") || strings.HasSuffix(name, ".tar.gz")
}

// resolvePackageArgs checks if any positional arg is a .mock or .tar.gz package.
// If found, it downloads (if URL), extracts the archive, and rewrites the
// positional args and flags to point at the extracted directory contents.
// Only the first package arg is used; the rest are passed through unchanged.
func resolvePackageArgs(positional []string, fl *flags) []string {
	for i, arg := range positional {
		raw := arg
		if isURL(raw) {
			u, err := url.Parse(raw)
			if err != nil {
				continue
			}
			if !isPackageFile(filepath.Base(u.Path)) {
				continue
			}
		} else if !isPackageFile(raw) {
			continue
		}

		path := raw
		if isURL(raw) {
			downloaded, err := downloadFile(raw)
			if err != nil {
				slog.Error("Failed to download package", "url", raw, "error", err)
				continue
			}
			path = downloaded
		}

		dir, err := extractPackage(path)
		if err != nil {
			slog.Error("Failed to extract package", "path", path, "error", err)
			continue
		}
		slog.Info("Extracted package", "path", raw, "dir", dir)

		// Pick up config/context from the package, renaming them so
		// resolveSpecs doesn't treat them as OpenAPI specs (same
		// approach as RunFS).
		for _, name := range []string{"app.yml", "context.yml"} {
			p := filepath.Join(dir, name)
			if !fileExists(p) {
				continue
			}
			renamed := p + ".cfg"
			_ = os.Rename(p, renamed)
			switch name {
			case "app.yml":
				if fl.config == "" {
					fl.config = renamed
				}
			case "context.yml":
				if fl.context == "" {
					fl.context = renamed
				}
			}
		}

		// Replace the package arg with the extracted contents.
		// Pass openapi/ subdir first (where specs live), then the
		// parent dir (for static/ resolution) -- same layout as
		// RunFS and init.sh.
		rewritten := make([]string, 0, len(positional)+1)
		rewritten = append(rewritten, positional[:i]...)
		if openapiDir := filepath.Join(dir, "openapi"); fileExists(openapiDir) {
			rewritten = append(rewritten, openapiDir)
		}
		rewritten = append(rewritten, dir)
		rewritten = append(rewritten, positional[i+1:]...)
		return rewritten
	}
	return positional
}

// extractPackage unpacks a .mock or .tar.gz archive into a temp directory
// and returns the path to the extracted root.
func extractPackage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening package: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	dir, err := os.MkdirTemp("", "mockzilla-package-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading tar: %w", err)
		}

		target := filepath.Join(dir, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, dir) {
			continue // path traversal guard
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", fmt.Errorf("mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", fmt.Errorf("mkdir parent %s: %w", target, err)
			}
			out, err := os.Create(target)
			if err != nil {
				return "", fmt.Errorf("creating %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return "", fmt.Errorf("writing %s: %w", target, err)
			}
			_ = out.Close()
		}
	}

	return dir, nil
}

// downloadFile downloads any file from a URL to a temp file and returns the local path.
func downloadFile(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

	name := filepath.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		name = "package.mock"
	}

	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("fetching: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	dir := filepath.Join(os.TempDir(), "mockzilla-portable", "packages")
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
		return "", fmt.Errorf("writing file: %w", err)
	}

	slog.Info("Downloaded package", "url", rawURL, "path", path)
	return path, nil
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
	fs.IntVar(&fl.port, "port", -1, "Server port (0 = kernel picks a free port; default: from app config or 2200)")
	fs.StringVar(&fl.config, "config", "", "Unified config YAML (app settings + per-service config)")
	fs.StringVar(&fl.context, "context", "", "Per-service context YAML for value replacements")
	fs.BoolVar(&fl.readyStamp, "ready-stamp", false, "Emit a single JSON line on stdout once the HTTP listener is bound (for programmatic supervisors)")

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
