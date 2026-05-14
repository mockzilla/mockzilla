package pack

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ReadManifest parses a manifest from r. Returns an error if the
// document is malformed or carries a `format` newer than this build
// understands.
func ReadManifest(r io.Reader) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decoding manifest: %w", err)
	}
	if m.Format > CurrentFormat {
		return nil, fmt.Errorf(
			"manifest format %d is newer than this build understands (max %d); upgrade mockzilla",
			m.Format, CurrentFormat)
	}
	if m.Format <= 0 {
		return nil, errors.New("manifest format must be a positive integer")
	}
	return &m, nil
}

// LoadManifestFromDir reads `.mockzilla.json` from dir. Returns
// (nil, nil) when the file is absent (archive packed by an older
// build, or a raw directory with no manifest); both are normal cases
// the caller handles by falling back to discovery.
func LoadManifestFromDir(dir string) (*Manifest, error) {
	f, err := os.Open(filepath.Join(dir, ManifestFilename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening manifest: %w", err)
	}
	defer func() { _ = f.Close() }()
	return ReadManifest(f)
}

// PeekManifest reads only the manifest from a .mockz archive at
// archivePath. Thin file-path wrapper around PeekManifestFromReader.
func PeekManifest(archivePath string) (*Manifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	return PeekManifestFromReader(f)
}

// PeekManifestFromReader reads only the manifest from a .mockz stream.
// Returns (nil, nil) when the archive doesn't carry a manifest as its
// first entry (older or hand-built archives). The reader only needs
// to contain enough bytes for the first tar entry; the rest can be
// short-read or never produced (useful for streaming over HTTP).
func PeekManifestFromReader(r io.Reader) (*Manifest, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		if hdr.Name != ManifestFilename {
			// Manifest must be the first entry. If we see anything
			// else first, the archive doesn't carry one.
			return nil, nil
		}
		return ReadManifest(tr)
	}
}
