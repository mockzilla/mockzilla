package types

import (
	"github.com/mockzilla/mockzilla-codegen/pkg/runtime"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// EncodeFormData encodes data as application/x-www-form-urlencoded using the provided encoding metadata.
func EncodeFormData(data any, encoding map[string]schema.RequestBodyEncoding) (string, error) {
	enc := make(runtime.Encoding, len(encoding))
	for key, e := range encoding {
		enc[key] = runtime.PropertyEncoding{
			ContentType: e.ContentType,
			Style:       runtime.Style(e.Style),
			IsExplode:   e.Explode == nil || *e.Explode,
		}
	}

	return runtime.EncodeForm(data, enc)
}
