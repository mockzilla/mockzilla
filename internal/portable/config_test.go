package portable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAppConfig(t *testing.T) {
	baseDir := t.TempDir()

	t.Run("empty root returns defaults", func(t *testing.T) {
		cfg, err := loadAppConfig("", baseDir)
		require.NoError(t, err)
		assert.Equal(t, "API Explorer", cfg.Title)
		assert.Equal(t, 2200, cfg.Port)
	})

	t.Run("missing app.yml returns defaults", func(t *testing.T) {
		cfg, err := loadAppConfig(t.TempDir(), baseDir)
		require.NoError(t, err)
		assert.Equal(t, "API Explorer", cfg.Title)
	})

	t.Run("reads global app settings", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yml"),
			[]byte("title: Demo\nport: 3030\n"), 0o644))

		cfg, err := loadAppConfig(dir, baseDir)
		require.NoError(t, err)
		assert.Equal(t, "Demo", cfg.Title)
		assert.Equal(t, 3030, cfg.Port)
	})

	t.Run("rejects invalid YAML", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yml"),
			[]byte("{{invalid"), 0o644))

		_, err := loadAppConfig(dir, baseDir)
		assert.Error(t, err)
	})
}

func TestLoadServiceConfig(t *testing.T) {
	t.Run("missing ConfigDir returns defaults with name", func(t *testing.T) {
		cfg, err := loadServiceConfig(Service{Name: "petstore"})
		require.NoError(t, err)
		assert.Equal(t, "petstore", cfg.Name)
		assert.NotNil(t, cfg.Cache)
	})

	t.Run("missing config.yml returns defaults with name", func(t *testing.T) {
		cfg, err := loadServiceConfig(Service{
			Name:      "petstore",
			ConfigDir: t.TempDir(),
		})
		require.NoError(t, err)
		assert.Equal(t, "petstore", cfg.Name)
	})

	t.Run("reads per-service config", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
			[]byte("latency: 100ms\nmount: pets/v2\nerrors:\n  p10: 500\n"), 0o644))

		cfg, err := loadServiceConfig(Service{Name: "petstore", ConfigDir: dir})
		require.NoError(t, err)
		assert.Equal(t, "petstore", cfg.Name)
		assert.Equal(t, "100ms", cfg.Latency.String())
		assert.Equal(t, "pets/v2", cfg.Mount)
		assert.Equal(t, 500, cfg.Errors["p10"])
	})

	t.Run("folder name wins over file name field", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
			[]byte("name: somethingElse\nlatency: 5ms\n"), 0o644))

		cfg, err := loadServiceConfig(Service{Name: "petstore", ConfigDir: dir})
		require.NoError(t, err)
		assert.Equal(t, "petstore", cfg.Name)
		assert.Equal(t, "5ms", cfg.Latency.String())
	})

	t.Run("rejects invalid YAML", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
			[]byte("{{invalid"), 0o644))

		_, err := loadServiceConfig(Service{Name: "petstore", ConfigDir: dir})
		assert.Error(t, err)
	})
}

func TestLoadServiceContext(t *testing.T) {
	t.Run("missing ConfigDir returns nil", func(t *testing.T) {
		bts, err := loadServiceContext(Service{Name: "petstore"})
		require.NoError(t, err)
		assert.Nil(t, bts)
	})

	t.Run("missing context.yml returns nil", func(t *testing.T) {
		bts, err := loadServiceContext(Service{
			Name:      "petstore",
			ConfigDir: t.TempDir(),
		})
		require.NoError(t, err)
		assert.Nil(t, bts)
	})

	t.Run("reads flat context values", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "context.yml"),
			[]byte("status: [available, pending, sold]\npayment_method: card\n"), 0o644))

		bts, err := loadServiceContext(Service{Name: "petstore", ConfigDir: dir})
		require.NoError(t, err)
		assert.Contains(t, string(bts), "status")
		assert.Contains(t, string(bts), "payment_method")
	})

	t.Run("empty file returns nil", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "context.yml"), []byte(""), 0o644))

		bts, err := loadServiceContext(Service{Name: "petstore", ConfigDir: dir})
		require.NoError(t, err)
		assert.Nil(t, bts)
	})

	t.Run("rejects invalid YAML", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "context.yml"), []byte("{{invalid"), 0o644))

		_, err := loadServiceContext(Service{Name: "petstore", ConfigDir: dir})
		assert.Error(t, err)
	})
}

func TestRootFromArgs(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "petstore.yml")
	require.NoError(t, os.WriteFile(spec, []byte("openapi: 3.0.0"), 0o644))

	t.Run("returns the dir for a dir arg", func(t *testing.T) {
		assert.Equal(t, dir, rootFromArgs([]string{dir}))
	})

	t.Run("returns the parent dir for a file arg", func(t *testing.T) {
		assert.Equal(t, dir, rootFromArgs([]string{spec}))
	})

	t.Run("ignores flags", func(t *testing.T) {
		assert.Equal(t, dir, rootFromArgs([]string{"--port", "3000", dir}))
	})

	t.Run("returns empty when no useful arg", func(t *testing.T) {
		assert.Empty(t, rootFromArgs([]string{"--port", "3000"}))
	})

	t.Run("skips URLs", func(t *testing.T) {
		assert.Equal(t, dir, rootFromArgs([]string{"https://x.com/a.yml", dir}))
	})
}
