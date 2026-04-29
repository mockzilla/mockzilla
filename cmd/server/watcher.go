package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

type fileEvent struct {
	Path      string
	Name      string
	IsDir     bool
	Operation fsnotify.Op
}

type eventHandler struct {
	onCreate func(fileEvent)
	onUpdate func(fileEvent)
	onDelete func(fileEvent)
}

type dataWatcher struct {
	paths config.Paths

	watcher  *fsnotify.Watcher
	stopChan chan struct{}

	// Callback to rebuild/reload server
	onReload func() error

	// Protects registeredServices map from concurrent access
	mu sync.RWMutex

	// Track services we've already seen to avoid duplicate rebuilds
	registeredServices map[string]bool

	// Debouncing for restart
	restartTimer    *time.Timer
	restartMu       sync.Mutex
	restartDebounce time.Duration
	pendingRestart  bool
}

func newDataWatcher(paths config.Paths) (*dataWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	sw := &dataWatcher{
		paths:              paths,
		watcher:            watcher,
		stopChan:           make(chan struct{}),
		registeredServices: make(map[string]bool),

		// Wait 2 seconds of quiet before restarting
		restartDebounce: 2 * time.Second,
	}

	// Scan existing services to avoid duplicate rebuilds
	sw.scanExistingServices()

	// Watch the three main directories
	dirs := []string{paths.Services, paths.OpenAPI, paths.Static}
	for _, dir := range dirs {
		// Create directory if it doesn't exist
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Warn("Failed to create directory", "dir", dir, "error", err)
			continue
		}

		// Add to watcher
		if err := watcher.Add(dir); err != nil {
			slog.Warn("Failed to watch directory", "dir", dir, "error", err)
		}

		// Watch existing subdirectories
		sw.watchSubdirectories(dir)
	}

	return sw, nil
}

func (dw *dataWatcher) start() {
	go dw.watch()
}

func (dw *dataWatcher) stop() {
	dw.restartMu.Lock()
	if dw.restartTimer != nil {
		dw.restartTimer.Stop()
		dw.restartTimer = nil
	}
	dw.pendingRestart = false
	dw.restartMu.Unlock()

	close(dw.stopChan)
	defer func() { _ = dw.watcher.Close() }()
}

func (dw *dataWatcher) setReloadCallback(callback func() error) {
	dw.onReload = callback
}

func (dw *dataWatcher) processExistingFiles() error {
	var generated int

	openapiGenerated, err := dw.processExistingOpenAPI()
	if err != nil {
		return fmt.Errorf("processing existing OpenAPI specs: %w", err)
	}
	generated += openapiGenerated

	staticGenerated, err := dw.processExistingStatic()
	if err != nil {
		return fmt.Errorf("processing existing static directories: %w", err)
	}
	generated += staticGenerated

	if generated > 0 {
		slog.Info("Processed existing files", "services", generated)

		if err := runGenDiscover(); err != nil {
			return fmt.Errorf("running gen-discover: %w", err)
		}

		if err := dw.rebuildServer(); err != nil {
			return fmt.Errorf("rebuilding server: %w", err)
		}

		slog.Info("Triggering restart to load new services")
		if dw.onReload != nil {
			_ = dw.onReload()
		}
	}

	return nil
}

func (dw *dataWatcher) watchSubdirectories(dir string) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		baseName := filepath.Base(path)
		if baseName == "setup" || baseName == "handler" || baseName == "types" {
			return filepath.SkipDir
		}

		if info.IsDir() {
			if err := dw.watcher.Add(path); err != nil {
				slog.Warn("Failed to watch subdirectory", "dir", path, "error", err)
			}
		}

		return nil
	})

	if err != nil {
		slog.Warn("Failed to walk directory tree", "dir", dir, "error", err)
	}
}

func (dw *dataWatcher) watch() {
	for {
		select {
		case <-dw.stopChan:
			return
		case event, ok := <-dw.watcher.Events:
			if !ok {
				return
			}

			// Route event to appropriate handler
			dw.routeEvent(event)

		case err, ok := <-dw.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("File watcher error", "error", err)
		}
	}
}

// routeEvent routes fsnotify events to the appropriate handler
func (dw *dataWatcher) routeEvent(event fsnotify.Event) {
	// Check if it's a new directory being created
	if event.Op&fsnotify.Create == fsnotify.Create {
		if fileInfo, err := os.Stat(event.Name); err == nil && fileInfo.IsDir() {
			// Add new directory to watcher
			_ = dw.watcher.Add(event.Name)
			slog.Info("Watching new directory", "dir", event.Name)
		}
	}

	// Create fileEvent with context
	fileEvent := dw.createFileEvent(event)

	// Determine which directory this event belongs to
	// Check in order: services, openapi, static
	inService := dw.isInDirectory(event.Name, dw.paths.Services)
	inOpenAPI := dw.isInDirectory(event.Name, dw.paths.OpenAPI)
	inStatic := dw.isInDirectory(event.Name, dw.paths.Static)

	slog.Debug("Event routing",
		"path", event.Name,
		"op", event.Op.String(),
		"inService", inService,
		"inOpenAPI", inOpenAPI,
		"inStatic", inStatic)

	switch {
	case inService:
		dw.handleServiceEvent(fileEvent)

	case inOpenAPI:
		dw.handleOpenAPIEvent(fileEvent)

	case inStatic:
		dw.handleStaticEvent(fileEvent)
	}
}

// createFileEvent creates a fileEvent from fsnotify.Event
func (dw *dataWatcher) createFileEvent(event fsnotify.Event) fileEvent {
	fileInfo, err := os.Stat(event.Name)
	isDir := err == nil && fileInfo.IsDir()

	return fileEvent{
		Path:      event.Name,
		Name:      filepath.Base(event.Name),
		IsDir:     isDir,
		Operation: event.Op,
	}
}

// isInDirectory checks if a path is within a directory
func (dw *dataWatcher) isInDirectory(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return !filepath.IsAbs(rel) && !strings.HasPrefix(rel, "..") && rel != "."
}

func (dw *dataWatcher) handleServiceEvent(event fileEvent) {
	dispatchEvent(event, eventHandler{
		onCreate: dw.onServiceCreate,
		onUpdate: dw.onServiceUpdate,
		onDelete: dw.onServiceDelete,
	})
}

func (dw *dataWatcher) onServiceCreate(event fileEvent) {
	slog.Info("Service created", "path", event.Path, "isDir", event.IsDir)

	// Only process service config files (setup/config.yml)
	if event.IsDir || event.Name != cmdapi.ServiceConfigFile {
		return
	}

	// Extract service name from path (config.yml is in setup/, service is parent of setup/)
	setupDir := filepath.Dir(event.Path)
	if filepath.Base(setupDir) != "setup" {
		return
	}
	serviceDir := filepath.Dir(setupDir)
	serviceName := filepath.Base(serviceDir)

	// Check if we've already registered this service
	dw.mu.RLock()
	alreadyRegistered := dw.registeredServices[serviceName]
	dw.mu.RUnlock()

	if alreadyRegistered {
		slog.Debug("Service already registered, skipping", "service", serviceName)
		return
	}

	slog.Info("New service detected", "service", serviceName)

	// Mark as registered
	dw.mu.Lock()
	dw.registeredServices[serviceName] = true
	dw.mu.Unlock()

	// Run gen-discover
	if err := runGenDiscover(); err != nil {
		slog.Error("Failed to run gen-discover", "error", err)
		return
	}

	// Rebuild server
	if err := dw.rebuildServer(); err != nil {
		slog.Error("Failed to rebuild server", "error", err)
		return
	}

	// Trigger restart
	slog.Info("Service registered, triggering restart", "service", serviceName)
	if dw.onReload != nil {
		_ = dw.onReload()
	}
}

func (dw *dataWatcher) onServiceUpdate(event fileEvent) {
	slog.Info("Service updated", "path", event.Path, "isDir", event.IsDir)
	dw.handleServiceChange(event)
}

func (dw *dataWatcher) onServiceDelete(event fileEvent) {
	slog.Info("Service deleted", "path", event.Path, "isDir", event.IsDir)
	dw.handleServiceChange(event)
}

// handleServiceChange handles both update and delete events
// by scheduling a debounced rebuild and restart
func (dw *dataWatcher) handleServiceChange(event fileEvent) {
	// If it's a directory change, ignore it
	if event.IsDir {
		slog.Debug("Ignoring directory change", "path", event.Path)
		return
	}

	// Only handle .go files
	if !strings.HasSuffix(event.Path, ".go") {
		return
	}

	// Schedule a debounced restart
	dw.scheduleRestart()
}

// scheduleRestart schedules a debounced restart
// Multiple rapid file changes will only trigger one restart after the quiet period
func (dw *dataWatcher) scheduleRestart() {
	dw.restartMu.Lock()
	defer dw.restartMu.Unlock()

	// Mark that we have a pending restart
	dw.pendingRestart = true

	// Cancel existing timer if any
	if dw.restartTimer != nil {
		dw.restartTimer.Stop()
	}

	// Create new timer
	dw.restartTimer = time.AfterFunc(dw.restartDebounce, func() {
		dw.restartMu.Lock()
		defer dw.restartMu.Unlock()

		if !dw.pendingRestart {
			return
		}

		dw.pendingRestart = false

		slog.Info("Debounce period elapsed, rebuilding and restarting server...")

		// Rebuild server
		if err := dw.rebuildServer(); err != nil {
			slog.Error("Failed to rebuild server", "error", err)
			return
		}

		// Trigger restart
		if dw.onReload != nil {
			_ = dw.onReload()
		}
	})

	slog.Info("Restart scheduled", "debounce", dw.restartDebounce)
}

func (dw *dataWatcher) handleOpenAPIEvent(event fileEvent) {
	dispatchEvent(event, eventHandler{
		onCreate: dw.onOpenAPICreate,
		onUpdate: dw.onOpenAPIUpdate,
		onDelete: dw.onOpenAPIDelete,
	})
}

func (dw *dataWatcher) onOpenAPICreate(event fileEvent) {
	slog.Info("OpenAPI spec create event", "path", event.Path, "isDir", event.IsDir)

	if event.IsDir {
		return
	}

	serviceName := dw.getOpenAPIServiceName(event.Path)
	if serviceName == "" {
		return
	}

	slog.Info("Converting OpenAPI spec", "path", event.Path, "service", serviceName)

	serviceDir := filepath.Join(dw.paths.Services, serviceName)
	sourceDir := dw.getOpenAPISourceDir(event.Path)
	configPath := findConfigFile(sourceDir)

	if err := runGenService(event.Path, serviceDir, configPath); err != nil {
		slog.Error("Setup failed", "error", err)
		return
	}

	slog.Info("Successfully generated service from OpenAPI spec", "service", serviceName)

	// Trigger service registration
	dw.triggerServiceCreate(serviceName)
}

// removeGeneratedService removes a generated service directory and unregisters it.
func (dw *dataWatcher) removeGeneratedService(serviceName string) {
	generatedServiceDir := filepath.Join(dw.paths.Services, serviceName)
	if err := os.RemoveAll(generatedServiceDir); err != nil {
		slog.Error("Failed to remove generated service", "service", serviceName, "error", err)
	}

	dw.mu.Lock()
	delete(dw.registeredServices, serviceName)
	dw.mu.Unlock()
}

func (dw *dataWatcher) onOpenAPIUpdate(event fileEvent) {
	slog.Info("OpenAPI spec updated", "path", event.Path, "isDir", event.IsDir)

	if event.IsDir {
		return
	}

	serviceName := dw.getOpenAPIServiceName(event.Path)
	if serviceName == "" {
		return
	}

	dw.removeGeneratedService(serviceName)
	dw.onOpenAPICreate(event)
}

func (dw *dataWatcher) onOpenAPIDelete(event fileEvent) {
	slog.Info("OpenAPI spec deleted", "path", event.Path)

	serviceName := dw.getOpenAPIServiceName(event.Path)
	if serviceName == "" {
		return
	}

	slog.Info("Removing generated service", "service", serviceName)
	dw.removeGeneratedService(serviceName)
	dw.scheduleRestart()
}

func (dw *dataWatcher) getOpenAPIServiceName(path string) string {
	return getServiceName(path, dw.paths.OpenAPI, true)
}

// getOpenAPISourceDir returns the directory containing the OpenAPI spec.
// For flat structure (openapi/petstore.yml), returns specDir.
// For nested structure (openapi/petstore/openapi.yml), returns the service directory.
func (dw *dataWatcher) getOpenAPISourceDir(path string) string {
	relPath, err := filepath.Rel(dw.paths.OpenAPI, path)
	if err != nil {
		return dw.paths.OpenAPI
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) > 1 {
		// Nested structure: openapi/petstore/openapi.yml
		return filepath.Join(dw.paths.OpenAPI, parts[0])
	}

	// Flat structure: openapi/petstore.yml
	return dw.paths.OpenAPI
}

func (dw *dataWatcher) handleStaticEvent(event fileEvent) {
	dispatchEvent(event, eventHandler{
		onCreate: dw.onStaticCreate,
		onUpdate: dw.onStaticUpdate,
		onDelete: dw.onStaticDelete,
	})
}

func (dw *dataWatcher) onStaticCreateOrUpdate(event fileEvent) {
	slog.Info("Static file changed", "path", event.Path, "op", event.Operation)

	serviceName := dw.getStaticServiceName(event.Path)
	if serviceName == "" {
		return
	}

	dw.regenerateStaticService(serviceName)
}

func (dw *dataWatcher) onStaticCreate(event fileEvent) { dw.onStaticCreateOrUpdate(event) }
func (dw *dataWatcher) onStaticUpdate(event fileEvent) { dw.onStaticCreateOrUpdate(event) }

func (dw *dataWatcher) onStaticDelete(event fileEvent) {
	slog.Info("Static file deleted", "path", event.Path)

	serviceName := dw.getStaticServiceName(event.Path)
	if serviceName == "" {
		return
	}

	// Check if the service directory still has files
	staticServiceDir := filepath.Join(dw.paths.Static, serviceName)
	entries, err := os.ReadDir(staticServiceDir)
	if err != nil || len(entries) == 0 {
		slog.Info("Static service directory empty, removing generated service", "service", serviceName)
		dw.removeGeneratedService(serviceName)
		dw.scheduleRestart()
		return
	}

	dw.regenerateStaticService(serviceName)
}

func (dw *dataWatcher) getStaticServiceName(path string) string {
	return getServiceName(path, dw.paths.Static, false)
}

// findConfigFile looks for config.yml in the given directory.
// Returns the path if found, empty string otherwise.
func findConfigFile(dir string) string {
	configPath := filepath.Join(dir, "config.yml")
	if _, err := os.Stat(configPath); err == nil {
		return configPath
	}
	return ""
}

// findSpecFile looks for openapi.yml, openapi.yaml, or openapi.json in the given directory.
// Returns the path if found, empty string otherwise.
func findSpecFile(dir string) string {
	for _, name := range []string{"openapi.yml", "openapi.yaml", "openapi.json"} {
		specPath := filepath.Join(dir, name)
		if _, err := os.Stat(specPath); err == nil {
			return specPath
		}
	}
	return ""
}

// triggerServiceCreate triggers the service registration flow for a service.
func (dw *dataWatcher) triggerServiceCreate(serviceName string) {
	serviceDir := filepath.Join(dw.paths.Services, serviceName)
	configPath := filepath.Join(serviceDir, "setup", cmdapi.ServiceConfigFile)
	dw.onServiceCreate(fileEvent{
		Path:      configPath,
		Name:      cmdapi.ServiceConfigFile,
		IsDir:     false,
		Operation: fsnotify.Create,
	})
}

// regenerateStaticService regenerates a service from static files
func (dw *dataWatcher) regenerateStaticService(serviceName string) {
	staticPath := filepath.Join(dw.paths.Static, serviceName)
	serviceDir := filepath.Join(dw.paths.Services, serviceName)
	configPath := findConfigFile(staticPath)

	slog.Info("Regenerating static service", "service", serviceName, "path", staticPath, "config", configPath)

	// Remove existing service directory to ensure clean regeneration
	if err := os.RemoveAll(serviceDir); err != nil {
		slog.Error("Failed to remove existing service directory", "service", serviceName, "error", err)
		return
	}

	if err := runGenService(staticPath, serviceDir, configPath); err != nil {
		slog.Error("Failed to regenerate static service", "service", serviceName, "error", err)
		return
	}

	dw.mu.Lock()
	dw.registeredServices[serviceName] = true
	dw.mu.Unlock()

	dw.triggerServiceCreate(serviceName)
}

// scanExistingServices scans for existing services and marks them as registered
// This prevents duplicate rebuilds when the watcher starts
func (dw *dataWatcher) scanExistingServices() {
	entries, err := os.ReadDir(dw.paths.Services)
	if err != nil {
		slog.Debug("No existing services directory", "error", err)
		return
	}

	dw.mu.Lock()
	defer dw.mu.Unlock()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		serviceName := entry.Name()
		configFile := filepath.Join(dw.paths.Services, serviceName, "setup", cmdapi.ServiceConfigFile)

		// Check if service config file exists
		if _, err := os.Stat(configFile); err == nil {
			dw.registeredServices[serviceName] = true
			slog.Debug("Marked existing service as registered", "service", serviceName)
		}
	}
}

// processExistingDir scans a directory and generates services from existing entries.
// If processFiles is true, processes files (for OpenAPI specs); otherwise processes directories (for static).
func (dw *dataWatcher) processExistingDir(dir string, processFiles bool, logType string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading %s directory: %w", logType, err)
	}

	var generated int
	for _, entry := range entries {
		// Filter: files for OpenAPI, directories for static
		if processFiles {
			if entry.IsDir() || !isSpecFile(entry.Name()) {
				continue
			}
		} else {
			if !entry.IsDir() {
				continue
			}
		}

		serviceName := api.NormalizeServiceName(entry.Name())

		dw.mu.RLock()
		alreadyRegistered := dw.registeredServices[serviceName]
		dw.mu.RUnlock()

		if alreadyRegistered {
			slog.Debug("Service already exists, skipping", "service", serviceName)
			continue
		}

		sourcePath := filepath.Join(dir, entry.Name())
		slog.Info("Generating service from existing "+logType, "path", sourcePath, "service", serviceName)

		// For directories (static), look for config in the source directory
		// For files (OpenAPI), look for config in the parent directory if it's a nested structure
		var configPath string
		if entry.IsDir() {
			configPath = findConfigFile(sourcePath)
		} else {
			// Check if there's a directory with the same name as the spec (without extension)
			// e.g., openapi/petstore/openapi.yml -> look in openapi/petstore/
			specDir := filepath.Join(dir, serviceName)
			if info, err := os.Stat(specDir); err == nil && info.IsDir() {
				configPath = findConfigFile(specDir)
			}
		}

		serviceDir := filepath.Join(dw.paths.Services, serviceName)
		if err := runGenService(sourcePath, serviceDir, configPath); err != nil {
			slog.Error("Failed to generate service from "+logType, "path", sourcePath, "error", err)
			continue
		}

		dw.mu.Lock()
		dw.registeredServices[serviceName] = true
		dw.mu.Unlock()

		generated++
	}

	return generated, nil
}

func (dw *dataWatcher) processExistingOpenAPI() (int, error) {
	entries, err := os.ReadDir(dw.paths.OpenAPI)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading OpenAPI directory: %w", err)
	}

	var generated int
	for _, entry := range entries {
		var serviceName, specPath, configPath string

		if entry.IsDir() {
			// Nested structure: openapi/petstore/openapi.yml
			serviceName = api.NormalizeServiceName(entry.Name())
			serviceDir := filepath.Join(dw.paths.OpenAPI, entry.Name())

			// Look for openapi.yml or openapi.json in the directory
			specPath = findSpecFile(serviceDir)
			if specPath == "" {
				continue // No spec file found in directory
			}
			configPath = findConfigFile(serviceDir)
		} else if isSpecFile(entry.Name()) {
			// Flat structure: openapi/petstore.yml
			serviceName = api.NormalizeServiceName(entry.Name())
			specPath = filepath.Join(dw.paths.OpenAPI, entry.Name())
			// No config for flat structure (config would need to be in a directory)
		} else {
			continue
		}

		dw.mu.RLock()
		alreadyRegistered := dw.registeredServices[serviceName]
		dw.mu.RUnlock()

		if alreadyRegistered {
			slog.Debug("Service already exists, skipping", "service", serviceName)
			continue
		}

		slog.Info("Generating service from existing OpenAPI spec", "path", specPath, "service", serviceName)

		serviceDir := filepath.Join(dw.paths.Services, serviceName)
		if err := runGenService(specPath, serviceDir, configPath); err != nil {
			slog.Error("Failed to generate service from OpenAPI spec", "path", specPath, "error", err)
			continue
		}

		dw.mu.Lock()
		dw.registeredServices[serviceName] = true
		dw.mu.Unlock()

		generated++
	}

	return generated, nil
}

func (dw *dataWatcher) processExistingStatic() (int, error) {
	return dw.processExistingDir(dw.paths.Static, false, "static files")
}

// rebuildServer rebuilds the server binary with the new services
func (dw *dataWatcher) rebuildServer() error {
	slog.Info("Running go mod tidy...")
	if err := runCmd(dw.paths.Base, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	slog.Info("Running go mod vendor...")
	if err := runCmd(dw.paths.Base, "go", "mod", "vendor"); err != nil {
		return fmt.Errorf("go mod vendor failed: %w", err)
	}

	buildDir := filepath.Join(dw.paths.Base, ".build", "server")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}

	serverBinary := filepath.Join(buildDir, "server")
	slog.Info("Building server binary...", "output", serverBinary)
	if err := runCmd(dw.paths.Base, "go", "build", "-o", serverBinary, "./cmd/server"); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	slog.Info("Server binary rebuilt successfully")
	return nil
}

// runGenService creates service setup structure and generates types, handlers, and service files.
// specPath: path to the OpenAPI spec file or static files directory
// serviceDir: target service directory (e.g., "resources/data/services/petstore")
// configPath: optional path to config.yml (empty string if not provided)
func runGenService(specPath, serviceDir, configPath string) error {
	serviceName := filepath.Base(serviceDir)

	slog.Info("Generating service", "service", serviceName, "spec", specPath, "output", serviceDir, "config", configPath)

	if err := cmdapi.GenerateService(cmdapi.ServiceOptions{
		Name:              serviceName,
		SpecPath:          specPath,
		OutputDir:         serviceDir,
		ServiceConfigPath: configPath,
	}); err != nil {
		return fmt.Errorf("generating service failed: %w", err)
	}

	slog.Info("Running go generate in service directory", "dir", serviceDir)
	if err := runCmd(serviceDir, "go", "generate", "./..."); err != nil {
		return fmt.Errorf("go generate failed: %w", err)
	}

	return nil
}

// runGenDiscover runs the discover command to update services_gen.go
func runGenDiscover() error {
	slog.Info("Running discover...")

	if err := cmdapi.Discover(cmdapi.DiscoverOptions{}); err != nil {
		return fmt.Errorf("discover failed: %w", err)
	}

	return nil
}

func dispatchEvent(event fileEvent, h eventHandler) {
	op := event.Operation
	switch {
	case op&fsnotify.Create == fsnotify.Create:
		h.onCreate(event)
	case op&fsnotify.Write == fsnotify.Write:
		h.onUpdate(event)
	case op&fsnotify.Remove == fsnotify.Remove, op&fsnotify.Rename == fsnotify.Rename:
		h.onDelete(event)
	}
}

func getServiceName(path, baseDir string, useFilename bool) string {
	relPath, err := filepath.Rel(baseDir, path)
	if err != nil {
		return ""
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "." {
		return ""
	}

	if useFilename && len(parts) == 1 {
		return api.NormalizeServiceName(filepath.Base(path))
	}

	return api.NormalizeServiceName(parts[0])
}

func isSpecFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".json")
}

func runCmd(dir string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
