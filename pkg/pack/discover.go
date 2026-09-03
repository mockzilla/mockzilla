package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
)

const (
	configFile  = "config.yml"
	contextFile = "context.yml"
	appFile     = "app.yml"
	servicesDir = "services"
)

var specExts = []string{".yml", ".yaml", ".json"}

// Discover walks srcDir and returns the service registry the manifest
// should declare. The returned ServiceEntry paths are relative to
// srcDir (so they round-trip cleanly as tar entry names inside the
// archive).
//
// Mirrors `internal/portable`'s runtime discovery so a packed archive yields the
// same service set as a raw directory invocation. Four shapes:
//   - `services/<name>/` subtree: one entry per child folder (explicit).
//   - Implicit services-root: no top-level service-signal files but at
//     least one non-noise subdir; each subdir becomes a service.
//   - `config.yml`, static endpoints, or one root-level spec: single-service folder,
//     named from `config.yml`'s `name:` or a non-generic spec basename.
//   - Multiple top-level spec files: flat-root mode, one service per spec basename.
func Discover(srcDir string) ([]ServiceEntry, error) {
	info, err := os.Stat(srcDir)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", srcDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", srcDir)
	}

	servicesRoot := filepath.Join(srcDir, servicesDir)
	if info, err := os.Stat(servicesRoot); err == nil && info.IsDir() {
		return discoverServicesRoot(srcDir, servicesRoot)
	}

	hasConfig := fileExists(filepath.Join(srcDir, configFile))
	specs := findAllSpecsInDir(srcDir)

	if isImplicitServicesRoot(srcDir, hasConfig, specs) {
		return discoverServicesRoot(srcDir, srcDir)
	}

	hasStatic := cmdapi.HasStaticEndpoints(srcDir)

	if hasConfig || hasStatic || len(specs) == 1 {
		entry, err := serviceEntryFromDir(srcDir, srcDir, "", inferServiceName(srcDir, hasConfig))
		if err != nil {
			return nil, err
		}
		return []ServiceEntry{entry}, nil
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf(
			"nothing to pack in %s (expected services/ subdir, a top-level spec, "+
				"a config.yml, or static endpoints)", srcDir)
	}

	out := make([]ServiceEntry, 0, len(specs))
	for _, specPath := range specs {
		rel, err := filepath.Rel(srcDir, specPath)
		if err != nil {
			return nil, err
		}
		name := serviceNameFromFile(specPath)
		out = append(out, ServiceEntry{
			Name:  name,
			Mount: "/" + name,
			Mode:  ModeSpec,
			Dir:   "", // flat-root specs live at the archive root
			Files: ServiceFiles{Spec: filepath.ToSlash(rel)},
		})
	}
	return out, nil
}

func discoverServicesRoot(srcDir, servicesRootDir string) ([]ServiceEntry, error) {
	entries, err := os.ReadDir(servicesRootDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", servicesRootDir, err)
	}
	var out []ServiceEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if cmdapi.ShouldSkipDir(e.Name()) {
			continue
		}
		svcDir := filepath.Join(servicesRootDir, e.Name())
		relDir, err := filepath.Rel(srcDir, svcDir)
		if err != nil {
			return nil, err
		}
		entry, err := serviceEntryFromDir(srcDir, svcDir, filepath.ToSlash(relDir), e.Name())
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", e.Name(), err)
		}
		out = append(out, entry)
	}
	return out, nil
}

// serviceEntryFromDir resolves a single service folder into a manifest
// entry. Paths in the returned entry are relative to srcDir so they
// match the tar entry names in the eventual archive. relDir is the
// service folder's in-archive path (empty when the archive root itself
// is the service folder).
func serviceEntryFromDir(srcDir, dir, relDir, name string) (ServiceEntry, error) {
	mount := readMountOverride(dir)
	if mount == "" {
		mount = mountFromName(name)
	}

	specPath := findSpecInDir(dir)
	hasStatic := cmdapi.HasStaticEndpoints(dir)

	if specPath == "" && !hasStatic {
		return ServiceEntry{}, fmt.Errorf(
			"no spec file and no <…>/index.<ext> static endpoints found in %s", dir)
	}

	mode := ModeSpec
	switch {
	case specPath != "" && hasStatic:
		mode = ModeMerge
	case specPath == "" && hasStatic:
		mode = ModeStatic
	}

	files, err := buildServiceFiles(srcDir, dir, specPath)
	if err != nil {
		return ServiceEntry{}, err
	}

	endpoints, err := buildEndpointList(srcDir, dir, hasStatic)
	if err != nil {
		return ServiceEntry{}, err
	}

	return ServiceEntry{
		Name:      name,
		Mount:     mount,
		Mode:      mode,
		Dir:       relDir,
		Files:     files,
		Endpoints: endpoints,
	}, nil
}

func buildServiceFiles(srcDir, dir, specPath string) (ServiceFiles, error) {
	var files ServiceFiles
	if specPath != "" {
		rel, err := filepath.Rel(srcDir, specPath)
		if err != nil {
			return files, err
		}
		files.Spec = filepath.ToSlash(rel)
	}
	if cfg := filepath.Join(dir, configFile); fileExists(cfg) {
		rel, _ := filepath.Rel(srcDir, cfg)
		files.Config = filepath.ToSlash(rel)
	}
	if ctx := filepath.Join(dir, contextFile); fileExists(ctx) {
		rel, _ := filepath.Rel(srcDir, ctx)
		files.Context = filepath.ToSlash(rel)
	}
	return files, nil
}

func buildEndpointList(srcDir, dir string, hasStatic bool) ([]EndpointEntry, error) {
	if !hasStatic {
		return nil, nil
	}
	routes, err := cmdapi.ScanStatic(dir)
	if err != nil {
		return nil, err
	}
	out := make([]EndpointEntry, 0, len(routes))
	for _, route := range routes {
		var fileRel string
		if route.SourceFile != "" {
			rel, err := filepath.Rel(srcDir, route.SourceFile)
			if err != nil {
				return nil, fmt.Errorf("relative path for %s: %w", route.SourceFile, err)
			}
			fileRel = filepath.ToSlash(rel)
		}
		out = append(out, EndpointEntry{
			Method:      strings.ToUpper(route.Method),
			Path:        route.Path,
			File:        fileRel,
			ContentType: route.ContentType,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out, nil
}

// readMountOverride parses just the `mount:` field out of config.yml,
// or returns empty if config.yml is absent or the field isn't set.
// Avoids a full ServiceConfig parse since we only care about one
// field here.
func readMountOverride(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "mount:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(trimmed, "mount:"))
		v = strings.Trim(v, `"'`)
		if v == "" {
			continue
		}
		if !strings.HasPrefix(v, "/") {
			v = "/" + v
		}
		return v
	}
	return ""
}

func mountFromName(name string) string {
	if name == "" {
		return "/"
	}
	return "/" + name
}

// isImplicitServicesRoot mirrors the runtime detector in
// internal/portable/resolve.go. Keep the two in sync so `mockzilla pack
// ./foo/` and `mockzilla ./foo/` discover the same set of services.
func isImplicitServicesRoot(dir string, hasConfigFile bool, specs []string) bool {
	if hasConfigFile || len(specs) > 0 {
		return false
	}
	if hasTopLevelIndexFile(dir) {
		return false
	}
	return hasServiceCandidateSubdir(dir)
}

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

// inferServiceName mirrors the portable runtime's name inference so that
// `mockzilla pack ./foo/` and `mockzilla ./foo/` agree on the service
// name (and therefore the mount URL). See
// internal/portable/resolve.go:inferServiceName.
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
	base := filepath.Base(filepath.Clean(dir))
	switch base {
	case ".", "..", string(filepath.Separator):
		return ""
	}
	return base
}

func readConfigName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "name:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		return strings.Trim(v, `"'`)
	}
	return ""
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

func findSpecInDir(dir string) string {
	c := findAllSpecsInDir(dir)
	if len(c) == 0 {
		return ""
	}
	return c[0]
}

// isReservedName reports whether a file is one the tooling owns rather than a
// spec to serve. Matched on the stem, because every reserved name is also a
// legal spec extension.
//
// This has to agree with the identical rule in internal/portable. The manifest
// written here is what the runtime registers services from, so when the two
// disagree a pack names one file as the spec and a live walk would name
// another, and the runtime believes the manifest.
func isReservedName(name string) bool {
	switch strings.TrimSuffix(name, filepath.Ext(name)) {
	case "config", "context", "app", "codegen", "index":
		return true
	}
	return false
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

func serviceNameFromFile(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
