// Package static This file is generated ONCE as a starting point and will NOT be overwritten.
// Modify it freely to add your business logic.
// To regenerate, delete this file or set generate.handler.output.overwrite: true in config.
package static

import (
	"context"

	"github.com/mockzilla/connexions/v2/pkg/api"
)

// service implements the ServiceInterface with your business logic.
// Return nil, nil to fall back to the generator for mock responses.
// Return a response to override the generated response.
// Return an error to return an error response.
type service struct {
	params *api.ServiceParams
}

// Ensure service implements ServiceInterface.
var _ ServiceInterface = (*service)(nil)

// newService creates a new service instance.
func newService(params *api.ServiceParams) *service {
	return &service{params: params}
}

// PostFooBar handles POST /foo/bar
func (s *service) PostFooBar(ctx context.Context, opts *PostFooBarServiceRequestOptions) (*PostFooBarResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}
