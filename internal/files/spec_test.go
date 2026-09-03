package files

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSpecFile(t *testing.T) {
	for _, name := range []string{"petstore.yaml", "petstore.yml", "petstore.json", "PETSTORE.YML"} {
		assert.True(t, IsSpec(name), name)
	}
	for _, name := range []string{"petstore.go", "petstore.txt", "petstore"} {
		assert.False(t, IsSpec(name), name)
	}
}

func TestIsReservedName(t *testing.T) {
	t.Run("every reserved stem, in every spec extension", func(t *testing.T) {
		for _, stem := range []string{"config", "context", "app", "codegen", "index"} {
			for _, ext := range SpecExts {
				assert.True(t, IsReservedName(stem+ext), stem+ext)
			}
		}
	})

	t.Run("a spec is not reserved", func(t *testing.T) {
		for _, name := range []string{"openapi.yml", "stripe.yaml", "petstore.json"} {
			assert.False(t, IsReservedName(name), name)
		}
	})
}

func TestFindSpecs(t *testing.T) {
	write := func(t *testing.T, dir, name string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("openapi: 3.0.0"), 0o644))
	}

	t.Run("a reserved file never wins on sort order", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "codegen.yml")
		write(t, dir, "openapi.yml")

		assert.Equal(t, []string{filepath.Join(dir, "openapi.yml")}, FindSpecs(dir))
	})

	t.Run("subdirectories are ignored", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "setup"), 0o755))
		write(t, dir, filepath.Join("setup", "codegen.yml"))
		write(t, dir, "openapi.yml")

		assert.Equal(t, []string{filepath.Join(dir, "openapi.yml")}, FindSpecs(dir))
	})

	t.Run("candidates come back sorted", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "z-other.yml")
		write(t, dir, "a-first.yml")

		assert.Equal(t, []string{
			filepath.Join(dir, "a-first.yml"),
			filepath.Join(dir, "z-other.yml"),
		}, FindSpecs(dir))
	})
}
