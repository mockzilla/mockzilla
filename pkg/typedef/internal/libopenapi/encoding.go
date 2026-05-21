package libopenapi

import (
	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// encodingMap aliases libopenapi's ordered encoding map for readability.
type encodingMap = orderedmap.Map[string, *v3.Encoding]

// contentMap aliases libopenapi's ordered content (MediaType) map for
// readability.
type contentMap = orderedmap.Map[string, *v3.MediaType]

// convertRequestBodyEncoding mirrors libopenapi's encoding map into the
// codegen.RequestBodyEncoding shape schema.Operation still embeds. The
// fields the form encoder actually reads are Style/Explode/ContentType;
// everything else stays zero.
func convertRequestBodyEncoding(encoding *encodingMap) map[string]codegen.RequestBodyEncoding {
	if encoding == nil || encoding.Len() == 0 {
		return nil
	}
	out := make(map[string]codegen.RequestBodyEncoding, encoding.Len())
	for name, enc := range encoding.FromOldest() {
		if enc == nil {
			continue
		}
		out[name] = codegen.RequestBodyEncoding{
			ContentType: enc.ContentType,
			Style:       enc.Style,
			Explode:     enc.Explode,
		}
	}
	return out
}

// convertParameterEncoding builds the codegen.ParameterEncoding entry
// the generator reads for query-string serialisation. It is only
// populated when the parameter declares an explicit style or explode.
func convertParameterEncoding(p *v3.Parameter) *codegen.ParameterEncoding {
	if p == nil {
		return nil
	}
	if p.Style == "" && p.Explode == nil {
		return nil
	}
	return &codegen.ParameterEncoding{
		Style:   p.Style,
		Explode: p.Explode,
	}
}
