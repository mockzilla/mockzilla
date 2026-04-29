// Package users This file is generated ONCE as a starting point and will NOT be overwritten.
// Modify it freely to add your business logic.
// To regenerate, delete this file or set generate.handler.output.overwrite: true in config.
package users

import (
	"context"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
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

// ListUsers handles GET /users
func (s *service) ListUsers(ctx context.Context) (*ListUsersResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUser handles GET /users/{id}
func (s *service) GetUser(ctx context.Context, opts *GetUserServiceRequestOptions) (*GetUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserAvatar handles GET /users/{id}/avatar
func (s *service) GetUserAvatar(ctx context.Context, opts *GetUserAvatarServiceRequestOptions) (*GetUserAvatarResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserProfile handles GET /users/{id}/profile
func (s *service) GetUserProfile(ctx context.Context, opts *GetUserProfileServiceRequestOptions) (*GetUserProfileResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// ExportUsers handles GET /users/export
func (s *service) ExportUsers(ctx context.Context) (*ExportUsersResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserConfig handles GET /users/{id}/config
func (s *service) GetUserConfig(ctx context.Context, opts *GetUserConfigServiceRequestOptions) (*GetUserConfigResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserAPIData handles GET /users/{id}/api-data
func (s *service) GetUserAPIData(ctx context.Context, opts *GetUserAPIDataServiceRequestOptions) (*GetUserAPIDataResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserHal handles GET /users/{id}/hal
func (s *service) GetUserHal(ctx context.Context, opts *GetUserHalServiceRequestOptions) (*GetUserHalResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserProblem handles GET /users/{id}/problem
func (s *service) GetUserProblem(ctx context.Context, opts *GetUserProblemServiceRequestOptions) (*GetUserProblemResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// StreamUsers handles GET /users/stream
func (s *service) StreamUsers(ctx context.Context) (*StreamUsersResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserPdf handles GET /users/{id}/pdf
func (s *service) GetUserPdf(ctx context.Context, opts *GetUserPdfServiceRequestOptions) (*GetUserPdfResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}
