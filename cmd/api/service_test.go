package api

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteCompressedSpec(t *testing.T) {
	spec := []byte("openapi: 3.0.0\ninfo:\n  title: t\n  version: 1\npaths: {}\n")

	readBack := func(t *testing.T, dir string) []byte {
		t.Helper()
		f, err := os.Open(filepath.Join(dir, "spec.gz"))
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		zr, err := gzip.NewReader(f)
		require.NoError(t, err)
		defer func() { _ = zr.Close() }()

		out, err := io.ReadAll(zr)
		require.NoError(t, err)
		return out
	}

	t.Run("writes a spec.gz that decompresses to the input", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeCompressedSpec(dir, spec))
		assert.Equal(t, spec, readBack(t, dir))
	})

	t.Run("leaves the plain spec name untouched", func(t *testing.T) {
		// The generated service embeds setup/spec.gz; a name matching the
		// setup/openapi.* glob older services embed would feed them gzip bytes.
		dir := t.TempDir()
		require.NoError(t, writeCompressedSpec(dir, spec))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "spec.gz", entries[0].Name())
	})

	t.Run("overwrites a stale spec.gz", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.gz"), []byte("stale"), 0644))
		require.NoError(t, writeCompressedSpec(dir, spec))
		assert.Equal(t, spec, readBack(t, dir))
	})

	t.Run("compresses a repetitive spec well below its source size", func(t *testing.T) {
		dir := t.TempDir()
		large := make([]byte, 0, 64*1024)
		for len(large) < 64*1024 {
			large = append(large, "    description: a repeated schema description\n"...)
		}
		require.NoError(t, writeCompressedSpec(dir, large))

		info, err := os.Stat(filepath.Join(dir, "spec.gz"))
		require.NoError(t, err)
		assert.Less(t, info.Size(), int64(len(large))/4)
		assert.Equal(t, large, readBack(t, dir))
	})

	t.Run("errors when the setup directory does not exist", func(t *testing.T) {
		err := writeCompressedSpec(filepath.Join(t.TempDir(), "missing"), spec)
		require.Error(t, err)
	})
}
