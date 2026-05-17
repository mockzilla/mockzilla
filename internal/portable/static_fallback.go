package portable

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cmdapi "github.com/mockzilla/mockzilla/v2/cmd/api"
)

// openapiKeyRE matches a top-level `openapi: 3.x...` declaration as it
// would appear in JSON or YAML. Anchoring the value to `3.` (we only
// support OpenAPI 3.x) avoids treating arbitrary `{"openapi": true}`
// or `"openapi": "foo"` JSON bodies as specs.
var openapiKeyRE = regexp.MustCompile(`(?mi)(^|[{,])\s*["']?openapi["']?\s*:\s*["']?3\.`)

// looksLikeOpenAPISpec reports whether the bytes appear to declare an
// OpenAPI document. Real specs (e.g. Stripe) can place `openapi:` after
// big `components:` blocks, so we scan the whole body — the caller has
// already read it into memory anyway.
func looksLikeOpenAPISpec(data []byte) bool {
	return openapiKeyRE.Match(data)
}

// staticContentExt returns a sensible file extension for a one-off
// static body. Honours the path's extension when it maps to a
// known content type, otherwise falls back to a content-type lookup,
// and finally to `.json`.
func staticContentExt(pathHint, contentType string) string {
	if ext := filepath.Ext(pathHint); cmdapi.GetContentType(ext) != "" {
		return ext
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])) {
	case "application/json":
		return ".json"
	case "application/xml", "text/xml":
		return ".xml"
	case "text/html":
		return ".html"
	case "text/plain":
		return ".txt"
	case "application/yaml", "text/yaml":
		return ".yml"
	}
	return ".json"
}

// resolveStaticFile turns a single non-spec file into a one-route
// Service mounted at `/`. We drop the bytes into a fresh temp dir as
// `index.<ext>` so the existing static-mode pipeline
// (scanStaticFiles → GenerateSpecFromStaticDir) takes over: the file
// becomes `GET /` on the synthesized spec.
//
// The service name is intentionally empty so the runtime mounts the
// route at the server root, which is what a user expects from
// `mockzilla some.json`.
func resolveStaticFile(pathHint string, data []byte, contentType string) ([]Service, error) {
	parent := filepath.Join(os.TempDir(), "mockzilla-portable", "static")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	dir, err := os.MkdirTemp(parent, "svc-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	ext := staticContentExt(pathHint, contentType)
	target := filepath.Join(dir, "index"+ext)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return nil, fmt.Errorf("writing static file: %w", err)
	}

	svc, err := resolveServiceDir(dir, "")
	if err != nil {
		return nil, fmt.Errorf("resolving static fallback: %w", err)
	}
	slog.Info("Serving file as static response", "source", pathHint, "mount", "/")
	return []Service{svc}, nil
}

// isStaticContentFile reports whether name has a file extension we can
// serve as a static response (`.json`, `.html`, `.txt`, etc.). Used to
// recognise non-spec single-file args as portable inputs.
func isStaticContentFile(name string) bool {
	return cmdapi.GetContentType(filepath.Ext(name)) != ""
}
