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
}

// httpMethods is the set of recognized HTTP methods used to identify method directories.
var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "patch": true,
	"delete": true, "head": true, "options": true, "trace": true,
}

// scanStaticFiles scans a static directory and returns all routes.
// Directory structure: <staticDir>/<path>/<method>/index.<ext>
// Example: data/users/get/index.json -> GET /users
func scanStaticFiles(staticDir string) ([]Route, error) {
	var routes []Route

	err := filepath.Walk(staticDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get extension and check if it's a supported content type
		ext := filepath.Ext(info.Name())
		contentType := getContentType(ext)
		if contentType == "" {
			return nil
		}

		// Get the path relative to static directory
		relPath, err := filepath.Rel(staticDir, path)
		if err != nil {
			return err
		}

		// Split into segments: [...pathSegments, method, filename]
		segments := strings.Split(filepath.ToSlash(relPath), "/")
		if len(segments) < 2 {
			return nil
		}

		// The method is the parent directory of the file
		methodDir := segments[len(segments)-2]
		if !httpMethods[strings.ToLower(methodDir)] {
			return nil
		}
		method := strings.ToUpper(methodDir)

		// Path segments are everything before the method directory
		pathSegments := segments[:len(segments)-2]

		// Build URL path
		urlPath := "/"
		if len(pathSegments) > 0 {
			urlPath = "/" + strings.Join(pathSegments, "/")
		}

		// Get filename without extension
		filename := strings.TrimSuffix(info.Name(), ext)

		// If filename is not "index", append it to the URL path
		if filename != "index" {
			if urlPath == "/" {
				urlPath = "/" + info.Name()
			} else {
				urlPath = urlPath + "/" + info.Name()
			}
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading file %s: %w", path, err)
		}

		contentStr := strings.TrimRight(string(content), "\n\r\t ")

		routes = append(routes, Route{
			Method:      method,
			Path:        urlPath,
			ContentType: contentType,
			Content:     contentStr,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking static directory: %w", err)
	}

	return routes, nil
}

// getContentType returns the content type for a file extension.
// Returns empty string for unsupported extensions.
func getContentType(ext string) string {
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

		// Infer schema from content
		responseSchema, err := schema.BuildSchemaFromContent([]byte(route.Content), route.ContentType)
		if err != nil {
			return nil, fmt.Errorf("failed to build schema for %s %s: %w", route.Method, route.Path, err)
		}

		// Convert our schema to OpenAPI schema map
		schemaMap := schemaToMap(responseSchema)

		// Generate operation ID from method and path
		operationId := generateOperationId(method, path)

		// Create operation
		operation := map[string]any{
			"operationId": operationId,
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
		}

		// Add an optional query parameter so all methods get ServiceRequestOptions
		// with RawRequest, giving access to headers, query parameters, and the raw HTTP request.
		operation["parameters"] = []any{
			map[string]any{
				"name":     "q",
				"in":       "query",
				"required": false,
				"schema":   map[string]any{"type": "string"},
			},
		}

		// For non-GET methods, also accept any request body.
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
