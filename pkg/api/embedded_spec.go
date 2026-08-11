package api

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

const specSetupDir = "setup"

// ReadEmbeddedSpec reads the OpenAPI spec a generated service embeds, expanding
// it when the service was generated with spec compression enabled. The embed
// pattern decides which single file is present.
func ReadEmbeddedSpec(fsys fs.FS) ([]byte, error) {
	entries, err := fs.ReadDir(fsys, specSetupDir)
	if err != nil {
		return nil, fmt.Errorf("reading embedded directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		return readSpecFile(fsys, path.Join(specSetupDir, entry.Name()))
	}

	return nil, errors.New("no file found in embedded filesystem")
}

func readSpecFile(fsys fs.FS, name string) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if !strings.HasSuffix(name, ".gz") {
		return io.ReadAll(f)
	}

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompressing embedded spec %s: %w", name, err)
	}
	defer func() { _ = zr.Close() }()

	return io.ReadAll(zr)
}
