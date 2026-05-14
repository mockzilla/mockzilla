package integrationtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// CacheFileName is the name of the cache file
	CacheFileName = ".integration-cache.json"

	// specBaseDir is the base directory for spec files
	specBaseDir = "testdata/specs"
)

// NormalizeSpecPath normalizes a spec path to a canonical absolute path.
// This ensures consistent cache keys regardless of how the spec path was provided
// (e.g., "3.0/aws/foo.yml" vs "testdata/specs/3.0/aws/foo.yml" vs absolute path).
func NormalizeSpecPath(specPath string) string {
	// Try as absolute path first
	if filepath.IsAbs(specPath) {
		return filepath.Clean(specPath)
	}

	// Try the path as-is
	if absPath, err := filepath.Abs(specPath); err == nil {
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	// Try with testdata/specs prefix
	withPrefix := filepath.Join(specBaseDir, specPath)
	if absPath, err := filepath.Abs(withPrefix); err == nil {
		if _, err := os.Stat(absPath); err == nil {
			return absPath
		}
	}

	// Fallback: return cleaned absolute path of original (even if file doesn't exist)
	absPath, _ := filepath.Abs(specPath)
	return filepath.Clean(absPath)
}

// CacheEntry represents a cached test result
type CacheEntry struct {
	Passed   bool      `json:"passed"`
	TestedAt time.Time `json:"tested_at"`
}

// ResultCache manages cached test results
type ResultCache struct {
	Entries map[string]CacheEntry `json:"entries"` // key is spec path
	mu      sync.RWMutex
	path    string
}

// NewResultCache creates or loads the codegen integration cache from
// the given directory using the default cache file name.
func NewResultCache(cacheDir string) (*ResultCache, error) {
	return NewResultCacheNamed(cacheDir, CacheFileName)
}

// NewResultCacheNamed creates or loads a cache from the given directory
// under the supplied file name. Use a distinct file name per test suite
// (codegen vs portable) so passing results from one don't mask failures
// in the other - the two suites exercise different code paths and a
// spec can pass one while failing the other.
func NewResultCacheNamed(cacheDir, fileName string) (*ResultCache, error) {
	cachePath := filepath.Join(cacheDir, fileName)
	cache := &ResultCache{
		Entries: make(map[string]CacheEntry),
		path:    cachePath,
	}

	data, err := os.ReadFile(cachePath)
	if err == nil {
		if err := json.Unmarshal(data, cache); err != nil {
			// Corrupted cache, start fresh
			cache.Entries = make(map[string]CacheEntry)
		}
	}

	return cache, nil
}

// IsCached checks if a spec has a valid cached passing result
func (c *ResultCache) IsCached(specPath string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedPath := NormalizeSpecPath(specPath)
	entry, ok := c.Entries[normalizedPath]
	return ok && entry.Passed
}

// MarkPassed marks a spec as passing
func (c *ResultCache) MarkPassed(specPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedPath := NormalizeSpecPath(specPath)
	c.Entries[normalizedPath] = CacheEntry{
		Passed:   true,
		TestedAt: time.Now(),
	}
}

// MarkFailed removes a spec from the cache (so it will be retested)
func (c *ResultCache) MarkFailed(specPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedPath := NormalizeSpecPath(specPath)
	delete(c.Entries, normalizedPath)
}

// Save persists the cache to disk
func (c *ResultCache) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.path, data, 0644)
}

// Clear removes all cached entries
func (c *ResultCache) Clear() error {
	c.mu.Lock()
	c.Entries = make(map[string]CacheEntry)
	c.mu.Unlock()

	// Remove the cache file
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Size returns the number of cached entries
func (c *ResultCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.Entries)
}

// FilterUncached returns only specs that are not cached as passing
func (c *ResultCache) FilterUncached(specs []string) []string {
	var uncached []string
	for _, spec := range specs {
		if !c.IsCached(spec) {
			uncached = append(uncached, spec)
		}
	}
	return uncached
}
