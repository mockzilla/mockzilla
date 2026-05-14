package pack

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Options carry the user-supplied knobs for Pack. All fields are
// optional; Pack picks sensible defaults when they're empty.
type Options struct {
	// Name overrides the manifest's display name. Defaults to empty.
	Name string

	// Description is free-text included in the manifest. Defaults to
	// empty.
	Description string

	// MinMockzillaVersion records the minimum CLI version required to
	// load the archive. Defaults to empty (no minimum declared).
	MinMockzillaVersion string

	// CreatedBy is the tool identifier embedded in the manifest, e.g.
	// "mockzilla/2.3.0". Defaults to "mockzilla/unknown".
	CreatedBy string

	// Now is the timestamp the manifest is stamped with. Defaults to
	// time.Now().UTC(). Override for deterministic tests.
	Now func() time.Time

	// SkipGitSource suppresses the manifest's `source` block even
	// when srcDir is inside a git repo. Useful in tests and for
	// build environments that prefer not to embed VCS metadata.
	SkipGitSource bool
}

// Pack assembles a `.mockz` archive at outPath from the service tree
// rooted at srcDir. It discovers services using the same rules as the
// runtime, builds a `.mockzilla.json` manifest, and writes the
// manifest as the first tar entry so streaming readers can grab it
// without unpacking the rest.
//
// The resulting archive contains:
//   - `.mockzilla.json` (manifest, first entry)
//   - the srcDir tree (services/, app.yml, etc.) with noise dirs
//     (.git, node_modules, vendor, dotted/underscore-prefixed)
//     filtered out
func Pack(srcDir, outPath string, opts Options) error {
	services, err := Discover(srcDir)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	if len(services) == 0 {
		return errors.New("nothing to pack: no services found")
	}

	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}

	manifest := &Manifest{
		Format:              CurrentFormat,
		Name:                opts.Name,
		Description:         opts.Description,
		CreatedAt:           opts.Now(),
		CreatedBy:           defaultCreatedBy(opts.CreatedBy),
		MinMockzillaVersion: opts.MinMockzillaVersion,
		Services:            services,
	}
	if !opts.SkipGitSource {
		if src := detectGitSource(srcDir); src != nil {
			manifest.Source = src
		}
	}

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output: %w", err)
	}
	defer func() { _ = out.Close() }()

	if err := writeArchive(out, manifest, srcDir); err != nil {
		_ = os.Remove(outPath)
		return err
	}
	return nil
}

// WriteTo streams a `.mockz` archive to w using a pre-built manifest
// and the tree at srcDir. Lower-level entry point used by callers that
// already have a Manifest in hand (e.g. tests, custom packagers).
func WriteTo(w io.Writer, manifest *Manifest, srcDir string) error {
	return writeArchive(w, manifest, srcDir)
}

func writeArchive(w io.Writer, manifest *Manifest, srcDir string) error {
	gw := gzip.NewWriter(w)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	// Manifest must be the first entry so streaming readers (e.g.
	// `mockzilla info foo.mockz`) can fetch it without consuming the
	// whole archive.
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling manifest: %w", err)
	}
	if err := writeTarFile(tw, ManifestFilename, manifestBytes, manifest.CreatedAt); err != nil {
		return fmt.Errorf("writing manifest entry: %w", err)
	}

	if err := walkAndPack(tw, srcDir); err != nil {
		return err
	}
	return nil
}

// walkAndPack walks srcDir and writes each file as a tar entry,
// skipping noise dirs (.git, node_modules, dotted/underscore prefixes)
// and the manifest itself if it somehow exists at the root.
func walkAndPack(tw *tar.Writer, srcDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if info.IsDir() {
			if shouldSkipPackDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		// Avoid clobbering the manifest entry we already wrote.
		if rel == ManifestFilename {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		return writeTarFile(tw, rel, data, info.ModTime())
	})
}

func writeTarFile(tw *tar.Writer, name string, data []byte, mtime time.Time) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: mtime,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func shouldSkipPackDir(name string) bool {
	if name == "" || name == "." {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "target", "dist":
		return true
	}
	return false
}

func defaultCreatedBy(provided string) string {
	if provided != "" {
		return provided
	}
	return "mockzilla/unknown"
}

// detectGitSource shells out to `git` to fill in remote/ref/commit.
// Returns nil if srcDir isn't in a git working tree or `git` is
// unavailable; the manifest's `source` block stays absent in that
// case.
func detectGitSource(srcDir string) *Source {
	if !inGitRepo(srcDir) {
		return nil
	}
	src := &Source{Type: "git"}
	if v, ok := runGit(srcDir, "remote", "get-url", "origin"); ok {
		src.Remote = v
	}
	if v, ok := runGit(srcDir, "symbolic-ref", "HEAD"); ok {
		src.Ref = v
	} else if v, ok := runGit(srcDir, "rev-parse", "--abbrev-ref", "HEAD"); ok && v != "HEAD" {
		// Detached HEAD case: skip Ref rather than record literal "HEAD".
		src.Ref = v
	}
	if v, ok := runGit(srcDir, "rev-parse", "HEAD"); ok {
		src.Commit = v
	}
	if src.Remote == "" && src.Ref == "" && src.Commit == "" {
		return nil
	}
	return src
}

func inGitRepo(srcDir string) bool {
	_, ok := runGit(srcDir, "rev-parse", "--is-inside-work-tree")
	return ok
}

func runGit(srcDir string, args ...string) (string, bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
