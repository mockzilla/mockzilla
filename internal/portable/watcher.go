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

	// Map: filesystem path → service name. We register watchers on
	// directories (fsnotify can't watch nonexistent files), then route
	// events back to the affected service via the directory the event
	// happened in.
	dirToService := make(map[string]string)
	for _, svc := range services {
		dirs := dirsToWatch(svc)
		for _, d := range dirs {
			if d == "" {
				continue
			}
			if err := watcher.Add(d); err != nil {
				slog.Debug("Failed to watch dir", "dir", d, "error", err)
				continue
			}
			dirToService[d] = svc.Name
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
			name := matchService(event.Name, dirToService)
			if name == "" {
				continue
			}
			if !isReloadEvent(event) {
				continue
			}

			mu.Lock()
			pending[name] = true
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
	if svc.SpecPath != "" {
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

// matchService returns the service name for an event path by walking
// the directory hierarchy up until it hits a watched directory.
func matchService(eventPath string, dirToService map[string]string) string {
	dir := filepath.Dir(eventPath)
	for dir != "/" && dir != "." {
		if name, ok := dirToService[dir]; ok {
			return name
		}
		dir = filepath.Dir(dir)
	}
	if name, ok := dirToService[eventPath]; ok {
		return name
	}
	return ""
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

	// Re-resolve from disk so spec/static changes are picked up
	// uniformly regardless of which mode the folder is in now. It can
	// even flip between modes (e.g. a user drops their first static
	// file alongside an existing spec).
	rebuilt, err := resolveServiceDir(svc.ConfigDir, svc.Name)
	if err != nil {
		slog.Error("Failed to re-resolve service", "name", svc.Name, "error", err)
		return
	}

	h, err := buildHandler(rebuilt)
	if err != nil {
		slog.Error("Failed to reload service", "name", svc.Name, "error", err)
		return
	}
	sw.swap(h)
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
