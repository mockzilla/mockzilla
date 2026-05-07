package portable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPortableConfig(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("empty path returns defaults", func(t *testing.T) {
		cfg, err := loadPortableConfig("", baseDir)
		require.NoError(t, err)
		assert.Nil(t, cfg.App)
		assert.Nil(t, cfg.Services)
	})

	t.Run("full config", func(t *testing.T) {
		content := `
app:
  port: 3000
  title: "Test Mocks"
services:
  petstore:
    latency: 100ms
    errors:
      p10: 400
  spoonacular:
    latency: 200ms
`
		path := filepath.Join(baseDir, "full.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))

		cfg, err := loadPortableConfig(path, baseDir)
		require.NoError(t, err)

		require.NotNil(t, cfg.App)
		assert.Equal(t, 3000, cfg.App.Port)
		assert.Equal(t, "Test Mocks", cfg.App.Title)

		require.Len(t, cfg.Services, 2)

		ps := cfg.Services["petstore"]
		require.NotNil(t, ps)
		assert.Equal(t, "100ms", ps.Latency.String())
		assert.Equal(t, 400, ps.Errors["p10"])
		// WithDefaults should have been called
		assert.NotNil(t, ps.Cache)
		assert.NotNil(t, ps.SpecOptions)

		sp := cfg.Services["spoonacular"]
		require.NotNil(t, sp)
		assert.Equal(t, "200ms", sp.Latency.String())
	})

	t.Run("app only", func(t *testing.T) {
		content := `
app:
  port: 4000
`
		path := filepath.Join(baseDir, "app-only.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))

		cfg, err := loadPortableConfig(path, baseDir)
		require.NoError(t, err)

		require.NotNil(t, cfg.App)
		assert.Equal(t, 4000, cfg.App.Port)
		// Default title should be preserved since we start from defaults
		assert.Equal(t, "API Explorer", cfg.App.Title)
		assert.Nil(t, cfg.Services)
	})

	t.Run("services only", func(t *testing.T) {
		content := `
services:
  petstore:
    latency: 50ms
`
		path := filepath.Join(baseDir, "svc-only.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))

		cfg, err := loadPortableConfig(path, baseDir)
		require.NoError(t, err)

		assert.Nil(t, cfg.App)
		require.Len(t, cfg.Services, 1)
		assert.Equal(t, "50ms", cfg.Services["petstore"].Latency.String())
	})

	t.Run("services with endpoint-level config", func(t *testing.T) {
		content := `
services:
  petstore:
    latency: 0ms
    endpoints:
      /pets/{id}:
        GET:
          latency: 200ms
        POST:
          errors:
            p10: 500
  spoonacular:
    endpoints:
      /recipes/search:
        GET:
          latencies:
            p50: 50ms
            p90: 300ms
`
		path := filepath.Join(baseDir, "endpoints.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))

		cfg, err := loadPortableConfig(path, baseDir)
		require.NoError(t, err)

		require.Len(t, cfg.Services, 2)

		ps := cfg.Services["petstore"]
		require.NotNil(t, ps.Endpoints)
		ep := ps.GetEndpointConfig("/pets/42", "GET")
		require.NotNil(t, ep)
		assert.Equal(t, "200ms", ep.Latency.String())

		epPost := ps.GetEndpointConfig("/pets/42", "POST")
		require.NotNil(t, epPost)
		assert.Equal(t, 500, epPost.Errors["p10"])

		sp := cfg.Services["spoonacular"]
		require.NotNil(t, sp.Endpoints)
		epSearch := sp.GetEndpointConfig("/recipes/search", "GET")
		require.NotNil(t, epSearch)
		assert.Len(t, epSearch.Latencies, 2)
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(baseDir, "empty.yml")
		require.NoError(t, os.WriteFile(path, []byte(""), 0644))

		cfg, err := loadPortableConfig(path, baseDir)
		require.NoError(t, err)
		assert.Nil(t, cfg.App)
		assert.Nil(t, cfg.Services)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := loadPortableConfig("/nonexistent/config.yml", baseDir)
		assert.Error(t, err)
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		path := filepath.Join(baseDir, "invalid.yml")
		require.NoError(t, os.WriteFile(path, []byte("{{invalid"), 0644))

		_, err := loadPortableConfig(path, baseDir)
		assert.Error(t, err)
	})
}

func TestLoadContexts(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty path returns nil", func(t *testing.T) {
		result, err := loadContexts("")
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("per-service contexts", func(t *testing.T) {
		content := `
petstore:
  status:
    - available
    - pending
    - sold
spoonacular:
  cuisineType:
    - Italian
    - Chinese
`
		path := filepath.Join(dir, "contexts.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))

		result, err := loadContexts(path)
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.NotNil(t, result["petstore"])
		assert.NotNil(t, result["spoonacular"])

		// Each entry should be valid YAML bytes
		assert.Contains(t, string(result["petstore"]), "status")
		assert.Contains(t, string(result["spoonacular"]), "cuisineType")
	})

	t.Run("empty context file returns nil", func(t *testing.T) {
		path := filepath.Join(dir, "empty-ctx.yml")
		require.NoError(t, os.WriteFile(path, []byte(""), 0644))

		result, err := loadContexts(path)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := loadContexts("/nonexistent/contexts.yml")
		assert.Error(t, err)
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		path := filepath.Join(dir, "invalid-ctx.yml")
		require.NoError(t, os.WriteFile(path, []byte("{{invalid"), 0644))

		_, err := loadContexts(path)
		assert.Error(t, err)
	})
}
