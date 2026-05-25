package generator

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"mime"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

func encodeContent(content any, contentType string, xmlRootName ...string) ([]byte, error) {
	if content == nil {
		return nil, nil
	}

	// Pre-serialized content (e.g. static-file responses) passes through as-is.
	if rm, ok := content.(json.RawMessage); ok {
		return rm, nil
	}

	if contentType == "" || isJSONMediaType(contentType) {
		return json.Marshal(content)
	}

	if isNDJSONMediaType(contentType) {
		return encodeNDJSON(content)
	}

	switch contentType {
	case "application/x-www-form-urlencoded",
		"multipart/form-data",
		"multipart/formdata":
		// For mock server: return JSON for easy debugging/display in browser dev tools.
		// Real servers would return proper URL-encoded (key1=value1&key2=value2) or
		// multipart format with boundaries, but JSON is more practical for development.
		res, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		if string(res) == "{}" {
			res = []byte("")
		}
		return res, nil

	case "application/xml":
		root := ""
		if len(xmlRootName) > 0 {
			root = xmlRootName[0]
		}
		if root != "" {
			return marshalGenericXML(content, root)
		}
		return xml.Marshal(content)

	case "application/x-yaml":
		return yaml.Dump(content, yaml.WithIndent(2))

	default:
		switch v := content.(type) {
		case []byte:
			return v, nil
		case string:
			return []byte(v), nil
		}
	}

	return nil, fmt.Errorf("cannot encode type %T with content-type %s", content, contentType)
}

// isJSONMediaType reports whether a Content-Type header value is JSON
// or a JSON-shaped flavour (RFC 6839's `+json` structured suffix, used
// by media types like application/vnd.amadeus+json).
func isJSONMediaType(contentType string) bool {
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return parsed == "application/json" || strings.HasSuffix(parsed, "+json")
}

func isNDJSONMediaType(contentType string) bool {
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch parsed {
	case "application/x-ndjson", "application/ndjson", "application/jsonl", "application/x-jsonlines":
		return true
	}
	return false
}

// encodeNDJSON serializes content as JSON with a trailing newline.
// libopenapi-validator parses NDJSON bodies as a single JSON document;
// splitting an array into newline-delimited lines satisfies real
// streaming clients but breaks the validator. JSON wins.
func encodeNDJSON(content any) ([]byte, error) {
	b, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// marshalGenericXML emits XML for the generator's map/slice/scalar shapes.
// encoding/xml refuses maps directly, so xml.Marshal alone errors on every
// non-struct response. OpenAPI `xml:` metadata (attribute, namespace, …) is ignored.
func marshalGenericXML(content any, rootName string) ([]byte, error) {
	if rootName == "" {
		rootName = "response"
	}
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	if err := writeXMLElement(enc, rootName, content); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeXMLElement(enc *xml.Encoder, name string, v any) error {
	if arr, ok := v.([]any); ok {
		for _, item := range arr {
			if err := writeXMLElement(enc, name, item); err != nil {
				return err
			}
		}
		return nil
	}

	start := xml.StartElement{Name: xml.Name{Local: name}}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}

	switch val := v.(type) {
	case nil:
		// empty element
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := writeXMLElement(enc, k, val[k]); err != nil {
				return err
			}
		}
	default:
		if err := enc.EncodeToken(xml.CharData(fmt.Sprint(val))); err != nil {
			return err
		}
	}

	return enc.EncodeToken(xml.EndElement{Name: start.Name})
}
