package libopenapi

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

const extStaticResponse = "x-static-response"

// staticResponseKey is the lookup key for an x-static-response value on
// one (method, path, status) triple.
type staticResponseKey string

func newStaticResponseKey(method, path string, code int) staticResponseKey {
	return staticResponseKey(fmt.Sprintf("%s %s %d", method, path, code))
}

// staticExtension returns the trimmed x-static-response value from a
// media type's or header's extensions, or "" when absent.
func staticExtension(extensions *orderedmap.Map[string, *yaml.Node]) string {
	if extensions == nil {
		return ""
	}

	ext, ok := extensions.Get(extStaticResponse)
	if !ok || ext == nil {
		return ""
	}

	return strings.TrimSpace(ext.Value)
}
