package libopenapi

import (
	"mime"
	"strconv"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

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
	codes := make([]int, 0, len(all))
	for k := range all {
		codes = append(codes, k)
	}
	min2xx, min3xx, min4xx, min5xx := schema.MinResponseCodes(codes)
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

// operationID returns the spec's operationId if set, otherwise
// synthesises a lowercase underscore form (GET /users/{id} → get_users_id).
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
