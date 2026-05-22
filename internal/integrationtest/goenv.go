package integrationtest

import (
	"os"
	"path/filepath"
)

// sandboxGoEnv returns environment variables that point Go's build cache
// and temp work directory inside the sandbox. Wired into every `go`
// command the integration tests run so the cache (which grows fast for
// a 2000-spec corpus) gets cleaned together with the sandbox instead of
// silently filling the user's global GOCACHE.
//
// Returned env is meant to be appended to os.Environ() on the *exec.Cmd:
//
//	cmd.Env = append(os.Environ(), sandboxGoEnv(sandboxDir)...)
//
// Directories are created on first call so callers don't have to.
func sandboxGoEnv(sandboxDir string) []string {
	cache := filepath.Join(sandboxDir, "go-build-cache")
	tmp := filepath.Join(sandboxDir, "go-tmp")
	_ = os.MkdirAll(cache, 0755)
	_ = os.MkdirAll(tmp, 0755)
	return []string{
		"GOCACHE=" + cache,
		"GOTMPDIR=" + tmp,
	}
}
