package libopenapi

import (
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// encodingMap aliases libopenapi's ordered encoding map for readability.
type encodingMap = orderedmap.Map[string, *v3.Encoding]

// contentMap aliases libopenapi's ordered content (MediaType) map for
// readability.
type contentMap = orderedmap.Map[string, *v3.MediaType]

// convertRequestBodyEncoding mirrors libopenapi's encoding map into
// schema.RequestBodyEncoding. The form encoder reads Style and Explode.
func convertRequestBodyEncoding(encoding *encodingMap) map[string]schema.RequestBodyEncoding {
	if encoding == nil || encoding.Len() == 0 {
		return nil
	}
	out := make(map[string]schema.RequestBodyEncoding, encoding.Len())
	for name, enc := range encoding.FromOldest() {
		if enc == nil {
			continue
		}
		out[name] = schema.RequestBodyEncoding{
			ContentType: enc.ContentType,
			Style:       enc.Style,
			Explode:     enc.Explode,
		}
	}
	return out
}

// convertParameterEncoding builds the schema.ParameterEncoding entry
// the generator reads for query-string serialisation. It is only
// populated when the parameter declares an explicit style or explode.
func convertParameterEncoding(p *v3.Parameter) *schema.ParameterEncoding {
	if p == nil {
		return nil
	}
	if p.Style == "" && p.Explode == nil {
		return nil
	}
	return &schema.ParameterEncoding{
		Style:   p.Style,
		Explode: p.Explode,
	}
}
