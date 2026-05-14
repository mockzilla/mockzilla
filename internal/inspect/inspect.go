// Package inspect implements the `mockzilla info <spec>` subcommand.
// It reads an OpenAPI spec (file path or URL), summarises it, and writes
// the summary as a single JSON object to stdout.
//
// The output is consumed by automation (the mockzilla MCP bridge in
// particular), so the schema is intentionally narrow and stable: title,
// version, openapi_version, endpoint_count, and a flat list of paths.
package inspect

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/pack"
	"github.com/pb33f/libopenapi"
)

const (
	exitOK    = 0
	exitError = 1
)

// Summary is the JSON object emitted on stdout.
type Summary struct {
	Title          string     `json:"title"`
	Version        string     `json:"version"`
	OpenAPIVersion string     `json:"openapi_version"`
	EndpointCount  int        `json:"endpoint_count"`
	Paths          []Endpoint `json:"paths"`
}

type Endpoint struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operation_id,omitempty"`
}

// Run parses the args and prints the summary. Returns a process exit
// code so the caller (cmd/mockzilla/main.go) can `return inspect.Run(...)`.
//
// Two input shapes are recognised:
//
//   - OpenAPI spec (file or URL) → emits Summary describing the spec.
//   - `.mockz` / `.tar.gz` package on disk → reads the manifest and
//     emits PackageSummary describing the package.
func Run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: mockzilla info <url-or-file>")
		return exitError
	}
	src := args[0]

	if isPackageFile(src) || isPackageURL(src) {
		return runPackage(src)
	}

	raw, err := load(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "info: %v\n", err)
		return exitError
	}

	summary, err := Summarize(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "info: %v\n", err)
		return exitError
	}

	out, err := json.Marshal(summary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "info: marshal: %v\n", err)
		return exitError
	}
	fmt.Println(string(out))
	return exitOK
}

// PackageSummary is the JSON object emitted for a `.mockz` archive.
type PackageSummary struct {
	Format              int             `json:"format"`
	Name                string          `json:"name,omitempty"`
	Description         string          `json:"description,omitempty"`
	CreatedAt           string          `json:"created_at"`
	CreatedBy           string          `json:"created_by"`
	MinMockzillaVersion string          `json:"min_mockzilla_version,omitempty"`
	Source              *pack.Source    `json:"source,omitempty"`
	ServiceCount        int             `json:"service_count"`
	Services            []PackedService `json:"services"`
}

// PackedService is a compact per-service entry in the package summary.
type PackedService struct {
	Name          string `json:"name"`
	Mount         string `json:"mount"`
	Mode          string `json:"mode"`
	EndpointCount int    `json:"endpoint_count"`
}

func runPackage(src string) int {
	manifest, err := loadPackageManifest(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "info: %v\n", err)
		return exitError
	}
	if manifest == nil {
		fmt.Fprintf(os.Stderr, "info: %s has no .mockzilla.json manifest (likely an older or hand-built archive)\n", src)
		return exitError
	}

	services := make([]PackedService, 0, len(manifest.Services))
	for _, s := range manifest.Services {
		services = append(services, PackedService{
			Name:          s.Name,
			Mount:         s.Mount,
			Mode:          string(s.Mode),
			EndpointCount: len(s.Endpoints),
		})
	}

	out, err := json.Marshal(PackageSummary{
		Format:              manifest.Format,
		Name:                manifest.Name,
		Description:         manifest.Description,
		CreatedAt:           manifest.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedBy:           manifest.CreatedBy,
		MinMockzillaVersion: manifest.MinMockzillaVersion,
		Source:              manifest.Source,
		ServiceCount:        len(services),
		Services:            services,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "info: marshal: %v\n", err)
		return exitError
	}
	fmt.Println(string(out))
	return exitOK
}

func isPackageFile(path string) bool {
	return strings.HasSuffix(path, ".mockz") || strings.HasSuffix(path, ".tar.gz")
}

func isPackageURL(src string) bool {
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return false
	}
	return isPackageFile(src)
}

// loadPackageManifest fetches just enough bytes of src to read the
// manifest. For local files this opens and streams via os.File. For
// URLs it HTTP-GETs and streams the response body through the gzip +
// tar reader; we never download the whole archive when the manifest
// is at the front.
func loadPackageManifest(src string) (*pack.Manifest, error) {
	if isPackageURL(src) {
		resp, err := http.Get(src) //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", src, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, src)
		}
		return pack.PeekManifestFromReader(resp.Body)
	}
	return pack.PeekManifest(src)
}

// Summarize parses an OpenAPI document and returns its summary. Exposed
// so unit tests can exercise it without going through stdin/stdout.
func Summarize(raw []byte) (Summary, error) {
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return Summary{}, fmt.Errorf("parse OpenAPI: %w", err)
	}

	model, buildErr := doc.BuildV3Model()
	if buildErr != nil {
		return Summary{}, fmt.Errorf("build OpenAPI model: %w", buildErr)
	}

	endpoints := []Endpoint{}
	if model.Model.Paths != nil && model.Model.Paths.PathItems != nil {
		for path, pathItem := range model.Model.Paths.PathItems.FromOldest() {
			for method, op := range pathItem.GetOperations().FromOldest() {
				endpoints = append(endpoints, Endpoint{
					Method:      strings.ToUpper(method),
					Path:        path,
					OperationID: op.OperationId,
				})
			}
		}
	}

	title, version := "", ""
	if model.Model.Info != nil {
		title = model.Model.Info.Title
		version = model.Model.Info.Version
	}

	return Summary{
		Title:          title,
		Version:        version,
		OpenAPIVersion: model.Model.Version,
		EndpointCount:  len(endpoints),
		Paths:          endpoints,
	}, nil
}

func load(src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return loadURL(src)
	}
	return os.ReadFile(src)
}

func loadURL(rawURL string) ([]byte, error) {
	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(resp.Body)
}
