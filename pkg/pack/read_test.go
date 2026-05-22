package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadManifestFromDir(t *testing.T) {
	t.Run("missing manifest returns nil", func(t *testing.T) {
		dir := t.TempDir()
		m, err := LoadManifestFromDir(dir)
		assert.NoError(t, err)
		assert.Nil(t, m)
	})

	t.Run("manifest loads", func(t *testing.T) {
		dir := t.TempDir()
		manifest := []byte(`{"format":1,"name":"x","created_at":"2025-01-01T00:00:00Z"}`)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFilename), manifest, 0o644))
		m, err := LoadManifestFromDir(dir)
		assert.NoError(t, err)
		require.NotNil(t, m)
		assert.Equal(t, "x", m.Name)
	})

	t.Run("invalid manifest errors", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ManifestFilename), []byte("not json"), 0o644))
		_, err := LoadManifestFromDir(dir)
		assert.Error(t, err)
	})
}
