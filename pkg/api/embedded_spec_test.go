package api

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failOpenFS lists its entries but refuses to open one of them. Embedding the
// fs.FS interface keeps ReadDirFS unpromoted, so directory reads fall back to
// Open and still succeed.
type failOpenFS struct {
	fs.FS
	fail string
}

func (f failOpenFS) Open(name string) (fs.File, error) {
	if name == f.fail {
		return nil, fs.ErrPermission
	}
	return f.FS.Open(name)
}

func TestReadEmbeddedSpec(t *testing.T) {
	spec := []byte("openapi: 3.0.0\ninfo:\n  title: t\n  version: 1\npaths: {}\n")

	gzipped := func(t *testing.T, payload []byte) []byte {
		t.Helper()
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, err := zw.Write(payload)
		require.NoError(t, err)
		require.NoError(t, zw.Close())
		return buf.Bytes()
	}

	t.Run("reads a plain yaml spec", func(t *testing.T) {
		got, err := ReadEmbeddedSpec(fstest.MapFS{"setup/openapi.yml": {Data: spec}})
		require.NoError(t, err)
		assert.Equal(t, spec, got)
	})

	t.Run("reads a plain json spec", func(t *testing.T) {
		got, err := ReadEmbeddedSpec(fstest.MapFS{"setup/openapi.json": {Data: spec}})
		require.NoError(t, err)
		assert.Equal(t, spec, got)
	})

	t.Run("expands a compressed spec", func(t *testing.T) {
		got, err := ReadEmbeddedSpec(fstest.MapFS{"setup/spec.gz": {Data: gzipped(t, spec)}})
		require.NoError(t, err)
		assert.Equal(t, spec, got)
	})

	t.Run("errors when nothing is embedded", func(t *testing.T) {
		_, err := ReadEmbeddedSpec(fstest.MapFS{})
		require.Error(t, err)
	})

	t.Run("errors when the setup directory holds only subdirectories", func(t *testing.T) {
		_, err := ReadEmbeddedSpec(fstest.MapFS{"setup/data/index.json": {Data: []byte("{}")}})
		require.Error(t, err)
	})

	t.Run("propagates a failure to open the embedded file", func(t *testing.T) {
		fsys := failOpenFS{
			FS:   fstest.MapFS{"setup/openapi.yml": {Data: spec}},
			fail: "setup/openapi.yml",
		}

		_, err := ReadEmbeddedSpec(fsys)
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run("errors when a .gz payload is not gzip", func(t *testing.T) {
		_, err := ReadEmbeddedSpec(fstest.MapFS{"setup/spec.gz": {Data: spec}})
		require.Error(t, err)
	})
}
