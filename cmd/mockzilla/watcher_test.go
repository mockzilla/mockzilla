package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

// TestIsInDirectory tests the directory containment logic
func TestIsInDirectory(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		dir      string
		expected bool
	}{
		{
			name:     "file in directory",
			path:     "/Users/test/data/services/foo/service.go",
			dir:      "/Users/test/data/services",
			expected: true,
		},
		{
			name:     "file in subdirectory",
			path:     "/Users/test/data/services/foo/handler/main.go",
			dir:      "/Users/test/data/services",
			expected: true,
		},
		{
			name:     "file in sibling directory",
			path:     "/Users/test/data/openapi/foo/spec.yaml",
			dir:      "/Users/test/data/services",
			expected: false,
		},
		{
			name:     "exact directory match",
			path:     "/Users/test/data/services",
			dir:      "/Users/test/data/services",
			expected: false,
		},
	}

	dw := &dataWatcher{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dw.isInDirectory(tt.path, tt.dir)
			if result != tt.expected {
				t.Errorf("isInDirectory(%q, %q) = %v, want %v", tt.path, tt.dir, result, tt.expected)
			}
		})
	}
}

func TestNewServiceWatcher(t *testing.T) {
	tmpDir := t.TempDir()
	paths := config.NewPaths(tmpDir)

	watcher, err := newDataWatcher(paths)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.stop()

	// Verify directories were created
	if _, err := os.Stat(paths.Services); os.IsNotExist(err) {
		t.Error("Expected services directory to be created")
	}
	if _, err := os.Stat(paths.OpenAPI); os.IsNotExist(err) {
		t.Error("Expected openapi directory to be created")
	}
	if _, err := os.Stat(paths.Static); os.IsNotExist(err) {
		t.Error("Expected static directory to be created")
	}
}

// TestStartStop tests starting and stopping the watcher
func TestStartStop(t *testing.T) {
	tmpDir := t.TempDir()
	paths := config.NewPaths(tmpDir)

	watcher, err := newDataWatcher(paths)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Start watcher
	watcher.start()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Stop watcher
	watcher.stop()

	// Verify timer was cleaned up
	watcher.restartMu.Lock()
	timerNil := watcher.restartTimer == nil
	pending := watcher.pendingRestart
	watcher.restartMu.Unlock()

	if !timerNil {
		t.Error("Expected restart timer to be nil after stop")
	}
	if pending {
		t.Error("Expected no pending restart after stop")
	}
}

// TestHandleServiceChange tests service change handling
func TestHandleServiceChange(t *testing.T) {
	tmpDir := t.TempDir()
	serviceFile := filepath.Join(tmpDir, "service.go")
	if err := os.WriteFile(serviceFile, []byte("package test"), 0644); err != nil {
		t.Fatalf("Failed to create service file: %v", err)
	}

	dw := &dataWatcher{
		registeredServices: make(map[string]bool),
	}

	event := fileEvent{
		Path:  serviceFile,
		Name:  "service.go",
		IsDir: false,
	}

	// Should schedule restart for .go file
	dw.handleServiceChange(event)

	dw.restartMu.Lock()
	scheduled := dw.pendingRestart
	dw.restartMu.Unlock()

	if !scheduled {
		t.Error("Expected restart to be scheduled for .go file")
	}
}

// TestHandleServiceEvent tests event routing
func TestHandleServiceEvent(t *testing.T) {
	tmpDir := t.TempDir()
	paths := config.NewPaths(tmpDir)

	dw := &dataWatcher{
		paths:              paths,
		registeredServices: make(map[string]bool),
		restartDebounce:    10 * time.Millisecond, // Short debounce for testing
	}

	operations := []fsnotify.Op{
		fsnotify.Create,
		fsnotify.Write,
		fsnotify.Remove,
		fsnotify.Rename,
		fsnotify.Chmod,
	}

	for _, op := range operations {
		event := fileEvent{
			Path:      "/tmp/service.go",
			Name:      "service.go",
			IsDir:     false,
			Operation: op,
		}

		// Should not panic
		dw.handleServiceEvent(event)
	}

	// Wait for any pending timers to complete and clean them up
	time.Sleep(50 * time.Millisecond)

	// Clean up any pending timers manually
	dw.restartMu.Lock()
	if dw.restartTimer != nil {
		dw.restartTimer.Stop()
		dw.restartTimer = nil
	}
	dw.pendingRestart = false
	dw.restartMu.Unlock()
}

func TestGetServiceName(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		baseDir     string
		useFilename bool
		expected    string
	}{
		{
			name:        "file directly in baseDir with useFilename",
			path:        "/data/openapi/petstore.yaml",
			baseDir:     "/data/openapi",
			useFilename: true,
			expected:    "petstore",
		},
		{
			name:        "file directly in baseDir without useFilename",
			path:        "/data/static/myservice",
			baseDir:     "/data/static",
			useFilename: false,
			expected:    "myservice",
		},
		{
			name:        "file in subdirectory",
			path:        "/data/openapi/petstore/spec.yaml",
			baseDir:     "/data/openapi",
			useFilename: true,
			expected:    "petstore",
		},
		{
			name:        "file with extension gets normalized",
			path:        "/data/openapi/my-api.v2.yaml",
			baseDir:     "/data/openapi",
			useFilename: true,
			expected:    "my_api_v2",
		},
		{
			name:        "path equals baseDir",
			path:        "/data/openapi",
			baseDir:     "/data/openapi",
			useFilename: true,
			expected:    "",
		},
		{
			name:        "path outside baseDir",
			path:        "/other/path/file.yaml",
			baseDir:     "/data/openapi",
			useFilename: true,
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getServiceName(tt.path, tt.baseDir, tt.useFilename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSpecFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{"yaml extension", "spec.yaml", true},
		{"yml extension", "spec.yml", true},
		{"json extension", "spec.json", true},
		{"go file", "main.go", false},
		{"txt file", "readme.txt", false},
		{"no extension", "Makefile", false},
		{"yaml in middle", "spec.yaml.bak", false},
		{"uppercase YAML", "spec.YAML", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSpecFile(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDispatchEvent(t *testing.T) {
	tests := []struct {
		name         string
		operation    fsnotify.Op
		expectCreate bool
		expectUpdate bool
		expectDelete bool
	}{
		{"create event", fsnotify.Create, true, false, false},
		{"write event", fsnotify.Write, false, true, false},
		{"remove event", fsnotify.Remove, false, false, true},
		{"rename event", fsnotify.Rename, false, false, true},
		{"chmod event", fsnotify.Chmod, false, false, false},
		{"write|chmod combined", fsnotify.Write | fsnotify.Chmod, false, true, false},
		{"create|write combined", fsnotify.Create | fsnotify.Write, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var createCalled, updateCalled, deleteCalled bool

			handler := eventHandler{
				onCreate: func(e fileEvent) { createCalled = true },
				onUpdate: func(e fileEvent) { updateCalled = true },
				onDelete: func(e fileEvent) { deleteCalled = true },
			}

			event := fileEvent{
				Path:      "/test/file.go",
				Name:      "file.go",
				Operation: tt.operation,
			}

			dispatchEvent(event, handler)

			assert.Equal(t, tt.expectCreate, createCalled, "onCreate")
			assert.Equal(t, tt.expectUpdate, updateCalled, "onUpdate")
			assert.Equal(t, tt.expectDelete, deleteCalled, "onDelete")
		})
	}
}

func TestSetReloadCallback(t *testing.T) {
	dw := &dataWatcher{}

	called := false
	callback := func() error {
		called = true
		return nil
	}

	dw.setReloadCallback(callback)

	assert.NotNil(t, dw.onReload)
	_ = dw.onReload()
	assert.True(t, called)
}

func TestCreateFileEvent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.yaml")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	assert.NoError(t, err)

	// Create a test directory
	testDir := filepath.Join(tmpDir, "testdir")
	err = os.Mkdir(testDir, 0755)
	assert.NoError(t, err)

	dw := &dataWatcher{}

	t.Run("file event", func(t *testing.T) {
		fsEvent := fsnotify.Event{
			Name: testFile,
			Op:   fsnotify.Create,
		}

		event := dw.createFileEvent(fsEvent)

		assert.Equal(t, testFile, event.Path)
		assert.Equal(t, "test.yaml", event.Name)
		assert.False(t, event.IsDir)
		assert.Equal(t, fsnotify.Create, event.Operation)
	})

	t.Run("directory event", func(t *testing.T) {
		fsEvent := fsnotify.Event{
			Name: testDir,
			Op:   fsnotify.Create,
		}

		event := dw.createFileEvent(fsEvent)

		assert.Equal(t, testDir, event.Path)
		assert.Equal(t, "testdir", event.Name)
		assert.True(t, event.IsDir)
	})

	t.Run("deleted file event", func(t *testing.T) {
		fsEvent := fsnotify.Event{
			Name: filepath.Join(tmpDir, "nonexistent.yaml"),
			Op:   fsnotify.Remove,
		}

		event := dw.createFileEvent(fsEvent)

		assert.Equal(t, "nonexistent.yaml", event.Name)
		assert.False(t, event.IsDir)
	})
}

func TestRouteEvent(t *testing.T) {
	tmpDir := t.TempDir()
	paths := config.NewPaths(tmpDir)

	dw := &dataWatcher{
		paths:              paths,
		registeredServices: make(map[string]bool),
	}

	// Create directories
	for _, dir := range []string{paths.Services, paths.OpenAPI, paths.Static} {
		err := os.MkdirAll(dir, 0755)
		assert.NoError(t, err)
	}

	tests := []struct {
		name        string
		eventPath   string
		shouldRoute bool
	}{
		{
			name:        "service file",
			eventPath:   filepath.Join(paths.Services, "myservice", "service.go"),
			shouldRoute: true,
		},
		{
			name:        "openapi spec",
			eventPath:   filepath.Join(paths.OpenAPI, "petstore.yaml"),
			shouldRoute: true,
		},
		{
			name:        "static file",
			eventPath:   filepath.Join(paths.Static, "myservice", "data.json"),
			shouldRoute: true,
		},
		{
			name:        "unrelated path",
			eventPath:   filepath.Join(tmpDir, "other", "file.txt"),
			shouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create parent directory for the event path
			parentDir := filepath.Dir(tt.eventPath)
			_ = os.MkdirAll(parentDir, 0755)

			fsEvent := fsnotify.Event{
				Name: tt.eventPath,
				Op:   fsnotify.Create,
			}

			// Should not panic
			dw.routeEvent(fsEvent)
		})
	}
}
