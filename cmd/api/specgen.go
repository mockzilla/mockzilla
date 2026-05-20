package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"go.yaml.in/yaml/v4"
)

// Route represents a static route with its content.
type Route struct {
	Method      string
	Path        string
	ContentType string
	Content     string
	// SourceFile is the on-disk path the route was scanned from,
	// relative to nothing (caller decides what to do with it). Empty
	// for synthetic routes that weren't materialised as a file.
	SourceFile string
}

// httpMethods is the set of recognized HTTP methods used to identify method directories.
var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true,
	"delete": true, "head": true, "options": true, "trace": true,
}

// reservedConfigFiles are top-level filenames that carry mockzilla's own
// configuration rather than user-served content. They are skipped during
// service-folder scanning so neither spec discovery nor static-file
// auto-serving picks them up.
var reservedConfigFiles = map[string]bool{
	"config.yml":  true,
	"context.yml": true,
	"app.yml":     true,
}

// skippedDirNames lists subdirectory names that scans skip outright.
// Anything dotted or underscore-prefixed is also skipped (covers .git,
// .idea, .vscode, _build, etc.).
var skippedDirNames = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
}

// shouldSkipDir reports whether a subdirectory of a scanned tree should
// be ignored: tooling caches, hidden dirs, and the usual noise.
func shouldSkipDir(name string) bool {
	if name == "" || name == "." {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	return skippedDirNames[name]
}

// scanStaticFiles scans a service folder for static-mode endpoint files.
// Routes are derived from path shape:
//   - `<path>/<method>/index.<ext>` becomes `<METHOD> /<path>` (non-GET).
//   - `<path>/index.<ext>` becomes `GET /<path>`.
//   - top-level `index.<ext>` becomes `GET /`; other top-level files become
//     `GET /<filename>` returning literal content.
//
// Files with unsupported extensions are ignored. Hidden and well-known noise
// directories (.git, node_modules, ...) are skipped.
func scanStaticFiles(staticDir string) ([]Route, error) {
	var routes []Route

	err := filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if path == staticDir {
				return nil
			}
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(info.Name())
		contentType := GetContentType(ext)
		if contentType == "" {
			return nil
		}

		relPath, err := filepath.Rel(staticDir, path)
		if err != nil {
			return err
		}
		segments := strings.Split(filepath.ToSlash(relPath), "/")
		stem := strings.TrimSuffix(info.Name(), ext)

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading file %s: %w", path, err)
		}
		body := strings.TrimRight(string(content), "\n\r\t ")

		// Top-level file (no parent dir inside the service folder).
		if len(segments) == 1 {
			if reservedConfigFiles[info.Name()] {
				return nil
			}
			if stem == "index" {
				// Root endpoint for the service.
				routes = append(routes, Route{
					Method: "GET", Path: "/",
					ContentType: contentType, Content: body,
					SourceFile: path,
				})
			} else {
				// Literal asset (e.g. spec file fetchable at its path).
				routes = append(routes, Route{
					Method: "GET", Path: "/" + info.Name(),
					ContentType: contentType, Content: body,
					SourceFile: path,
				})
			}
			return nil
		}

		// Nested file: figure out method + path. Default verb is GET;
		// use `<method>/` as the immediate parent of `index.<ext>` to
		// override.
		method := "GET"
		pathSegments := segments[:len(segments)-1] // drop filename
		methodDir := segments[len(segments)-2]
		if httpMethods[strings.ToLower(methodDir)] {
			method = strings.ToUpper(methodDir)
			pathSegments = segments[:len(segments)-2] // drop method + filename
		}

		urlPath := "/"
		if len(pathSegments) > 0 {
			urlPath = "/" + strings.Join(pathSegments, "/")
		}

		// Non-`index` filenames keep their name in the URL
		// (e.g. `users/admin.json` becomes `/users/admin.json`).
		if stem != "index" {
			if urlPath == "/" {
				urlPath = "/" + info.Name()
			} else {
				urlPath = urlPath + "/" + info.Name()
			}
		}

		routes = append(routes, Route{
			Method:      method,
			Path:        urlPath,
			ContentType: contentType,
			Content:     body,
			SourceFile:  path,
		})
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking static directory: %w", err)
	}

	return routes, nil
}

// HasStaticEndpoints reports whether the directory contains at least
// one `<…>/index.<ext>` file (with or without an explicit method dir).
// Used by service-folder discovery to decide between spec mode and
// static mode.
func HasStaticEndpoints(dir string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			if path == dir {
				return nil
			}
			if shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		filename := info.Name()
		stem := strings.TrimSuffix(filename, filepath.Ext(filename))
		if stem != "index" {
			return nil
		}
		if GetContentType(filepath.Ext(filename)) == "" {
			return nil
		}
		// A top-level `index.<ext>` is also a static endpoint
		// (mounted at the service root). Anywhere deeper, the file is
		// always a static endpoint regardless of whether the parent
		// dir is an HTTP method (we default to GET when it isn't).
		found = true
		return filepath.SkipDir
	})
	return found
}

// GetContentType returns the content type for a file extension.
// Returns empty string for unsupported extensions.
func GetContentType(ext string) string {
	switch ext {
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		return ""
	}
}

// generateOpenAPIFromStatic generates an OpenAPI spec from static routes.
func generateOpenAPIFromStatic(routes []Route, serviceName string) ([]byte, error) {
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   serviceName,
			"version": "1.0.0",
		},
		"paths": make(map[string]any),
	}

	paths := spec["paths"].(map[string]any)

	for _, route := range routes {
		path := route.Path
		method := strings.ToLower(route.Method)

		// Get or create path item
		var pathItem map[string]any
		if existing, ok := paths[path]; ok {
			pathItem = existing.(map[string]any)
		} else {
			pathItem = make(map[string]any)
			paths[path] = pathItem
		}

		operation, err := buildStaticOperation(route)
		if err != nil {
			return nil, err
		}
		pathItem[method] = operation
	}

	// Marshal to YAML with 2-space indent
	yamlBytes, err := yaml.Dump(spec, yaml.WithIndent(2))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OpenAPI spec: %w", err)
	}

	header := "# This file is auto-generated from static files in setup/data/.\n# Do not edit manually - modify the static files and regenerate instead.\n"
	return append([]byte(header), yamlBytes...), nil
}

// buildStaticOperation builds one OpenAPI operation that carries a
// literal response body via the `x-static-response` extension. Shared
// between the spec-synthesized-from-static path and the merge path
// (where it overrides an entry inside a user-supplied spec).
func buildStaticOperation(route Route) (map[string]any, error) {
	method := strings.ToLower(route.Method)

	responseSchema, err := schema.BuildSchemaFromContent([]byte(route.Content), route.ContentType)
	if err != nil {
		return nil, fmt.Errorf("failed to build schema for %s %s: %w", route.Method, route.Path, err)
	}
	schemaMap := schemaToMap(responseSchema)

	operation := map[string]any{
		"operationId": generateOperationId(method, route.Path),
		"responses": map[string]any{
			"200": map[string]any{
				"description": "Success",
				"content": map[string]any{
					route.ContentType: map[string]any{
						"schema":            schemaMap,
						"x-static-response": route.Content,
					},
				},
			},
		},
		"parameters": []any{
			map[string]any{
				"name":     "q",
				"in":       "query",
				"required": false,
				"schema":   map[string]any{"type": "string"},
			},
		},
	}

	if method != "get" {
		operation["requestBody"] = map[string]any{
			"required": false,
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
			},
		}
	}

	return operation, nil
}

// MergeStaticIntoSpec overlays static routes on top of an existing
// OpenAPI document. Each route either replaces the spec's (path,
// method) operation (carrying the static body via `x-static-response`)
// or adds a new path/method that wasn't in the spec. The merged
// document is returned as YAML bytes regardless of the input format.
// Mockzilla parses both, and downstream consumers only need to read
// the result.
func MergeStaticIntoSpec(specBytes []byte, routes []Route) ([]byte, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(specBytes, &doc); err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	if doc == nil {
		doc = make(map[string]any)
	}

	paths, _ := doc["paths"].(map[string]any)
	if paths == nil {
		paths = make(map[string]any)
		doc["paths"] = paths
	}

	for _, route := range routes {
		operation, err := buildStaticOperation(route)
		if err != nil {
			return nil, err
		}
		method := strings.ToLower(route.Method)
		pathItem, _ := paths[route.Path].(map[string]any)
		if pathItem == nil {
			pathItem = make(map[string]any)
			paths[route.Path] = pathItem
		}
		pathItem[method] = operation
	}

	out, err := yaml.Dump(doc, yaml.WithIndent(2))
	if err != nil {
		return nil, fmt.Errorf("serialising merged spec: %w", err)
	}
	return out, nil
}

// ScanStatic exposes scanStaticFiles for callers outside this package.
func ScanStatic(dir string) ([]Route, error) {
	return scanStaticFiles(dir)
}

// schemaToMap converts our schema.Schema to a map for OpenAPI spec.
func schemaToMap(s *schema.Schema) map[string]any {
	m := make(map[string]any)

	if s.Type != "" {
		m["type"] = s.Type
	}

	if s.Format != "" {
		m["format"] = s.Format
	}

	if s.Items != nil {
		m["items"] = schemaToMap(s.Items)
	}

	if len(s.Properties) > 0 {
		props := make(map[string]any)
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
	}

	if len(s.Required) > 0 {
		m["required"] = s.Required
	}

	if s.AdditionalProperties != nil {
		m["additionalProperties"] = schemaToMap(s.AdditionalProperties)
	}

	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}

	if s.Example != nil {
		m["example"] = s.Example
	}

	if s.Default != nil {
		m["default"] = s.Default
	}

	if s.Nullable {
		m["nullable"] = true
	}

	if s.Pattern != "" {
		m["pattern"] = s.Pattern
	}

	if s.MinLength != nil {
		m["minLength"] = *s.MinLength
	}

	if s.MaxLength != nil {
		m["maxLength"] = *s.MaxLength
	}

	if s.Minimum != nil {
		m["minimum"] = *s.Minimum
	}

	if s.Maximum != nil {
		m["maximum"] = *s.Maximum
	}

	return m
}

// generateOperationId creates an operation ID from method and path.
// Example: "get", "/users/{id}" -> "getUsers"
func generateOperationId(method, path string) string {
	// Remove leading slash and split by /
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Filter out path parameters and build camelCase name
	var nameParts []string
	for _, part := range parts {
		// Skip path parameters like {id}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}
		// Skip file extensions
		if strings.Contains(part, ".") {
			part = strings.Split(part, ".")[0]
		}
		if part != "" {
			nameParts = append(nameParts, part)
		}
	}

	// Build operation ID: method + CamelCasePath
	if len(nameParts) == 0 {
		return method + "Root"
	}

	// Capitalize first letter of each part
	for i, part := range nameParts {
		if len(part) > 0 {
			nameParts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}

	return method + strings.Join(nameParts, "")
}

// GenerateSpecFromStaticDir generates an OpenAPI spec from a static files directory.
func GenerateSpecFromStaticDir(staticDir, serviceName string) ([]byte, error) {
	// Scan static files
	routes, err := scanStaticFiles(staticDir)
	if err != nil {
		return nil, fmt.Errorf("failed to scan static files: %w", err)
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no static files found in directory: %s", staticDir)
	}

	// Generate OpenAPI spec from routes
	specBytes, err := generateOpenAPIFromStatic(routes, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to generate OpenAPI spec: %w", err)
	}

	return specBytes, nil
}
