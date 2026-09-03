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
	"github.com/mockzilla/mockzilla/v2/pkg/pack"
	"go.yaml.in/yaml/v4"
)

// Service is one discovered unit of work for portable mode. The folder name is
// the service identity verbatim; what the user types is what they see on the URL.
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
// can serve: a spec, a URL, a package, a recognised directory shape,
// or any single static-content file (.json/.html/.txt/.xml/.yml/...)
// that we can fall back to serving at `GET /`.
func isPortableArg(arg string) bool {
	if isURL(arg) || isSpecFile(arg) || isPackageFile(arg) {
		return true
	}

	info, err := os.Stat(arg)
	if err != nil {
		return false
	}

	if !info.IsDir() {
		return isStaticContentFile(arg)
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
		// Fast path: URL has a recognised package extension. Skips the
		// content sniff and goes straight to download-and-extract.
		if isPackageFile(filenameFromURL(arg)) {
			dir, err := downloadAndExtract(arg)
			if err != nil {
				return nil, err
			}
			return resolveDir(dir)
		}

		// Slow path: URL has no extension hint.
		// Fetch the body and dispatch based on Content-Type and gzip magic bytes.
		return resolveURLByContent(arg)
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
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", arg, err)
		}
		if !looksLikeOpenAPISpec(data) {
			return resolveStaticFile(arg, data, "")
		}
		return []Service{{Name: serviceNameFromFile(arg), SpecPath: arg}}, nil
	}

	if isStaticContentFile(arg) {
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", arg, err)
		}
		return resolveStaticFile(arg, data, "")
	}

	return nil, fmt.Errorf("unrecognised input: %s", arg)
}

// resolveDir handles the recognized directory shapes, in order:
//  0. `.mockzilla.json` manifest at the root: build services from declared entries.
//  1. `services/<name>/` subdir: multi-service from that subtree (explicit).
//  2. Implicit services-root: dir with no top-level service-signal files
//     (no config.yml, no spec, no top-level index.<ext>) and at least one
//     non-noise subdirectory. Each subdir becomes a service. Lets users
//     skip the explicit `services/` wrapper for "folder of services" layouts.
//  3. dir with `config.yml`, static endpoints, or exactly one top-level spec:
//     single-service folder named after the dir basename.
//  4. multiple top-level spec files: flat-root mode, one service per spec basename.
//     An optional `context.yml` at the root applies to every service.
func resolveDir(dir string) ([]Service, error) {
	if services, err := resolveFromManifest(dir); err != nil {
		return nil, err
	} else if services != nil {
		return services, nil
	}

	servicesRoot := filepath.Join(dir, servicesDir)
	if info, err := os.Stat(servicesRoot); err == nil && info.IsDir() {
		return resolveServicesRoot(servicesRoot)
	}

	hasConfigFile := fileExists(filepath.Join(dir, configFile))
	specs := findAllSpecsInDir(dir)

	if isImplicitServicesRoot(dir, hasConfigFile, specs) {
		return resolveServicesRoot(dir)
	}

	hasStatic := cmdapi.HasStaticEndpoints(dir)

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

		if cmdapi.ShouldSkipDir(e.Name()) {
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

// isImplicitServicesRoot detects a "folder of services" layout where the
// user skipped the explicit `services/` wrapper. The dir qualifies when
// it has no top-level service-signal files (no config.yml, no spec, no
// `index.<ext>`) and contains at least one non-noise subdirectory. Each
// such subdirectory will be resolved as its own service.
//
// The no-top-level-files rule is what disambiguates this from the
// single-service-with-deep-endpoints case: as soon as the user drops
// any top-level file (an `openapi.yml`, a `config.yml`, or even a bare
// `index.json` at the root), we read that as "this dir IS the service"
// and don't walk subdirs as services.
func isImplicitServicesRoot(dir string, hasConfigFile bool, specs []string) bool {
	if hasConfigFile || len(specs) > 0 {
		return false
	}
	if hasTopLevelIndexFile(dir) {
		return false
	}
	return hasServiceCandidateSubdir(dir)
}

// hasTopLevelIndexFile reports whether the dir has an `index.<ext>` file
// directly at its top level (a static endpoint for the service root).
// Used as a "this dir IS a service" signal.
func hasTopLevelIndexFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if stem != "index" {
			continue
		}
		if cmdapi.GetContentType(filepath.Ext(name)) != "" {
			return true
		}
	}
	return false
}

// excludeSpecAssetRoute drops the auto-generated `GET /<spec-basename>`
// route that scanStaticFiles adds for the spec file itself, so the spec
// doesn't show up as one of its own endpoints in the merged service.
func excludeSpecAssetRoute(routes []cmdapi.Route, specPath string) []cmdapi.Route {
	if specPath == "" {
		return routes
	}
	skipPath := "/" + filepath.Base(specPath)
	out := make([]cmdapi.Route, 0, len(routes))

	for _, r := range routes {
		if r.Path == skipPath && strings.EqualFold(r.Method, "GET") {
			continue
		}
		out = append(out, r)
	}
	return out
}

// hasServiceCandidateSubdir reports whether the dir contains at least one
// subdirectory that isn't filtered as noise (.git, node_modules, …).
func hasServiceCandidateSubdir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if cmdapi.ShouldSkipDir(e.Name()) {
			continue
		}
		return true
	}
	return false
}

// resolveServiceDir resolves a single service folder into a Service. Three modes:
//   - spec only: the file is used as-is.
//   - static only: a spec is synthesized from the static files.
//   - both: the spec is parsed and static routes overlaid (matching `(path, method)`
//     overrides the spec response; the spec file stays fetchable at `GET /<filename>`).
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

		// The static scan also picks up the spec file itself as a
		// top-level literal asset (e.g. `GET /openapi.yml`). Including
		// that in the merged operation list would surface the spec as
		// a regular endpoint in the UI - chicken-and-egg, since those
		// endpoints were derived from this file. Drop it before merging.
		routes = excludeSpecAssetRoute(routes, specPath)
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

// resolveFromManifest reads `.mockzilla.json` at the root of dir and
// builds the service list directly from its declared entries. Returns
// (nil, nil) when the manifest is absent so the caller can fall
// through to the discovery-based shapes (services/, single-folder,
// flat-root). A malformed or future-format manifest is an error.
//
// For each manifest entry, the service folder is `dir + entry.Dir`
// (or dir itself when Dir is empty). The resulting Service mirrors
// what discovery would have produced, except we already know the name
// and mount from pack time so we don't re-infer them.
func resolveFromManifest(dir string) ([]Service, error) {
	manifest, err := pack.LoadManifestFromDir(dir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return nil, nil
	}

	out := make([]Service, 0, len(manifest.Services))
	for _, entry := range manifest.Services {
		svc, err := serviceFromManifestEntry(dir, entry)
		if err != nil {
			slog.Error("Skipping manifest service", "name", entry.Name, "error", err)
			continue
		}
		out = append(out, svc)
	}
	if len(out) == 0 {
		return nil, errors.New("manifest declares no usable services")
	}
	return out, nil
}

// serviceFromManifestEntry turns a manifest service entry into a
// runtime-ready Service. Static and merge modes still need a spec
// document; we reuse resolveServiceDir on the service folder to
// synthesize or merge it (the manifest declaration gives us name and
// mode upfront so the runtime trusts them rather than re-inferring).
func serviceFromManifestEntry(archiveDir string, entry pack.ServiceEntry) (Service, error) {
	svcDir := archiveDir
	if entry.Dir != "" {
		svcDir = filepath.Join(archiveDir, filepath.FromSlash(entry.Dir))
	}
	if entry.Mode == pack.ModeSpec && entry.Files.Spec != "" {
		return Service{
			Name:      entry.Name,
			SpecPath:  filepath.Join(archiveDir, filepath.FromSlash(entry.Files.Spec)),
			ConfigDir: svcDir,
		}, nil
	}

	// Static / merge modes need a synthesized or overlaid spec.
	// resolveServiceDir already handles both; let it run on the folder
	// the manifest pointed at, using the manifest-supplied name (skips
	// the re-inference step).
	return resolveServiceDir(svcDir, entry.Name)
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

// inferServiceName picks a name for a single-folder invocation. In order:
//  1. The `name:` field of a top-level config.yml.
//  2. The basename of a single non-generic spec file (anything but `openapi.*`).
//  3. The folder's own basename, as a last-resort fallback so multi-arg
//     invocations like `mockzilla a.yml b.yml ./other` mount the folder
//     at `/other` instead of at `/`. Cwd-shaped args (`.`, `./`, `..`)
//     are skipped here so they keep falling through to empty.
//
// Empty string is a first-class state: the service mounts at `/` and the UI
// surfaces it as `.root` (api.RootServiceName).
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
	return folderBasename(dir)
}

// folderBasename returns the folder's own basename when it's a useful name,
// or empty string when the arg points at the current/parent dir or the
// filesystem root. Used by inferServiceName as the last fallback.
func folderBasename(dir string) string {
	base := filepath.Base(filepath.Clean(dir))
	switch base {
	case ".", "..", string(filepath.Separator):
		return ""
	}
	return base
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

// findAllSpecsInDir returns every top-level non-reserved spec file in the dir,
// sorted alphabetically. Reserved names (config, context, app, codegen, index)
// are excluded in every spec extension, so `codegen.yaml` is skipped the same
// way `codegen.yml` is.
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
		if isReservedName(name) {
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

// isReservedName reports whether a file is one the loader owns rather than a
// spec to serve. Matched on the stem, because every reserved name is also a
// legal spec extension: a `codegen.yaml` next to an `openapi.yml` would
// otherwise sort first and be served as the spec.
func isReservedName(name string) bool {
	switch strings.TrimSuffix(name, filepath.Ext(name)) {
	case "config", "context", "app", "codegen", "index":
		return true
	}
	return false
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

// serviceNameFromURL picks a service name for a remote spec. Prefer the filename
// basename when specific (e.g. `petstore.yml`); otherwise fall back to the URL
// host with dots replaced by underscores.
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

// resolveURLByContent fetches an extensionless URL once, classifies the
// body as either a portable package or an OpenAPI spec, and dispatches.
func resolveURLByContent(rawURL string) ([]Service, error) {
	body, contentType, err := fetchURL(rawURL)
	if err != nil {
		return nil, err
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}

	if isPackageBytes(contentType, body) {
		path, err := writeTempBytes(
			filepath.Join(os.TempDir(), "mockzilla-portable", "packages"),
			packageNameFromURL(parsed),
			body,
		)
		if err != nil {
			return nil, err
		}

		slog.Info("Downloaded package", "url", rawURL, "path", path, "content_type", contentType)
		dir, err := extractPackage(path)
		if err != nil {
			return nil, err
		}
		return resolveDir(dir)
	}

	if !looksLikeOpenAPISpec(body) {
		slog.Info("Remote body is not an OpenAPI spec; serving as static",
			"url", rawURL, "content_type", contentType)
		return resolveStaticFile(parsed.Path, body, contentType)
	}

	path, err := writeTempBytes(
		filepath.Join(os.TempDir(), "mockzilla-portable", "specs"),
		specNameFromURL(parsed),
		body,
	)
	if err != nil {
		return nil, err
	}

	slog.Info("Downloaded spec", "url", rawURL, "path", path, "content_type", contentType)
	return []Service{{Name: serviceNameFromURL(rawURL, path), SpecPath: path}}, nil
}

func fetchURL(rawURL string) ([]byte, string, error) {
	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return nil, "", fmt.Errorf("fetching: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading body: %w", err)
	}

	return body, resp.Header.Get("Content-Type"), nil
}

// isPackageBytes decides whether a fetched body should be unpacked as a
// portable .mockz. Content-Type wins when explicit (`application/gzip`,
// our vendor types). Magic bytes catch ambiguous types like
// `application/octet-stream` and the missing-Content-Type case. The tar
// inside is validated later in extractPackage; this is a fast pre-filter.
func isPackageBytes(contentType string, body []byte) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "application/gzip", "application/x-gzip",
		"application/vnd.mockz", "application/vnd.mockz+gzip":
		return true
	}
	return len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b
}

func writeTempBytes(dir, name string, body []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

func packageNameFromURL(parsed *url.URL) string {
	name := filepath.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		return "package.mockz"
	}
	return name
}

func specNameFromURL(parsed *url.URL) string {
	name := filepath.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		name = parsed.Host
	}
	if !isSpecFile(name) {
		name += ".yml"
	}
	return name
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
