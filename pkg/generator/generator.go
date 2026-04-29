package generator

import (
	"encoding/json"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/contexts"
	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"go.yaml.in/yaml/v4"
)

type Generate interface {
	Request(req *api.GenerateRequest, op *schema.Operation, ctxData map[string]any) json.RawMessage
	Response(respSchema *schema.ResponseSchema, ctxData map[string]any) schema.ResponseData
	Error(errSchema *schema.Schema, errPath, error string) []byte
}

type ResponseGenerator struct {
	serviceContexts []map[string]any
	defaultContexts []map[string]map[string]any
	valueReplacer   replacer.ValueReplacer
}

func (g *ResponseGenerator) Request(req *api.GenerateRequest, op *schema.Operation, ctxData map[string]any) json.RawMessage {
	valueReplacer := g.resolveReplacer(ctxData)

	// static resources.
	if op == nil {
		props := map[string]*schema.Schema{}

		// extract path params from the path
		for _, param := range types.ExtractPlaceholders(req.Path) {
			param = strings.Trim(param, "{}")
			props[param] = &schema.Schema{Type: "any"}
		}

		staticOp := &schema.Operation{
			Path:   req.Path,
			Method: req.Method,
			PathParams: &schema.Schema{
				Type:       "object",
				Properties: props,
			},
		}
		path := generatePath(staticOp, valueReplacer)
		res := map[string]any{
			"path": path,
		}
		jsonBytes, _ := json.Marshal(res)
		return jsonBytes
	}

	res := map[string]any{
		"path":        generatePath(op, valueReplacer),
		"contentType": op.ContentType,
	}

	if op.Headers != nil {
		state := replacer.NewReplaceState(replacer.WithWriteOnly(), replacer.WithHeader())
		headers := generateContentFromSchema(op.Headers, valueReplacer, state)
		if headers != nil {
			res["headers"] = headers
		}
	}

	if op.Body != nil {
		state := replacer.NewReplaceState(replacer.WithWriteOnly())
		body := generateContentFromSchema(op.Body, valueReplacer, state)
		if body != nil {
			// For form-encoded content, encode the body as a form string
			if strings.Contains(strings.ToLower(op.ContentType), "application/x-www-form-urlencoded") {
				formStr, err := types.EncodeFormData(body, op.BodyEncoding)
				if err == nil {
					res["body"] = formStr
				} else {
					// Fallback to the original body if encoding fails
					res["body"] = body
				}
			} else {
				res["body"] = body
			}
		}
	}

	jsonBytes, err := json.Marshal(res)
	if err == nil {
		return jsonBytes
	}

	return nil
}

func (g *ResponseGenerator) Response(respSchema *schema.ResponseSchema, ctxData map[string]any) schema.ResponseData {
	// no response respSchema, nothing to generate
	if respSchema == nil {
		return schema.ResponseData{}
	}

	valueReplacer := g.resolveReplacer(ctxData)

	state := replacer.NewReplaceState(
		replacer.WithContentType(respSchema.ContentType),
		replacer.WithReadOnly())

	content := generateContentFromSchema(respSchema.Body, valueReplacer, state)
	headers := generateHeaders(respSchema.Headers, valueReplacer)

	isError := false
	enc, err := encodeContent(content, respSchema.ContentType)
	if err != nil {
		enc = []byte(err.Error())
		isError = true
	}

	return schema.ResponseData{
		Body:    enc,
		Headers: headers,
		IsError: isError,
	}
}

func (g *ResponseGenerator) Error(errSchema *schema.Schema, errPath, error string) []byte {
	// Default case: no schema or path, return error string directly
	if errSchema == nil || errPath == "" {
		return []byte(error)
	}

	// Response the base structure from the schema
	state := replacer.NewReplaceState()
	content := generateContentFromSchema(errSchema, g.valueReplacer, state)

	// If no content was generated, create an empty object
	var result map[string]any
	if content == nil {
		result = make(map[string]any)
	} else {
		// Try to convert content to map
		var ok bool
		result, ok = content.(map[string]any)
		if !ok {
			// If content is not a map, return error string
			return []byte(error)
		}
	}

	// Inject the error message at the specified path
	types.SetValueByDottedPath(result, errPath, error)

	// Encode the result as JSON (assuming JSON content type for errors)
	encoded, err := encodeContent(result, "application/json")
	if err != nil {
		return []byte(error)
	}

	return encoded
}

// resolveReplacer returns a valueReplacer with the given user context processed and prepended,
// or the default valueReplacer if ctx is nil.
func (g *ResponseGenerator) resolveReplacer(ctxData map[string]any) replacer.ValueReplacer {
	if len(ctxData) == 0 {
		return g.valueReplacer
	}
	yamlBytes, _ := yaml.Marshal(ctxData)
	processed := contexts.Load(map[string][]byte{"user": yamlBytes}, g.defaultContexts)
	orderedCtx := append([]map[string]any{processed["user"]}, g.serviceContexts...)
	return replacer.CreateValueReplacer(replacer.Replacers, orderedCtx)
}

func NewGenerator(orderedCtx []map[string]any, defaultContexts []map[string]map[string]any) (*ResponseGenerator, error) {
	valueReplacer := replacer.CreateValueReplacer(replacer.Replacers, orderedCtx)

	return &ResponseGenerator{
		serviceContexts: orderedCtx,
		defaultContexts: defaultContexts,
		valueReplacer:   valueReplacer,
	}, nil
}
