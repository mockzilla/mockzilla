package libopenapi

import "fmt"

const extStaticResponse = "x-static-response"

// staticResponseKey is the lookup key for an x-static-response value on
// one (method, path, status) triple.
type staticResponseKey string

func newStaticResponseKey(method, path string, code int) staticResponseKey {
	return staticResponseKey(fmt.Sprintf("%s %s %d", method, path, code))
}
