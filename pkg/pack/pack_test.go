package pack

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPack_FlatRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "petstore.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stripe.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))

	out := filepath.Join(t.TempDir(), "pack.mockz")
	require.NoError(t, Pack(dir, out, Options{
		Name:          "demo",
		CreatedBy:     "mockzilla/test",
		Now:           fixedNow,
		SkipGitSource: true,
	}))

	m, err := PeekManifest(out)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, CurrentFormat, m.Format)
	assert.Equal(t, "demo", m.Name)
	assert.Equal(t, "mockzilla/test", m.CreatedBy)
	require.Len(t, m.Services, 2)
	names := []string{m.Services[0].Name, m.Services[1].Name}
	assert.Contains(t, names, "petstore")
	assert.Contains(t, names, "stripe")
}

func TestPack_ServicesRoot_WithMergeMode(t *testing.T) {
	dir := t.TempDir()
	pets := filepath.Join(dir, "services", "petstore")
	require.NoError(t, os.MkdirAll(pets, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pets, "openapi.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pets, "config.yml"),
		[]byte("latency: 100ms\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(pets, "v1", "users", "get"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pets, "v1", "users", "get", "index.json"),
		[]byte(`{"id":1}`), 0o644))

	out := filepath.Join(t.TempDir(), "pack.mockz")
	require.NoError(t, Pack(dir, out, Options{
		CreatedBy:     "mockzilla/test",
		Now:           fixedNow,
		SkipGitSource: true,
	}))

	m, err := PeekManifest(out)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Len(t, m.Services, 1)
	s := m.Services[0]
	assert.Equal(t, "petstore", s.Name)
	assert.Equal(t, "/petstore", s.Mount)
	assert.Equal(t, ModeMerge, s.Mode)
	assert.Equal(t, "services/petstore/openapi.yml", s.Files.Spec)
	assert.Equal(t, "services/petstore/config.yml", s.Files.Config)
	// Merge mode produces two scanner entries: the static endpoint
	// override AND the spec file itself exposed as a literal asset
	// at GET /<filename>. Both are valid manifest entries the
	// runtime uses.
	require.Len(t, s.Endpoints, 2)
	byPath := map[string]EndpointEntry{}
	for _, ep := range s.Endpoints {
		byPath[ep.Path] = ep
	}
	override, ok := byPath["/v1/users"]
	require.True(t, ok)
	assert.Equal(t, "GET", override.Method)
	assert.Equal(t, "services/petstore/v1/users/get/index.json", override.File)

	asset, ok := byPath["/openapi.yml"]
	require.True(t, ok)
	assert.Equal(t, "GET", asset.Method)
	assert.Equal(t, "services/petstore/openapi.yml", asset.File)
}

func TestPack_NameInferenceFromConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "openapi.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte("name: payments\nmount: api/v1\n"), 0o644))

	out := filepath.Join(t.TempDir(), "pack.mockz")
	require.NoError(t, Pack(dir, out, Options{
		CreatedBy:     "mockzilla/test",
		Now:           fixedNow,
		SkipGitSource: true,
	}))

	m, err := PeekManifest(out)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Len(t, m.Services, 1)
	assert.Equal(t, "payments", m.Services[0].Name)
	assert.Equal(t, "/api/v1", m.Services[0].Mount)
}

func TestPack_FolderBasenameFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "staticsvc")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"),
		[]byte(`{"ok":true}`), 0o644))

	out := filepath.Join(t.TempDir(), "pack.mockz")
	require.NoError(t, Pack(dir, out, Options{
		CreatedBy:     "mockzilla/test",
		Now:           fixedNow,
		SkipGitSource: true,
	}))

	m, err := PeekManifest(out)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Len(t, m.Services, 1)
	// No config.yml and no non-generic spec, so the folder basename is
	// the last-resort name signal (mirrors the portable runtime).
	assert.Equal(t, "staticsvc", m.Services[0].Name)
	assert.Equal(t, "/staticsvc", m.Services[0].Mount)
	assert.Equal(t, ModeStatic, m.Services[0].Mode)
}

func TestPack_ManifestIsFirstEntry(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "services", "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "services", "a", "openapi.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))

	out := filepath.Join(t.TempDir(), "pack.mockz")
	require.NoError(t, Pack(dir, out, Options{
		CreatedBy:     "mockzilla/test",
		Now:           fixedNow,
		SkipGitSource: true,
	}))

	f, err := os.Open(out)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gr, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	require.NoError(t, err)
	assert.Equal(t, ManifestFilename, hdr.Name, "manifest must be the first tar entry")
}

func TestPack_SkipsNoiseDirs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "services", "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "services", "a", "openapi.yml"),
		[]byte("openapi: 3.0.0\ninfo: {title: x, version: '1'}\npaths: {}\n"), 0o644))
	// Noise we expect to be skipped.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules", "foo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules", "foo", "x.json"),
		[]byte(`{}`), 0o644))

	out := filepath.Join(t.TempDir(), "pack.mockz")
	require.NoError(t, Pack(dir, out, Options{
		CreatedBy:     "mockzilla/test",
		Now:           fixedNow,
		SkipGitSource: true,
	}))

	for _, name := range tarEntries(t, out) {
		assert.NotContains(t, name, ".git/")
		assert.NotContains(t, name, "node_modules/")
	}
}

func TestReadManifest_RejectsFutureFormat(t *testing.T) {
	future := []byte(`{"format":999,"created_at":"2026-01-01T00:00:00Z","created_by":"x","services":[]}`)
	_, err := ReadManifest(bytesReader(future))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer")
}

func TestPeekManifest_ReturnsNilForArchiveWithoutManifest(t *testing.T) {
	// Build a tar that doesn't start with .mockzilla.json.
	out := filepath.Join(t.TempDir(), "legacy.mockz")
	f, err := os.Create(out)
	require.NoError(t, err)
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	body := []byte("openapi: 3.0.0")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "services/x/openapi.yml", Size: int64(len(body))}))
	_, err = tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	require.NoError(t, f.Close())

	m, err := PeekManifest(out)
	require.NoError(t, err)
	assert.Nil(t, m)
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
}

func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func tarEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gr, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	return names
}

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

func TestDetectGitSourceNotInRepo(t *testing.T) {
	assert.Nil(t, detectGitSource(t.TempDir()))
}
