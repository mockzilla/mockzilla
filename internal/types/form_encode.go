package types

import (
	"github.com/mockzilla/mockzilla/v2/internal/formenc"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// EncodeFormData encodes data as application/x-www-form-urlencoded using the provided encoding metadata.
func EncodeFormData(data any, encoding map[string]schema.RequestBodyEncoding) (string, error) {
	fields := make(map[string]formenc.FieldEncoding, len(encoding))
	for key, enc := range encoding {
		fields[key] = formenc.FieldEncoding{Style: enc.Style, Explode: enc.Explode}
	}

	return formenc.Encode(data, fields)
}
