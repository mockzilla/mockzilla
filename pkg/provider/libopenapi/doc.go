// Package libopenapi implements typedef.OperationRegistry by walking
// libopenapi's v3 document directly, without going through the
// oapi-codegen-dd intermediate GoSchema/TypeDefinition layer.
//
// The libopenapi imports for spec parsing are contained within this
// package. Callers receive plain *schema.Operation / *schema.Schema
// values and never see libopenapi types.
//
// Operation lookup is O(1) (a path:METHOD -> entry map built at startup)
// and schema conversion happens lazily on first request per operation;
// there is no per-request spec reparse.
package libopenapi
