package api

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

const multipartPrefix = "multipart/"

// inlineMultipartBodies rewrites every multipart request body whose schema is a
// reference or an allOf into the equivalent inline object.
//
// oapi-codegen emits the form-field binding from the properties it finds on the
// operation's own body schema, and a referenced schema carries none of its own.
// The generated handler then parses the form, declares the body struct and hands
// it over without ever writing to it, so an upload arrives as a zero value.
//
// It runs on the spec as written rather than on the filtered and overlaid
// document the generator builds from, because rendering that document back to
// bytes leaves behind references its own pruning has already removed.
func inlineMultipartBodies(specContents []byte) ([]byte, error) {
	doc, err := libopenapi.NewDocumentWithConfiguration(specContents, &datamodel.DocumentConfiguration{
		SkipCircularReferenceCheck: true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating document: %w", err)
	}

	model, errs := doc.BuildV3Model()
	if errs != nil {
		return nil, fmt.Errorf("building model: %w", errs)
	}

	rewritten := false
	for _, item := range allPathItems(&model.Model) {
		for _, op := range item.GetOperations().FromOldest() {
			if inlineOperationBody(op) {
				rewritten = true
			}
		}
	}

	// A spec with nothing to rewrite reaches the generator exactly as written.
	if !rewritten {
		return specContents, nil
	}

	indent := doc.GetSpecInfo().OriginalIndentation
	if indent <= 0 {
		indent = 2
	}
	return model.Model.RenderWithIndention(indent), nil
}

func allPathItems(model *v3high.Document) []*v3high.PathItem {
	if model.Paths == nil || model.Paths.PathItems == nil {
		return nil
	}
	return slices.Collect(model.Paths.PathItems.ValuesFromOldest())
}

func inlineOperationBody(op *v3high.Operation) bool {
	if op == nil || op.RequestBody == nil || op.RequestBody.Content == nil {
		return false
	}

	rewritten := false
	for contentType, media := range op.RequestBody.Content.FromOldest() {
		if !strings.HasPrefix(contentType, multipartPrefix) || media.Schema == nil {
			continue
		}
		if inline := inlineSchema(media.Schema); inline != nil {
			media.Schema = inline
			rewritten = true
		}
	}
	return rewritten
}

// inlineSchema returns the flattened stand-in for a body schema, or nil when the
// generator can already see the properties.
func inlineSchema(proxy *base.SchemaProxy) *base.SchemaProxy {
	schema := proxy.Schema()
	if schema == nil {
		return nil
	}
	if !proxy.IsReference() && len(schema.AllOf) == 0 {
		return nil
	}

	properties := orderedmap.New[string, *base.SchemaProxy]()
	var required []string
	collectProperties(schema, properties, &required, make(map[*base.Schema]bool))
	if properties.Len() == 0 {
		return nil
	}

	return base.CreateSchemaProxy(&base.Schema{
		Type:        []string{"object"},
		Description: schema.Description,
		Properties:  properties,
		Required:    required,
	})
}

func collectProperties(schema *base.Schema, into *orderedmap.Map[string, *base.SchemaProxy], required *[]string, seen map[*base.Schema]bool) {
	if schema == nil || seen[schema] {
		return
	}
	seen[schema] = true

	for _, member := range schema.AllOf {
		collectProperties(member.Schema(), into, required, seen)
	}

	for name, prop := range schema.Properties.FromOldest() {
		into.Set(name, prop)
	}

	for _, name := range schema.Required {
		if !slices.Contains(*required, name) {
			*required = append(*required, name)
		}
	}
}
