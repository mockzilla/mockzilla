package libopenapi

import (
	"fmt"
	"mime"
	"strconv"
	"strings"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// convertOperation builds a *schema.Operation for one indexed entry by
// walking the libopenapi *v3.Operation directly. Schema conversion uses
// a fresh convertCtx per call so per-operation caches don't leak across
// independent FindOperation invocations.
func (r *Registry) convertOperation(e *opEntry) *schema.Operation {
	op := e.op
	if op == nil {
		return nil
	}

	ctx := newConvertCtx()
	id := operationID(op, e.method, e.path)
	id = r.uniqueOperationID(id)

	pathSchema, headerSchema, querySchema := convertParameters(op.Parameters, ctx)

	contentType := "application/json"
	var bodySchema *schema.Schema
	var bodyEncoding map[string]codegen.RequestBodyEncoding
	if op.RequestBody != nil && op.RequestBody.Content != nil && op.RequestBody.Content.Len() > 0 {
		mediaType, picked := pickContent(op.RequestBody.Content)
		if mediaType != "" {
			contentType = normaliseJSONMediaType(mediaType)
		}
		if picked != nil {
			bodySchema = convertProxy(picked.Schema, ctx)
			bodyEncoding = convertRequestBodyEncoding(picked.Encoding)
		}
	}

	response := r.convertResponses(op.Responses, e.method, e.path, ctx)

	return &schema.Operation{
		ID:           id,
		Method:       e.method,
		Path:         e.path,
		ContentType:  contentType,
		Headers:      headerSchema,
		PathParams:   pathSchema,
		Query:        querySchema,
		Response:     response,
		Body:         bodySchema,
		BodyEncoding: bodyEncoding,
	}
}

// convertParameters groups OpenAPI parameters by location and converts
// each into the surrounding schema.Schema shapes the generator expects:
// path -> object Schema with one property per param, header -> same,
// query -> QueryParameters map keyed by param name.
func convertParameters(params []*v3.Parameter, ctx *convertCtx) (pathSchema, headerSchema *schema.Schema, query schema.QueryParameters) {
	if len(params) == 0 {
		return nil, nil, nil
	}

	var (
		pathProps   = map[string]*schema.Schema{}
		pathReq     []string
		headerProps = map[string]*schema.Schema{}
		headerReq   []string
		queryParams = schema.QueryParameters{}
	)

	for _, p := range params {
		if p == nil {
			continue
		}
		sub := convertProxy(p.Schema, ctx)
		if sub == nil {
			sub = &schema.Schema{}
		}
		switch p.In {
		case "path":
			pathProps[p.Name] = sub
			if derefBool(p.Required) {
				pathReq = appendUnique(pathReq, p.Name)
			}
		case "header":
			headerProps[p.Name] = sub
			if derefBool(p.Required) {
				headerReq = appendUnique(headerReq, p.Name)
			}
		case "query":
			queryParams[p.Name] = &schema.QueryParameter{
				Schema:   sub,
				Required: derefBool(p.Required),
				Encoding: convertParameterEncoding(p),
			}
		}
	}

	if len(pathProps) > 0 {
		pathSchema = &schema.Schema{
			Type:       "object",
			Properties: pathProps,
			Required:   pathReq,
		}
	}
	if len(headerProps) > 0 {
		headerSchema = &schema.Schema{
			Type:       "object",
			Properties: headerProps,
			Required:   headerReq,
		}
	}
	if len(queryParams) > 0 {
		query = queryParams
	}
	return pathSchema, headerSchema, query
}

// convertResponses walks every declared response and produces the
// schema.Response with .SuccessCode populated to the lowest 2xx (or the
// lowest declared code when no 2xx exists; matches today's behaviour
// before factory.SuccessStatusCode picks a more refined code).
func (r *Registry) convertResponses(responses *v3.Responses, method, path string, ctx *convertCtx) *schema.Response {
	all := map[int]*schema.ResponseItem{}

	if responses != nil && responses.Codes != nil {
		for codeStr, resp := range responses.Codes.FromOldest() {
			code, ok := parseStatusCode(codeStr)
			if !ok {
				continue
			}
			item := r.convertResponseItem(code, resp, ctx)
			if static, ok := r.staticResponses[newStaticResponseKey(method, path, code)]; ok && item != nil {
				if item.Content == nil {
					item.Content = &schema.Schema{}
				}
				item.Content.StaticContent = static
			}
			all[code] = item
		}
	}

	// When no 2xx is declared, fabricate an entry to serve as SuccessCode.
	// If a `default` response is declared, inherit its content/headers so
	// the response writer picks the right content-type and schema; otherwise
	// the entry stays empty. factory.SuccessStatusCode still substitutes a
	// spec-declared code for the HTTP status line.
	successCode := pickSuccessCode(all)
	if successCode < 200 || successCode >= 300 {
		fabricated := &schema.ResponseItem{StatusCode: 204}
		if responses != nil && responses.Default != nil {
			if item := r.convertResponseItem(204, responses.Default, ctx); item != nil {
				fabricated.ContentType = item.ContentType
				fabricated.Content = item.Content
				fabricated.Headers = item.Headers
			}
		}
		all[204] = fabricated
		successCode = 204
	}
	return schema.NewResponse(all, successCode)
}

func (r *Registry) convertResponseItem(code int, resp *v3.Response, ctx *convertCtx) *schema.ResponseItem {
	if resp == nil {
		return &schema.ResponseItem{StatusCode: code}
	}

	contentType := ""
	var contentSchema *schema.Schema
	if resp.Content != nil && resp.Content.Len() > 0 {
		mediaType, picked := pickContent(resp.Content)
		contentType = mediaType
		if picked != nil {
			contentSchema = convertProxy(picked.Schema, ctx)
		}
	}

	headers := map[string]*schema.Schema{}
	if resp.Headers != nil {
		for k, h := range resp.Headers.FromOldest() {
			if h == nil {
				continue
			}
			if sub := convertProxy(h.Schema, ctx); sub != nil {
				headers[k] = sub
			}
		}
	}

	return &schema.ResponseItem{
		StatusCode:  code,
		ContentType: contentType,
		Content:     contentSchema,
		Headers:     headers,
	}
}

// pickContent returns one preferred media type entry from a content
// map. JSON shapes win; otherwise the first entry is used.
func pickContent(content *contentMap) (string, *v3.MediaType) {
	if content == nil || content.Len() == 0 {
		return "", nil
	}
	var firstKey string
	var firstMT *v3.MediaType
	for k, mt := range content.FromOldest() {
		if firstKey == "" {
			firstKey = k
			firstMT = mt
		}
		if isMediaTypeJSON(k) {
			return k, mt
		}
	}
	return firstKey, firstMT
}

func pickSuccessCode(all map[int]*schema.ResponseItem) int {
	var min2xx, min3xx, min4xx, min5xx int
	for code := range all {
		switch {
		case code >= 200 && code < 300:
			if min2xx == 0 || code < min2xx {
				min2xx = code
			}
		case code >= 300 && code < 400:
			if min3xx == 0 || code < min3xx {
				min3xx = code
			}
		case code >= 400 && code < 500:
			if min4xx == 0 || code < min4xx {
				min4xx = code
			}
		case code >= 500:
			if min5xx == 0 || code < min5xx {
				min5xx = code
			}
		}
	}
	switch {
	case min2xx > 0:
		return min2xx
	case min3xx > 0:
		return min3xx
	case min4xx > 0:
		return min4xx
	case min5xx > 0:
		return min5xx
	}
	return 0
}

// uniqueOperationID disambiguates synthesised IDs the same way
// pkg/typedef/registry.go does: identical names get a trailing counter.
func (r *Registry) uniqueOperationID(id string) string {
	if id == "" {
		return id
	}
	if r.usedIDs == nil {
		r.usedIDs = map[string]int{}
	}
	if count, ok := r.usedIDs[id]; ok {
		r.usedIDs[id] = count + 1
		return fmt.Sprintf("%s%d", id, count+1)
	}
	r.usedIDs[id] = 1
	return id
}

// operationID resolves the operation's identifier. The spec's literal
// operationId wins when present so codegen filtering keeps working.
// Otherwise we synthesise a lowercase underscore form: GET /users/{id}
// becomes `get_users_id`.
func operationID(op *v3.Operation, method, path string) string {
	if op != nil && op.OperationId != "" {
		return op.OperationId
	}
	id := strings.ToLower(method) + "_" + strings.ReplaceAll(strings.ReplaceAll(path, "/", "_"), "{", "")
	id = strings.ReplaceAll(id, "}", "")
	return id
}

// parseStatusCode parses a response key from the spec. Plain numeric
// codes ("200") return as-is; OpenAPI range patterns ("2XX", "5XX")
// map to the lowest concrete code in the range so the rest of the
// pipeline can treat them uniformly. Anything else (e.g. "default")
// is rejected.
func parseStatusCode(s string) (int, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	if len(s) == 3 && (s[1] == 'X' || s[1] == 'x') && (s[2] == 'X' || s[2] == 'x') {
		switch s[0] {
		case '1':
			return 100, true
		case '2':
			return 200, true
		case '3':
			return 300, true
		case '4':
			return 400, true
		case '5':
			return 500, true
		}
	}
	return 0, false
}

func normaliseJSONMediaType(mt string) string {
	if isMediaTypeJSON(mt) {
		return "application/json"
	}
	return mt
}

func isMediaTypeJSON(mediaType string) bool {
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return false
	}
	return parsed == "application/json" || strings.HasSuffix(parsed, "+json")
}
