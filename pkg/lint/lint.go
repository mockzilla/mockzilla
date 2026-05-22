// Package lint flags OpenAPI specs that contain constructs the
// libopenapi-validator rejects as unsatisfiable. Specs ship in the wild with
// these defects (real examples: dracoon's array-level enum, atlassian-jira's
// additionalProperties:false + oneOf, zuora's allOf + additionalProperties
// boolean clash), so a clean lint pass tells callers up-front "this spec is
// broken; don't bother validating responses against it" instead of opaque
// 500s mid-run.
//
// The rules are deliberately conservative: they only match shapes we've
// observed in practice and confirmed are strictly-unsatisfiable per JSON
// Schema. Rule names are stable identifiers so callers can filter, suppress,
// or count specific categories.
//
// Files in this package:
//   - lint.go (this file): public API + spec walker
//   - rules.go: registry + individual rule detectors
package lint

import (
	"fmt"
	"os"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Defect describes one location in a spec that fails strict JSON Schema
// validation. Path is the YAML pointer (e.g. `components.schemas.Foo`),
// Detail is a short human-readable description.
type Defect struct {
	Rule   string
	Path   string
	Detail string
}

// Spec parses the OpenAPI document at the given path and returns all
// defects across operations and component schemas. Returns an error only
// when the file is unreadable or unparsable; a spec that loads but is
// internally broken yields defects, not an error.
func Spec(path string) ([]Defect, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	v3model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("build v3 model: %w", err)
	}
	if v3model == nil {
		return nil, fmt.Errorf("empty v3 model")
	}
	w := walker{seen: map[*base.Schema]bool{}, seenRefs: map[string]bool{}}
	w.walkV3(&v3model.Model)
	return w.defects, nil
}

// walker carries the accumulating defect list and visited sets. We dedupe
// by *Schema pointer for inline schemas, and by $ref string for refs:
// each SchemaProxy on a ref builds its own underlying *Schema, so pointer
// identity alone misses cycles like treeNode → children.items.$ref →
// treeNode.
type walker struct {
	defects  []Defect
	seen     map[*base.Schema]bool
	seenRefs map[string]bool
}

func (w *walker) walkV3(doc *v3.Document) {
	if doc == nil {
		return
	}
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, proxy := range doc.Components.Schemas.FromOldest() {
			w.walkProxy(proxy, "components.schemas."+name)
		}
	}
	if doc.Paths != nil && doc.Paths.PathItems != nil {
		for path, item := range doc.Paths.PathItems.FromOldest() {
			w.walkPathItem(item, "paths."+path)
		}
	}
}

func (w *walker) walkPathItem(item *v3.PathItem, base string) {
	if item == nil {
		return
	}
	w.walkParameters(item.Parameters, base+".parameters")

	ops := map[string]*v3.Operation{
		"get": item.Get, "post": item.Post, "put": item.Put, "delete": item.Delete,
		"patch": item.Patch, "head": item.Head, "options": item.Options, "trace": item.Trace,
	}

	for method, op := range ops {
		if op == nil {
			continue
		}
		prefix := base + "." + method

		w.walkParameters(op.Parameters, prefix+".parameters")

		if op.RequestBody != nil && op.RequestBody.Content != nil {
			for ct, mt := range op.RequestBody.Content.FromOldest() {
				w.walkProxy(mt.Schema, prefix+".requestBody.content."+ct+".schema")
			}
		}
		if op.Responses != nil && op.Responses.Codes != nil {
			for code, resp := range op.Responses.Codes.FromOldest() {
				if resp.Content == nil {
					continue
				}
				for ct, mt := range resp.Content.FromOldest() {
					w.walkProxy(mt.Schema, prefix+".responses."+code+".content."+ct+".schema")
				}
			}
		}
	}
}

// walkParameters flags parameters that carry neither `schema` nor a
// non-empty `content` - the symptom of an OAS-2.0-style parameter (with
// top-level `type`/`enum`) that libopenapi parses as an empty 3.0
// Parameter. libopenapi-validator then dereferences the nil schema and
// panics on every request to the route. Marking the spec lint-bad
// short-circuits that before the test even tries the route. Note that
// libopenapi initialises Content to an empty *orderedmap (not nil), so
// nil-vs-empty matters.
func (w *walker) walkParameters(params []*v3.Parameter, basePath string) {
	for _, p := range params {
		if p == nil {
			continue
		}
		hasContent := p.Content != nil && p.Content.Len() > 0
		if p.Schema == nil && !hasContent {
			w.defects = append(w.defects, Defect{
				Rule:   "param-missing-schema",
				Path:   basePath + "." + p.Name,
				Detail: "parameter has neither `schema` nor `content` (likely OAS 2.0 style with top-level type/enum on the parameter object)",
			})
			continue
		}
		if p.Schema != nil {
			w.walkProxy(p.Schema, basePath+"."+p.Name+".schema")
		}
	}
}

func (w *walker) walkProxy(proxy *base.SchemaProxy, path string) {
	if proxy == nil {
		return
	}
	if proxy.IsReference() {
		ref := proxy.GetReference()
		if w.seenRefs[ref] {
			return
		}
		w.seenRefs[ref] = true
	}
	s := proxy.Schema()
	if s == nil || w.seen[s] {
		return
	}
	w.seen[s] = true
	w.check(s, path)

	// Recurse into sub-schemas. Children inherit the path so defects
	// surface where the offending shape lives, not where it was first
	// dereferenced.
	if s.Properties != nil {
		for k, p := range s.Properties.FromOldest() {
			w.walkProxy(p, path+".properties."+k)
		}
	}
	if s.Items != nil && s.Items.A != nil {
		w.walkProxy(s.Items.A, path+".items")
	}
	if s.AdditionalProperties != nil && s.AdditionalProperties.A != nil {
		w.walkProxy(s.AdditionalProperties.A, path+".additionalProperties")
	}

	for i, p := range s.AllOf {
		w.walkProxy(p, fmt.Sprintf("%s.allOf[%d]", path, i))
	}

	for i, p := range s.OneOf {
		w.walkProxy(p, fmt.Sprintf("%s.oneOf[%d]", path, i))
	}

	for i, p := range s.AnyOf {
		w.walkProxy(p, fmt.Sprintf("%s.anyOf[%d]", path, i))
	}
}

func (w *walker) check(s *base.Schema, path string) {
	for _, rule := range rules {
		w.defects = append(w.defects, rule(s, path)...)
	}
}
