package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMountFromName(t *testing.T) {
	t.Run("empty name maps to root", func(t *testing.T) {
		assert.Equal(t, "/", mountFromName(""))
	})

	t.Run("non-empty name is prefixed with /", func(t *testing.T) {
		assert.Equal(t, "/petstore", mountFromName("petstore"))
	})

	t.Run("nested name keeps its slashes", func(t *testing.T) {
		assert.Equal(t, "/adyen/v71", mountFromName("adyen/v71"))
	})
}

func TestHasServiceCandidateSubdir(t *testing.T) {
	t.Run("nonexistent dir returns false", func(t *testing.T) {
		assert.False(t, hasServiceCandidateSubdir("/nonexistent-dir-that-should-not-exist-xyz"))
	})

	t.Run("empty dir returns false", func(t *testing.T) {
		assert.False(t, hasServiceCandidateSubdir(t.TempDir()))
	})

	t.Run("dir with only files returns false", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"), []byte("openapi: 3.0.0\n"), 0o644))
		assert.False(t, hasServiceCandidateSubdir(dir))
	})

	t.Run("dir with skipable subdir only returns false", func(t *testing.T) {
		// .git is a directory that ShouldSkipDir filters out.
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
		assert.False(t, hasServiceCandidateSubdir(dir))
	})

	t.Run("dir with a real subdir returns true", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "petstore"), 0o755))
		assert.True(t, hasServiceCandidateSubdir(dir))
	})
}

func TestHasTopLevelIndexFile(t *testing.T) {
	t.Run("nonexistent dir returns false", func(t *testing.T) {
		assert.False(t, hasTopLevelIndexFile("/nonexistent-dir-that-should-not-exist-xyz"))
	})

	t.Run("dir without an index.json returns false", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"), []byte("openapi: 3.0.0\n"), 0o644))
		assert.False(t, hasTopLevelIndexFile(dir))
	})

	t.Run("dir with index.json at top level returns true", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte("{}"), 0o644))
		assert.True(t, hasTopLevelIndexFile(dir))
	})
}

func TestIsImplicitServicesRoot(t *testing.T) {
	t.Run("returns false when a config file is present", func(t *testing.T) {
		dir := t.TempDir()
		assert.False(t, isImplicitServicesRoot(dir, true /* hasConfigFile */, nil))
	})

	t.Run("returns false when specs are present", func(t *testing.T) {
		dir := t.TempDir()
		assert.False(t, isImplicitServicesRoot(dir, false, []string{"openapi.yml"}))
	})

	t.Run("returns false when an index.json sits at the top level", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), []byte("{}"), 0o644))
		assert.False(t, isImplicitServicesRoot(dir, false, nil))
	})

	t.Run("returns true when only service candidate subdirs exist", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "petstore"), 0o755))
		assert.True(t, isImplicitServicesRoot(dir, false, nil))
	})
}
