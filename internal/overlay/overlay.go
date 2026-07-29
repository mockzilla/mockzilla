package overlay

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/files"
)

// Source is one resolved OpenAPI Overlay: Name as configured (used in error
// messages), Path for generation, and Literal for the generated service.
type Source struct {
	Name    string
	Path    string
	Literal string
}

// Resolve reads every configured overlay, resolving relative paths against
// setupDir. Contents are inlined into the generated service rather than
// embedded: go:embed cannot reach outside the package directory, so overlays
// shared between API versions (../) and URL sources have no embeddable path.
func Resolve(setupDir string, sources []string) ([]Source, error) {
	overlays := make([]Source, 0, len(sources))
	for _, src := range sources {
		path := src
		if !filepath.IsAbs(path) && !files.IsURL(path) {
			path = filepath.Join(setupDir, path)
		}

		contents, err := files.ReadFileOrURL(path)
		if err != nil {
			return nil, fmt.Errorf("reading overlay %q: %w", src, err)
		}

		resolved := Source{
			Name:    src,
			Path:    path,
			Literal: goStringLiteral(string(contents)),
		}
		overlays = append(overlays, resolved)
	}

	return overlays, nil
}

// goStringLiteral renders s as Go source, preferring a raw string so the
// overlay stays readable in the generated file. Raw strings cannot hold
// backquotes or carriage returns, so those fall back to a quoted string.
func goStringLiteral(s string) string {
	if strings.ContainsAny(s, "`\r") {
		return strconv.Quote(s)
	}
	return "`" + s + "`"
}
