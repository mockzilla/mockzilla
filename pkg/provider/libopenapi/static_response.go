package libopenapi

import (
	"fmt"
	"strconv"
	"strings"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

const extStaticResponse = "x-static-response"

// staticResponseKey is the lookup key for a static response on one
// (method, path, status) triple. Same shape as the codegen path uses,
// kept private here since callers go through the Registry.
type staticResponseKey string

func newStaticResponseKey(method, path string, code int) staticResponseKey {
	return staticResponseKey(fmt.Sprintf("%s %s %d", method, path, code))
}

// collectStaticResponses walks an operation's responses and records any
// x-static-response extension values. Called from buildIndex so the
// extraction happens during the same model walk that builds the
// operation index (no separate libopenapi parse).
func (r *Registry) collectStaticResponses(path, method string, op *v3.Operation) {
	if op == nil || op.Responses == nil || op.Responses.Codes == nil {
		return
	}
	for codeStr, resp := range op.Responses.Codes.FromOldest() {
		if resp == nil || resp.Content == nil {
			continue
		}
		code, err := strconv.Atoi(codeStr)
		if err != nil {
			continue
		}
		for _, mt := range resp.Content.FromOldest() {
			if mt == nil || mt.Extensions == nil {
				continue
			}
			ext, ok := mt.Extensions.Get(extStaticResponse)
			if !ok || ext == nil {
				continue
			}
			value := strings.TrimSpace(ext.Value)
			if value == "" {
				continue
			}
			r.staticResponses[newStaticResponseKey(method, path, code)] = value
		}
	}
}
