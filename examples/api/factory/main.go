package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http/httptest"

	"github.com/mockzilla/mockzilla/v2/pkg/factory"
)

//go:embed openapi.yml
var spec []byte

func main() {
	// --8<-- [start:init]
	f, err := factory.NewFactory(spec)
	if err != nil {
		log.Fatalf("creating factory: %v", err)
	}
	// --8<-- [end:init]

	// --8<-- [start:operations]
	// List all available operations
	for _, op := range f.Operations() {
		fmt.Printf("%s %s\n", op.Method, op.Path)
	}
	// --8<-- [end:operations]

	// --8<-- [start:response]
	// Generate a full response (body + headers)
	resp, err := f.Response("/pets/{petId}", "GET", nil)
	if err != nil {
		log.Fatalf("generating response: %v", err)
	}
	fmt.Println("Response:", string(resp.Body))
	// --8<-- [end:response]

	// --8<-- [start:response-body]
	// Generate just the response body bytes
	body, err := f.ResponseBody("/pets/{petId}", "GET", nil)
	if err != nil {
		log.Fatalf("generating response body: %v", err)
	}
	fmt.Println("Response body:", string(body))
	// --8<-- [end:response-body]

	// --8<-- [start:request]
	// Generate a full request (path with values, contentType, headers, body)
	req, err := f.Request("/pets", "POST", nil)
	if err != nil {
		log.Fatalf("generating request: %v", err)
	}
	fmt.Println("Request path:", req.Path)
	fmt.Println("Request body:", string(req.Body))
	// --8<-- [end:request]

	// --8<-- [start:request-body]
	// Generate just the request body bytes
	reqBody, err := f.RequestBody("/pets", "POST", nil)
	if err != nil {
		log.Fatalf("generating request body: %v", err)
	}
	fmt.Println("Request body:", string(reqBody))
	// --8<-- [end:request-body]

	// --8<-- [start:from-request]
	// Generate response from an http.Request (path auto-matched)
	r := httptest.NewRequest("GET", "/pets/42", nil)
	respFromReq, err := f.ResponseBodyFromRequest(r, nil)
	if err != nil {
		log.Fatalf("generating response from request: %v", err)
	}
	fmt.Println("Response from request:", string(respFromReq))
	// --8<-- [end:from-request]

	// --8<-- [start:context]
	// Pass a custom replacement context to control generated values
	customResp, err := f.ResponseBody("/pets/{petId}", "GET", map[string]any{
		"name":   "Buddy",
		"status": "available",
	})
	if err != nil {
		log.Fatalf("generating response with context: %v", err)
	}
	fmt.Println("Custom response:", string(customResp))
	// --8<-- [end:context]
}
