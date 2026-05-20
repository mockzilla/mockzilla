package pack

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteTo(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "openapi.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))

	m := &Manifest{
		Format:    1,
		Name:      "n",
		CreatedAt: time.Unix(0, 0).UTC(),
	}
	var buf bytes.Buffer
	require.NoError(t, WriteTo(&buf, m, src))
	assert.NotZero(t, buf.Len())
}

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

func TestShouldSkipPackDir(t *testing.T) {
	assert.False(t, shouldSkipPackDir(""))
	assert.False(t, shouldSkipPackDir("."))
	assert.False(t, shouldSkipPackDir("src"))
	assert.True(t, shouldSkipPackDir(".git"))
	assert.True(t, shouldSkipPackDir("_build"))
	assert.True(t, shouldSkipPackDir("node_modules"))
	assert.True(t, shouldSkipPackDir("vendor"))
	assert.True(t, shouldSkipPackDir("target"))
	assert.True(t, shouldSkipPackDir("dist"))
}

func TestDefaultCreatedBy(t *testing.T) {
	assert.Equal(t, "explicit", defaultCreatedBy("explicit"))
	assert.Equal(t, "mockzilla/unknown", defaultCreatedBy(""))
}

func TestInGitRepoFalse(t *testing.T) {
	assert.False(t, inGitRepo(t.TempDir()))
}

func TestRunGitFailure(t *testing.T) {
	_, ok := runGit(t.TempDir(), "rev-parse", "--is-inside-work-tree")
	assert.False(t, ok)
}

func TestDetectGitSourceNotInRepo(t *testing.T) {
	assert.Nil(t, detectGitSource(t.TempDir()))
}
