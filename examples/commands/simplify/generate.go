package simplify

// Simplifies source.yml by removing union types and limiting optional properties.
// The simplified spec is written to simplified.yml in this directory.
//
// Run: go generate
//
// Compare source.yml and simplified.yml to see what was removed:
// - optional properties with anyOf/oneOf unions (entire property removed)
// - anyOf/oneOf from required properties (union removed, property kept)
//
//go:generate go run github.com/mockzilla/mockzilla/v2/cmd/gen/simplify -output simplified.yml -keep-optional 5 source.yml
