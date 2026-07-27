package generator

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// skipHeaders are managed by the HTTP transport or the handler; any
// spec-generated value would conflict with the real one.
var skipHeaders = map[string]bool{
	"content-encoding":  true,
	"content-length":    true,
	"transfer-encoding": true,
	"content-type":      true,
}

// generateHeaders generates response headers from the given headers.
// It filters out headers that would mislead HTTP clients about the response encoding
// or content length, since these are managed by the HTTP transport layer.
func generateHeaders(headers map[string]*schema.Schema, valueReplacer replacer.ValueReplacer, opts ...replacer.ReplaceStateOption) http.Header {
	res := http.Header{}

	for name, s := range headers {
		name = strings.ToLower(name)

		// Skip headers that are managed by the HTTP transport layer
		if skipHeaders[name] {
			continue
		}

		state := replacer.NewReplaceState(append([]replacer.ReplaceStateOption{
			replacer.WithName(name), replacer.WithHeader(),
		}, opts...)...)

		value := generateContentFromSchema(s, valueReplacer, state)
		res.Set(name, fmt.Sprintf("%v", value))
	}
	return res
}
