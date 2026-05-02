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
// code so the caller (cmd/server/main.go) can `return inspect.Run(...)`.
func Run(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: mockzilla info <url-or-file>")
		return exitError
	}
	src := args[0]

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
