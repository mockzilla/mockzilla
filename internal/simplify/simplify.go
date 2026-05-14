// Package simplify is the pure-logic core of the `mockzilla simplify` command.
//
// It exposes a single Simplify entry point that takes an OpenAPI spec as bytes
// and returns a simplified YAML document as bytes — no flag parsing, no file
// I/O, no cobra. The CLI wrapper that wires these into a user-facing command
// lives in cmd/mockzilla (simplify.go).
package simplify

import (
	"fmt"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"go.yaml.in/yaml/v4"
)

// Options controls how Simplify treats the input spec.
type Options struct {
	// ConfigYAML is an optional oapi-codegen-dd codegen.yml. When non-empty,
	// the spec is run through filter + overlay + prune before simplification
	// (so callers can include/exclude paths/tags/operation-ids, apply OpenAPI
	// Overlay 1.0 deltas, and drop dangling refs).
	ConfigYAML []byte

	// OptionalProperties controls how optional schema properties are pruned:
	//   nil               keep every optional property
	//   &{Min:0, Max:0}   drop every optional property
	//   &{Min:N, Max:N}   keep exactly N optional properties
	//   &{Min:A, Max:B}   keep a random number in [A,B] per schema
	// (Seed reproduces the random selection — leave 0 for time-based.)
	OptionalProperties *typedef.OptionalPropertyConfig
}

// Simplify reads an OpenAPI spec, removes anyOf/oneOf unions, strips schema-
// level x-* extensions, and optionally limits optional properties per the
// supplied Options. The output is YAML, indented to match the source spec
// (falling back to two spaces if the source indent is unknown).
//
// Examples are deliberately preserved to avoid breaking $ref targets pointing
// at components/examples.
func Simplify(specBytes []byte, opts Options) ([]byte, error) {
	doc, err := loadDocument(specBytes, opts.ConfigYAML)
	if err != nil {
		return nil, fmt.Errorf("loading OpenAPI spec: %w", err)
	}

	model, err := typedef.BuildModel(doc, true, opts.OptionalProperties)
	if err != nil {
		return nil, fmt.Errorf("simplifying document: %w", err)
	}

	// libopenapi's v3 Render() goes through yaml.Marshal which defaults to
	// 4-space indent; that bloats 2-space specs (most hand-written ones) on
	// round-trip even when the simplifier makes no structural changes.
	indent := doc.GetSpecInfo().OriginalIndentation
	if indent <= 0 {
		indent = 2
	}
	return model.RenderWithIndention(indent), nil
}

// loadDocument parses the OpenAPI bytes and, when configYAML is non-empty,
// runs them through oapi-codegen-dd's filter + overlay + prune pipeline
// before returning. Without a config it falls back to a plain libopenapi
// document with circular-ref check disabled.
func loadDocument(specBytes, configYAML []byte) (libopenapi.Document, error) {
	if len(configYAML) == 0 {
		return libopenapi.NewDocumentWithConfiguration(specBytes, &datamodel.DocumentConfiguration{
			SkipCircularReferenceCheck: true,
		})
	}

	var cfg codegen.Configuration
	if err := yaml.Unmarshal(configYAML, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg = cfg.WithDefaults()

	return codegen.CreateDocument(specBytes, cfg)
}
