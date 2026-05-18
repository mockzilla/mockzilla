package generator

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// skipHeaders contains headers that should not be generated from the spec.
// These headers are managed by the HTTP server/transport layer or by
// mockzilla itself, and a spec-generated value would conflict with the
// real one:
// - Content-Encoding: We don't compress responses, so "gzip" causes "invalid header" errors
// - Content-Length: Spec values don't match actual body size, causing "unexpected EOF" errors
// - Transfer-Encoding: We don't use chunked encoding, causing parsing errors
// - Content-Type: Set from the response media type by the handler;
//   generating a random string from a `type: string` schema (as GitHub's
//   spec declares for many responses) would override the real media type
//   and break response validation.
var skipHeaders = map[string]bool{
	"content-encoding":  true,
	"content-length":    true,
	"transfer-encoding": true,
	"content-type":      true,
}

// generateHeaders generates response headers from the given headers.
// It filters out headers that would mislead HTTP clients about the response encoding
// or content length, since these are managed by the HTTP transport layer.
func generateHeaders(headers map[string]*schema.Schema, valueReplacer replacer.ValueReplacer) http.Header {
	res := http.Header{}

	for name, s := range headers {
		name = strings.ToLower(name)

		// Skip headers that are managed by the HTTP transport layer
		if skipHeaders[name] {
			continue
		}

		state := replacer.NewReplaceState(replacer.WithName(name), replacer.WithHeader())

		value := generateContentFromSchema(s, valueReplacer, state)
		res.Set(name, fmt.Sprintf("%v", value))
	}
	return res
}
