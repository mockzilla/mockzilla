// Package petstore This file is generated ONCE as a starting point and will NOT be overwritten.
// Modify it freely to add your business logic.
// To regenerate, delete this file or set generate.handler.output.overwrite: true in config.
package petstore

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

// UpdatePet handles PUT /pet
func (s *service) UpdatePet(ctx context.Context, opts *UpdatePetServiceRequestOptions) (*UpdatePetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// AddPet handles POST /pet
func (s *service) AddPet(ctx context.Context, opts *AddPetServiceRequestOptions) (*AddPetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// FindPetsByStatus handles GET /pet/findByStatus
func (s *service) FindPetsByStatus(ctx context.Context, opts *FindPetsByStatusServiceRequestOptions) (*FindPetsByStatusResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// FindPetsByTags handles GET /pet/findByTags
func (s *service) FindPetsByTags(ctx context.Context, opts *FindPetsByTagsServiceRequestOptions) (*FindPetsByTagsResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetPetByID handles GET /pet/{petId}
func (s *service) GetPetByID(ctx context.Context, opts *GetPetByIDServiceRequestOptions) (*GetPetByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// UpdatePetWithForm handles POST /pet/{petId}
func (s *service) UpdatePetWithForm(ctx context.Context, opts *UpdatePetWithFormServiceRequestOptions) (*UpdatePetWithFormResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DeletePet handles DELETE /pet/{petId}
func (s *service) DeletePet(ctx context.Context, opts *DeletePetServiceRequestOptions) (*DeletePetResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// UploadFile handles POST /pet/{petId}/uploadImage
func (s *service) UploadFile(ctx context.Context, opts *UploadFileServiceRequestOptions) (*UploadFileResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetInventory handles GET /store/inventory
func (s *service) GetInventory(ctx context.Context) (*GetInventoryResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// PlaceOrder handles POST /store/order
func (s *service) PlaceOrder(ctx context.Context, opts *PlaceOrderServiceRequestOptions) (*PlaceOrderResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetOrderByID handles GET /store/order/{orderId}
func (s *service) GetOrderByID(ctx context.Context, opts *GetOrderByIDServiceRequestOptions) (*GetOrderByIDResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DeleteOrder handles DELETE /store/order/{orderId}
func (s *service) DeleteOrder(ctx context.Context, opts *DeleteOrderServiceRequestOptions) (*DeleteOrderResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// CreateUser handles POST /user
func (s *service) CreateUser(ctx context.Context, opts *CreateUserServiceRequestOptions) (*CreateUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// CreateUsersWithListInput handles POST /user/createWithList
func (s *service) CreateUsersWithListInput(ctx context.Context, opts *CreateUsersWithListInputServiceRequestOptions) (*CreateUsersWithListInputResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// LoginUser handles GET /user/login
func (s *service) LoginUser(ctx context.Context, opts *LoginUserServiceRequestOptions) (*LoginUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// LogoutUser handles GET /user/logout
func (s *service) LogoutUser(ctx context.Context) (*LogoutUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// GetUserByName handles GET /user/{username}
func (s *service) GetUserByName(ctx context.Context, opts *GetUserByNameServiceRequestOptions) (*GetUserByNameResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// UpdateUser handles PUT /user/{username}
func (s *service) UpdateUser(ctx context.Context, opts *UpdateUserServiceRequestOptions) (*UpdateUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}

// DeleteUser handles DELETE /user/{username}
func (s *service) DeleteUser(ctx context.Context, opts *DeleteUserServiceRequestOptions) (*DeleteUserResponseData, error) {
	// TODO: Implement your business logic here.
	// Return nil, nil to use the generated mock response.
	return nil, nil
}
