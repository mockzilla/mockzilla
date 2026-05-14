package portable

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
	"go.yaml.in/yaml/v4"
)

// Service is one discovered unit of work for portable mode. The folder
// (or file) the user pointed at maps to exactly one Service per spec.
//
// The folder name is the service identity: no derivation, no
// snake-casing, no map keys. That's the whole point of the layout:
// what the user types is what they see on the mounted URL.
type Service struct {
	Name      string // folder name (or single-spec basename), preserved as-is
	SpecPath  string // openapi.{yml,yaml,json}, possibly synthesized from static endpoint files
	ConfigDir string // dir holding config.yml / context.yml; empty for bare specs
	StaticDir string // <ConfigDir>/static, when present
}

const (
	configFile  = "config.yml"
	contextFile = "context.yml"
	appFile     = "app.yml"
	servicesDir = "services"
)

var specExts = []string{".yml", ".yaml", ".json"}

// isPortableArg returns true if arg looks like something portable mode
// can serve: a spec, a URL, a package, or a recognised directory shape.
func isPortableArg(arg string) bool {
	if isURL(arg) || isSpecFile(arg) || isPackageFile(arg) {
		return true
	}
	info, err := os.Stat(arg)
	if err != nil || !info.IsDir() {
		return false
	}
	if dirHasServicesRoot(arg) {
		return true
	}
	if findSpecInDir(arg) != "" {
		return true
	}
	if cmdapi.HasStaticEndpoints(arg) {
		return true
	}
	return false
}

// IsPortableMode reports whether the CLI args indicate portable mode.
func IsPortableMode(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if isPortableArg(a) {
			return true
		}
	}
	return false
}

// resolveServices walks the positional args and returns the services to
// register, in the order they appear. Each arg can contribute one or
// more services (a directory of services/<name>/ contributes many; a
// bare spec contributes one).
func resolveServices(args []string) ([]Service, error) {
	var out []Service
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		svcs, err := resolveOne(arg)
		if err != nil {
			slog.Error("Failed to resolve arg", "arg", arg, "error", err)
			continue
		}
		out = append(out, svcs...)
	}
	if len(out) == 0 {
		return nil, errors.New("no services found")
	}
	return out, nil
}

func resolveOne(arg string) ([]Service, error) {
	if isURL(arg) {
		if isPackageFile(filenameFromURL(arg)) {
			dir, err := downloadAndExtract(arg)
			if err != nil {
				return nil, err
			}
			return resolveDir(dir)
		}
		path, err := downloadSpec(arg)
		if err != nil {
			return nil, err
		}
		return []Service{{Name: serviceNameFromURL(arg, path), SpecPath: path}}, nil
	}

	info, err := os.Stat(arg)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", arg, err)
	}

	if info.IsDir() {
		return resolveDir(arg)
	}

	if isPackageFile(arg) {
		dir, err := extractPackage(arg)
		if err != nil {
			return nil, err
		}
		return resolveDir(dir)
	}

	if isSpecFile(arg) {
		return []Service{{Name: serviceNameFromFile(arg), SpecPath: arg}}, nil
	}

	return nil, fmt.Errorf("unrecognised input: %s", arg)
}

// resolveDir handles the recognised directory shapes, in order:
//
//  1. `services/<name>/` subdir → multi-service from that subtree.
//
//  2. The dir has a `config.yml`, has static endpoints, or has exactly
//     one top-level spec file: single-service folder, named after the
//     dir basename. See resolveServiceDir for the spec / static /
//     merge mode pick inside that one folder.
//
//  3. The dir has multiple top-level spec files (and none of the
//     single-service signals above): "flat root" mode. Each spec
//     becomes its own service named after its filename basename.
//     An optional `context.yml` at the root applies to every service.
func resolveDir(dir string) ([]Service, error) {
	servicesRoot := filepath.Join(dir, servicesDir)
	if info, err := os.Stat(servicesRoot); err == nil && info.IsDir() {
		return resolveServicesRoot(servicesRoot)
	}

	hasConfigFile := fileExists(filepath.Join(dir, configFile))
	hasStatic := cmdapi.HasStaticEndpoints(dir)
	specs := findAllSpecsInDir(dir)

	// "This folder IS one service" signals: explicit config, static
	// endpoints, or just a single top-level spec. Any of these means
	// the user has expressed the intent to treat this dir as a unit.
	if hasConfigFile || hasStatic || len(specs) == 1 {
		svc, err := resolveServiceDir(dir, inferServiceName(dir, hasConfigFile))
		if err != nil {
			return nil, err
		}
		return []Service{svc}, nil
	}

	// Flat root: one service per top-level spec file. The service
	// name comes from the spec basename so each ends up on a
	// meaningful URL prefix without any setup.
	if len(specs) == 0 {
		return nil, fmt.Errorf(
			"no services in %s (expected services/ subdir, a top-level spec, "+
				"a config.yml, or static endpoints)", dir)
	}
	out := make([]Service, 0, len(specs))
	for _, specPath := range specs {
		out = append(out, Service{
			Name:      serviceNameFromFile(specPath),
			SpecPath:  specPath,
			ConfigDir: dir, // shared root for context.yml
		})
	}
	return out, nil
}

func resolveServicesRoot(root string) ([]Service, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}

	var out []Service
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svcDir := filepath.Join(root, e.Name())
		svc, err := resolveServiceDir(svcDir, e.Name())
		if err != nil {
			slog.Error("Skipping service", "name", e.Name(), "error", err)
			continue
		}
		out = append(out, svc)
	}
	return out, nil
}

// resolveServiceDir resolves a single service folder into a Service.
//
// Three modes, all handled uniformly:
//
//   - Spec only (folder has a *.{yml,yaml,json} but no `<…>/<method>/
//     index.<ext>` files): the file is used as-is.
//   - Static only (no spec file): a spec is synthesized from the
//     static files.
//   - Both (spec + at least one static file): the spec is parsed,
//     static routes are overlaid on top: matching `(path, method)`
//     entries override the spec's response, others are added. The spec
//     file itself is also exposed at `GET /<filename>` as a literal
//     asset, so it stays fetchable for documentation.
func resolveServiceDir(dir, name string) (Service, error) {
	svc := Service{Name: name, ConfigDir: dir}

	specPath := findSpecInDir(dir)
	hasStatic := cmdapi.HasStaticEndpoints(dir)

	// Pure spec mode: nothing to overlay, use the file as-is.
	if specPath != "" && !hasStatic {
		svc.SpecPath = specPath
		return svc, nil
	}

	// Pure static or merge: we'll synthesize / overlay into a temp file.
	if specPath == "" && !hasStatic {
		return Service{}, fmt.Errorf(
			"no spec file and no <method>/index.<ext> static endpoints found in %s", dir)
	}

	routes, err := cmdapi.ScanStatic(dir)
	if err != nil {
		return Service{}, fmt.Errorf("scanning static endpoints: %w", err)
	}

	var built []byte
	if specPath == "" {
		built, err = cmdapi.GenerateSpecFromStaticDir(dir, name)
		if err != nil {
			return Service{}, fmt.Errorf("synthesizing spec from static: %w", err)
		}
	} else {
		specBytes, err := os.ReadFile(specPath)
		if err != nil {
			return Service{}, fmt.Errorf("reading spec: %w", err)
		}
		built, err = cmdapi.MergeStaticIntoSpec(specBytes, routes)
		if err != nil {
			return Service{}, fmt.Errorf("merging static into spec: %w", err)
		}
	}

	tmpPath, err := writeTempSpec(name, built)
	if err != nil {
		return Service{}, fmt.Errorf("writing merged spec: %w", err)
	}
	svc.SpecPath = tmpPath
	svc.StaticDir = dir
	return svc, nil
}

// writeTempSpec persists a synthesized/merged spec into a per-process
// temp dir keyed by service name. The runtime expects a filesystem
// path for the spec (so watchers can latch onto it); we don't bother
// cleaning these up; temp dirs are reclaimed by the OS.
func writeTempSpec(name string, body []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "mockzilla-portable", "specs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".yml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// findAllSpecsInDir returns every top-level non-reserved spec file in
// the directory, sorted alphabetically. Used by flat-root mode where
// each spec becomes its own service, and by findSpecInDir to pick the
// canonical spec in a single-service folder.
//
// Reserved names are excluded: config.yml, context.yml, app.yml, and
// anything named `index.<ext>` (those are static endpoint files, not
// OpenAPI specs).
// inferServiceName picks a name for a single-folder invocation by
// looking inside the folder, never at the folder's own basename.
//
// In priority order:
//
//  1. The `name:` field of a top-level config.yml.
//  2. The basename of a single non-generic spec file (any *.{yml,yaml,
//     json} other than `openapi.*`, which is too generic to convey
//     identity).
//
// If neither signal is present, we return an empty string. Empty Name
// is a first-class state across the runtime: the service mounts at
// `/`, and the UI surfaces it as `.root` (api.RootServiceName). This
// matches the user intent of "I just want to serve this folder, don't
// care about a prefix" without leaking the cwd's basename into URLs.
func inferServiceName(dir string, hasConfigFile bool) string {
	if hasConfigFile {
		if name := readConfigName(dir); name != "" {
			return name
		}
	}
	for _, spec := range findAllSpecsInDir(dir) {
		base := filepath.Base(spec)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if strings.EqualFold(stem, "openapi") {
			continue
		}
		return stem
	}
	return ""
}

// readConfigName parses just the `name:` field out of a service
// folder's config.yml. Returns empty string when the file is missing,
// malformed, or doesn't carry a name.
func readConfigName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		return ""
	}
	var probe struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return probe.Name
}

func findAllSpecsInDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == configFile || name == contextFile || name == appFile {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if stem == "index" {
			continue
		}
		if !isSpecFile(name) {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, name))
	}
	sort.Strings(candidates)
	return candidates
}

// findSpecInDir returns the path to a single canonical OpenAPI spec
// inside a service folder, or empty string if none is present. When
// multiple candidates exist, the alphabetically first one wins and
// the others are logged so the user can disambiguate.
func findSpecInDir(dir string) string {
	candidates := findAllSpecsInDir(dir)
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) > 1 {
		slog.Warn("Multiple spec candidates in folder; using first",
			"dir", dir, "chosen", candidates[0], "others", candidates[1:])
	}
	return candidates[0]
}

func dirHasServicesRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, servicesDir))
	return err == nil && info.IsDir()
}

func isSpecFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range specExts {
		if ext == e {
			return true
		}
	}
	return false
}

func isPackageFile(name string) bool {
	return strings.HasSuffix(name, ".mockz") || strings.HasSuffix(name, ".tar.gz")
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func serviceNameFromFile(path string) string {
	base := filepath.Base(path)
	for _, ext := range specExts {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			return strings.TrimSuffix(base, base[len(base)-len(ext):])
		}
	}
	return base
}

// genericSpecBases are filename stems that don't identify a service in
// any useful way ("openapi.json" appears in almost every API). For
// these, derive the service name from the URL hostname instead so the
// mount path doesn't end up as `/openapi`.
var genericSpecBases = map[string]bool{
	"openapi": true,
	"swagger": true,
	"api":     true,
	"spec":    true,
	"schema":  true,
}

// serviceNameFromURL picks a service name for a remote spec. Prefer the
// filename basename when it's something specific (e.g. `petstore.yml`),
// otherwise fall back to the URL host (e.g. `https://api.example.com/openapi.json`
// → "api.example.com" → "api_example_com").
func serviceNameFromURL(rawURL, downloadedPath string) string {
	base := serviceNameFromFile(downloadedPath)
	if !genericSpecBases[strings.ToLower(base)] {
		return base
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return base
	}
	host := strings.TrimPrefix(u.Host, "www.")
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i] // strip :port
	}
	return host
}

func filenameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return filepath.Base(u.Path)
}

// downloadSpec downloads a spec from a URL to a temp file and returns the path.
func downloadSpec(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}

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

func downloadAndExtract(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL: %w", err)
	}
	name := filepath.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		name = "package.mockz"
	}

	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("fetching: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	tmp := filepath.Join(os.TempDir(), "mockzilla-portable", "packages")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	path := filepath.Join(tmp, name)
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("writing package: %w", err)
	}
	_ = f.Close()

	slog.Info("Downloaded package", "url", rawURL, "path", path)
	return extractPackage(path)
}

// extractPackage unpacks a .mockz or .tar.gz archive into a temp dir
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

	slog.Info("Extracted package", "path", path, "dir", dir)
	return dir, nil
}
