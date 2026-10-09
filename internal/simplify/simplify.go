// Package simplify is the pure-logic core of the `mockzilla simplify` command.
//
// Simplify takes an OpenAPI spec as bytes and returns simplified YAML bytes;
// no flag parsing, no file I/O. The CLI wrapper lives in cmd/mockzilla/simplify.go.
package simplify

import (
	"context"
	"fmt"

	"github.com/mockzilla/mockzilla-codegen/pkg/codegen"
	codegenconfig "github.com/mockzilla/mockzilla-codegen/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
)

// Options controls how Simplify treats the input spec.
type Options struct {
	// ConfigYAML is an optional mockzilla-codegen config. When non-empty,
	// the spec is prepared as its spec.overlays, spec.filter and spec.prune
	// say before simplification (so callers can include/exclude
	// paths/tags/operation-ids, apply OpenAPI Overlay 1.0 deltas, and drop
	// dangling refs).
	ConfigYAML []byte

	// ConfigDir is the folder relative paths in ConfigYAML resolve against.
	ConfigDir string

	// OptionalProperties controls pruning of optional schema properties:
	//   nil               keep every optional property
	//   &{Min:0, Max:0}   drop every optional property
	//   &{Min:N, Max:N}   keep exactly N optional properties
	//   &{Min:A, Max:B}   keep a random number in [A,B] per schema
	OptionalProperties *config.OptionalProperties
}

// Simplify reads an OpenAPI spec, removes anyOf/oneOf unions, strips schema-
// level x-* extensions, and optionally limits optional properties per the
// supplied Options. The output is YAML, indented to match the source spec
// (falling back to two spaces if the source indent is unknown).
//
// Examples are deliberately preserved to avoid breaking $ref targets pointing
// at components/examples.
func Simplify(specBytes []byte, opts Options) ([]byte, error) {
	doc, err := loadDocument(specBytes, opts.ConfigYAML, opts.ConfigDir)
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
// prepares them with the mockzilla-codegen config (overlays, filter, prune)
// before returning. The document has the circular-ref check disabled.
func loadDocument(specBytes, configYAML []byte, configDir string) (libopenapi.Document, error) {
	if len(configYAML) > 0 {
		cfg, err := codegenconfig.Parse(configYAML, configDir)
		if err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}

		specBytes, _, err = codegen.Prepare(context.Background(), cfg, codegen.WithSpec(specBytes))
		if err != nil {
			return nil, fmt.Errorf("preparing spec: %w", err)
		}
	}

	return libopenapi.NewDocumentWithConfiguration(specBytes, &datamodel.DocumentConfiguration{
		SkipCircularReferenceCheck: true,
	})
}
