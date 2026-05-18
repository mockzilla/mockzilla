package portable

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/factory"
)

// watchServices watches each service's spec, config.yml, context.yml,
// and static endpoint files for changes; on debounce timeout it
// rebuilds the affected service's handler and swaps it in-place.
func watchServices(
	services []Service,
	router *api.Router,
	handlers map[string]*swappableHandler,
) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Failed to create file watcher", "error", err)
		return
	}
	defer func() { _ = watcher.Close() }()

	// Map: filesystem path → service names. A single dir can host
	// several services (flat-root mode: each top-level spec is its own
	// service sharing the parent dir), so the value is a slice. We
	// register watchers on directories (fsnotify can't watch nonexistent
	// files), then route events back via the directory the event
	// happened in, narrowed by filename when multiple services share it.
	// Resolve to absolute paths: fsnotify echoes back event paths in the
	// same form they were registered, and matchServices walks parents
	// stopping at "/" or ".". Relative dirs like "." (from a bare spec
	// in the cwd) would then never match anything.
	dirToServices := make(map[string][]string)
	watched := make(map[string]bool)

	// bareSpecs: service-name → spec basename, for services where the
	// user pointed at a single file (no ConfigDir, no StaticDir). The
	// watched dir is then likely full of unrelated files (think
	// `mockzilla petstore.yaml` from ~/Downloads), so we filter strictly
	// to the spec's own filename.
	bareSpecs := make(map[string]string)
	for _, svc := range services {
		if svc.ConfigDir == "" && svc.StaticDir == "" && svc.SpecPath != "" {
			bareSpecs[svc.Name] = filepath.Base(svc.SpecPath)
		}

		for _, d := range dirsToWatch(svc) {
			if d == "" {
				continue
			}

			abs, err := filepath.Abs(d)
			if err != nil {
				slog.Debug("Failed to resolve watch dir", "dir", d, "error", err)
				continue
			}

			if !watched[abs] {
				if err := watcher.Add(abs); err != nil {
					slog.Debug("Failed to watch dir", "dir", abs, "error", err)
					continue
				}
				watched[abs] = true
			}
			dirToServices[abs] = append(dirToServices[abs], svc.Name)
		}
	}

	serviceByName := make(map[string]Service, len(services))
	for _, s := range services {
		serviceByName[s.Name] = s
	}

	var debounceTimer *time.Timer
	pending := make(map[string]bool)
	var mu sync.Mutex

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !isReloadEvent(event) {
				continue
			}
			names := matchServices(event.Name, dirToServices)
			names = filterBareSpecs(names, event.Name, bareSpecs)
			if len(names) == 0 {
				continue
			}

			mu.Lock()
			for _, n := range names {
				pending[n] = true
			}
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(2*time.Second, func() {
				mu.Lock()
				names := make([]string, 0, len(pending))
				for n := range pending {
					names = append(names, n)
				}
				pending = make(map[string]bool)
				mu.Unlock()

				for _, n := range names {
					reloadService(serviceByName[n], router, handlers)
				}
			})
			mu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("File watcher error", "error", err)
		}
	}
}

// dirsToWatch returns the directories whose events should trigger a
// reload of the given service. Watching dirs (not files) handles the
// editor-rename-on-save pattern fsnotify can't otherwise see.
func dirsToWatch(svc Service) []string {
	var dirs []string
	if svc.ConfigDir != "" {
		dirs = append(dirs, svc.ConfigDir)
	}

	// Skip the spec's parent when the spec was synthesized: each reload
	// rewrites that file via writeTempSpec, which would feed straight
	// back into the watcher and loop forever. StaticDir != "" is the
	// synthesis signal; the real sources live under ConfigDir/StaticDir
	// which are already watched.
	if svc.SpecPath != "" && svc.StaticDir == "" {
		dirs = append(dirs, filepath.Dir(svc.SpecPath))
	}
	if svc.StaticDir != "" {
		dirs = append(dirs, svc.StaticDir)
	}
	return dedup(dirs)
}

func dedup(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// matchServices returns the service names for an event path. It walks
// the directory hierarchy up until it hits a watched directory (the
// tightest-fitting one wins). When that dir hosts multiple services
// (flat-root), it narrows by the event's filename: a spec file
// `<name>.<ext>` routes to just that service; shared signals like
// config.yml/context.yml reload all candidates.
func matchServices(eventPath string, dirToServices map[string][]string) []string {
	var candidates []string
	dir := filepath.Dir(eventPath)
	for dir != "/" && dir != "." {
		if names, ok := dirToServices[dir]; ok {
			candidates = names
			break
		}
		dir = filepath.Dir(dir)
	}

	if candidates == nil {
		if names, ok := dirToServices[eventPath]; ok {
			candidates = names
		}
	}
	if len(candidates) <= 1 {
		return candidates
	}

	base := filepath.Base(eventPath)
	// Shared signals apply to every service sharing the dir. Checked
	// before isSpecFile because context.yml/config.yml are also `.yml`.
	if base == configFile || base == contextFile {
		return candidates
	}

	if isSpecFile(base) {
		stem := serviceNameFromFile(base)
		for _, n := range candidates {
			if n == stem {
				return []string{n}
			}
		}
		// Spec file in the shared dir but no matching service: ignore.
		return nil
	}

	// Ambiguous files (e.g. unknown extensions) reload every candidate.
	// Safer to over-reload than to miss an edit.
	return candidates
}

// filterBareSpecs drops bare-spec services from `names` unless the event
// is on the spec file itself. Bare specs watch their parent dir purely
// because fsnotify can't reliably follow editor-rename-on-save on a
// single file, but the parent (e.g. ~/Downloads) is full of noise that
// shouldn't trigger reloads.
func filterBareSpecs(names []string, eventPath string, bareSpecs map[string]string) []string {
	if len(bareSpecs) == 0 || len(names) == 0 {
		return names
	}
	base := filepath.Base(eventPath)
	out := names[:0]
	for _, n := range names {
		if specBase, ok := bareSpecs[n]; ok && base != specBase {
			continue
		}
		out = append(out, n)
	}
	return out
}

func isReloadEvent(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}
	base := filepath.Base(event.Name)
	if base == configFile || base == contextFile {
		return true
	}
	if isSpecFile(base) {
		return true
	}
	// Any file with an extension inside the service folder might be a
	// static endpoint or asset; trigger a reload.
	return filepath.Ext(base) != ""
}

func reloadService(svc Service, router *api.Router, handlers map[string]*swappableHandler) {
	sw, ok := handlers[svc.Name]
	if !ok {
		slog.Warn("No handler to reload", "service", svc.Name)
		return
	}

	// Static/merge services need to re-resolve so changes to static
	// endpoint files get re-synthesized into the spec. Pure-spec
	// services don't: the on-disk spec IS the source of truth, and
	// re-resolving by ConfigDir would break bare-spec mode (empty
	// ConfigDir) and flat-root mode (shared ConfigDir, where
	// findSpecInDir would pick the wrong service's spec).
	rebuilt := svc
	if svc.StaticDir != "" {
		r, err := resolveServiceDir(svc.StaticDir, svc.Name)
		if err != nil {
			slog.Error("Failed to re-resolve service", "name", svc.Name, "error", err)
			return
		}
		rebuilt = r
	}

	h, err := buildHandler(rebuilt)
	if err != nil {
		slog.Error("Failed to reload service", "name", svc.Name, "error", err)
		return
	}
	sw.swap(h, buildValidator(svc.Name, h))
	slog.Info("Reloaded service", "name", svc.Name)
	_ = router
}

func buildHandler(svc Service) (*handler, error) {
	specBytes, err := os.ReadFile(svc.SpecPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}
	ctxBytes, err := loadServiceContext(svc)
	if err != nil {
		return nil, fmt.Errorf("loading context: %w", err)
	}

	var opts []factory.FactoryOption
	if ctxBytes != nil {
		opts = append(opts, factory.WithServiceContext(ctxBytes))
	}
	opts = append(opts, factory.WithSpecOptions(&config.SpecOptions{LazyLoad: true}))
	return newHandler(specBytes, opts...)
}
