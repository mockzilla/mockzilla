package generator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mockzilla/mockzilla-codegen/pkg/runtime"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
)

// ResponseOption configures a single response generation.
type ResponseOption func(*responseOptions)

type responseOptions struct {
	request *http.Request
}

// WithRequest exposes the incoming request to `request:` context values, whose
// dotted paths are resolved against its payload.
func WithRequest(r *http.Request) ResponseOption {
	return func(o *responseOptions) {
		o.request = r
	}
}

// requestPayload decodes the request body into the structure `request:` context
// paths navigate. The Content-Type header decides how the body is read: form
// bodies go through the same form encoding mockzilla generates them with, so
// deepObject keys (`order[payment][currency]`) and repeated keys land as nested
// objects and arrays. Returns nil when there is nothing usable to navigate.
func requestPayload(r *http.Request) any {
	if r == nil {
		return nil
	}

	body := api.RequestBody(r)
	if len(body) == 0 {
		return nil
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		var form map[string]any
		if err := runtime.DecodeForm(bytes.NewReader(body), &form, false, nil); err != nil {
			return nil
		}
		return form
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	return payload
}
