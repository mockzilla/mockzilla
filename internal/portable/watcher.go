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

// watchSpecs watches spec files for changes, hot-swaps existing handlers
// and registers new services when new spec files appear.
func watchSpecs(
	specs []string,
	router *api.Router,
	cfg *portableConfig,
	contexts map[string][]byte,
	handlers map[string]*swappableHandler,
) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("Failed to create file watcher", "error", err)
		return
	}
	defer func() { _ = watcher.Close() }()

	// Watch spec files and their parent directories (for new files)
	dirs := make(map[string]bool)
	for _, spec := range specs {
		dir := filepath.Dir(spec)
		if !dirs[dir] {
			dirs[dir] = true
			if err := watcher.Add(dir); err != nil {
				slog.Error("Failed to watch directory", "dir", dir, "error", err)
			}
		}
	}

	// Debounce timer
	var debounceTimer *time.Timer
	pendingPaths := make(map[string]bool)
	var mu sync.Mutex

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !isSpecFile(event.Name) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			mu.Lock()
			pendingPaths[event.Name] = true
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(2*time.Second, func() {
				mu.Lock()
				paths := make([]string, 0, len(pendingPaths))
				for p := range pendingPaths {
					paths = append(paths, p)
				}
				pendingPaths = make(map[string]bool)
				mu.Unlock()

				for _, path := range paths {
					reloadSpec(path, router, cfg, contexts, handlers)
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

// reloadSpec reloads a single spec file and hot-swaps the handler.
// If the spec is new (not yet registered), it registers a new service.
func reloadSpec(
	specPath string,
	router *api.Router,
	cfg *portableConfig,
	contexts map[string][]byte,
	handlers map[string]*swappableHandler,
) {
	name := api.NormalizeServiceName(specPath)
	ctxBytes := contexts[name]

	// Existing service - hot-swap the handler
	if sw, ok := handlers[name]; ok {
		h, err := buildHandler(specPath, ctxBytes)
		if err != nil {
			slog.Error("Failed to reload spec", "path", specPath, "error", err)
			return
		}
		sw.swap(h)
		slog.Info("Reloaded spec", "service", name, "path", specPath)
		return
	}

	// New service - register it
	svcCfg := cfg.Services[name]
	if err := registerService(router, specPath, svcCfg, ctxBytes, handlers); err != nil {
		slog.Error("Failed to register new spec", "path", specPath, "error", err)
		return
	}
	slog.Info("Registered new service", "service", name, "path", specPath)
}

// buildHandler creates a handler from a spec file path.
func buildHandler(specPath string, contextBytes []byte) (*handler, error) {
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	var opts []factory.FactoryOption
	if contextBytes != nil {
		opts = append(opts, factory.WithServiceContext(contextBytes))
	}
	opts = append(opts, factory.WithSpecOptions(&config.SpecOptions{LazyLoad: true}))

	return newHandler(specBytes, opts...)
}
