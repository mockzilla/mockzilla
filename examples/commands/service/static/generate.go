package static

// Generates types, handlers, register.go, and middleware.go from OpenAPI spec.
// All generated code (except middleware.go) is auto-generated and will be overwritten.
// middleware.go is only generated once and can be edited to add custom middleware.
//
// Run after any OpenAPI spec changes: go generate
//
// The command automatically uses setup/codegen.yml and setup/openapi.yml from the current directory.
//go:generate go run github.com/mockzilla/connexions/v2/cmd/gen/service -type static ./setup/data
