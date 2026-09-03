package files

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SpecExts are the file extensions an OpenAPI spec may carry.
var SpecExts = []string{".yml", ".yaml", ".json"}

// FindSpecs returns every top-level non-reserved spec file in dir, sorted.
// The packer shares it with the loader deliberately: the manifest it writes is
// what the runtime registers from, so the two picking different files fails
// silently.
func FindSpecs(dir string) []string {
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
		if IsReservedName(name) || !IsSpec(name) {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, name))
	}

	sort.Strings(candidates)
	return candidates
}

// IsSpec reports whether name carries a spec extension.
func IsSpec(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range SpecExts {
		if ext == e {
			return true
		}
	}
	return false
}

// IsReservedName reports whether name belongs to the tooling rather than being
// a spec to serve. Matched on the stem, because every reserved name is also a
// legal spec extension.
func IsReservedName(name string) bool {
	switch strings.TrimSuffix(name, filepath.Ext(name)) {
	case "config", "context", "app", "codegen", "index":
		return true
	}
	return false
}
