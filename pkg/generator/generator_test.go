package generator

import (
	"encoding/json"
	"testing"

	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	assert2 "github.com/stretchr/testify/assert"
)

func TestGenerator_Generate(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()
	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)

	t.Run("nil response schema returns nil", func(t *testing.T) {
		res := gen.Response(nil, nil)
		assert.Nil(res.Body)
	})

	t.Run("empty response schema returns nil", func(t *testing.T) {
		res := gen.Response(&schema.ResponseSchema{}, nil)
		assert.Nil(res.Body)
	})

	t.Run("empty body schema returns nil", func(t *testing.T) {
		res := gen.Response(&schema.ResponseSchema{Body: &schema.Schema{}}, nil)
		assert.Nil(res.Body)
	})

	t.Run("empty object schema returns empty JSON object", func(t *testing.T) {
		res := gen.Response(&schema.ResponseSchema{
			ContentType: "application/json",
			Body: &schema.Schema{
				Type:       "object",
				Properties: map[string]*schema.Schema{},
				Nullable:   true,
			},
		}, nil)
		assert.Equal("{}", string(res.Body))
	})

	t.Run("string example is returned as is", func(t *testing.T) {
		res := gen.Response(&schema.ResponseSchema{
			ContentType: "text/plain",
			Body:        &schema.Schema{Example: "hallo, welt!"},
		}, nil)
		assert.Equal("hallo, welt!", string(res.Body))
	})

	t.Run("response with headers", func(t *testing.T) {
		respSchema := &schema.ResponseSchema{
			ContentType: "application/json",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"message": {Type: "string", Enum: []any{"success"}},
				},
			},
			Headers: map[string]*schema.Schema{
				"X-Request-ID": {Type: "string", Enum: []any{"req-123"}},
				"X-Rate-Limit": {Type: "integer", Enum: []any{100}},
			},
		}

		res := gen.Response(respSchema, nil)
		assert.NotNil(res.Body)
		assert.NotNil(res.Headers)
		assert.Equal("req-123", res.Headers.Get("x-request-id"))
		assert.Equal("100", res.Headers.Get("x-rate-limit"))
		assert.False(res.IsError)
	})

	t.Run("response with encoding error", func(t *testing.T) {
		respSchema := &schema.ResponseSchema{
			ContentType: "application/xml",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"data": {
						Type: "object",
						Properties: map[string]*schema.Schema{
							"invalid": {Type: "function"}, // Invalid type that can't be encoded
						},
					},
				},
			},
		}

		res := gen.Response(respSchema, nil)
		assert.NotNil(res.Body)
		// When encoding fails, the error message is returned as body
		assert.True(res.IsError)
	})

	t.Run("nested array generates correct structure", func(t *testing.T) {
		// Test that triple nested arrays ([][][]T) generate the correct JSON structure
		respSchema := &schema.ResponseSchema{
			ContentType: "application/json",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"data": {
						Type: "array",
						Items: &schema.Schema{
							Type: "array",
							Items: &schema.Schema{
								Type: "array",
								Items: &schema.Schema{
									Type: "object",
									Properties: map[string]*schema.Schema{
										"name": {Type: "string", Enum: []any{"test"}},
									},
								},
							},
						},
					},
				},
			},
		}

		res := gen.Response(respSchema, nil)
		assert.NotNil(res.Body)
		t.Logf("Generated response: %s", string(res.Body))

		// Parse the response
		var result map[string]any
		err := json.Unmarshal(res.Body, &result)
		assert.NoError(err)

		// Check that data is an array
		data, ok := result["data"].([]any)
		assert.True(ok, "data should be an array")
		assert.NotEmpty(data, "data should not be empty")

		// Check level 1 -> level 2
		level1, ok := data[0].([]any)
		assert.True(ok, "data[0] should be an array, got %T", data[0])
		assert.NotEmpty(level1, "data[0] should not be empty")

		// Check level 2 -> level 3
		level2, ok := level1[0].([]any)
		assert.True(ok, "data[0][0] should be an array, got %T", level1[0])
		assert.NotEmpty(level2, "data[0][0] should not be empty")

		// Check level 3 contains objects
		obj, ok := level2[0].(map[string]any)
		assert.True(ok, "data[0][0][0] should be an object, got %T", level2[0])
		assert.Contains(obj, "name", "object should have name property")
	})

	t.Run("additionalProperties with object type generates map", func(t *testing.T) {
		// Test that additionalProperties with object type generates a map of objects
		respSchema := &schema.ResponseSchema{
			ContentType: "application/json",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"buildscripts": {
						Type: "object",
						AdditionalProperties: &schema.Schema{
							Type: "object",
							Properties: map[string]*schema.Schema{
								"name": {Type: "string", Enum: []any{"script1"}},
								"path": {Type: "string", Enum: []any{"/path/to/script"}},
							},
						},
					},
				},
			},
		}

		res := gen.Response(respSchema, nil)
		assert.NotNil(res.Body)
		t.Logf("Generated response: %s", string(res.Body))

		// Parse the response
		var result map[string]any
		err := json.Unmarshal(res.Body, &result)
		assert.NoError(err)

		// Check that buildscripts is a map
		buildscripts, ok := result["buildscripts"].(map[string]any)
		assert.True(ok, "buildscripts should be a map, got %T", result["buildscripts"])
		assert.NotEmpty(buildscripts, "buildscripts should not be empty")

		// Check that each value in the map is an object with name and path
		for key, value := range buildscripts {
			obj, ok := value.(map[string]any)
			assert.True(ok, "buildscripts[%s] should be an object, got %T", key, value)
			if ok {
				assert.Contains(obj, "name", "object should have name property")
				assert.Contains(obj, "path", "object should have path property")
			}
		}
	})
}

func TestGenerator_GenerateError(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)

	t.Run("nil schema returns error string", func(t *testing.T) {
		result := gen.Error(nil, "error.message", "Something went wrong")
		assert.Equal([]byte("Something went wrong"), result)
	})

	t.Run("empty path returns error string", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"message": {Type: "string"},
			},
		}
		result := gen.Error(errSchema, "", "Something went wrong")
		assert.Equal([]byte("Something went wrong"), result)
	})

	t.Run("nil schema and empty path returns error string", func(t *testing.T) {
		result := gen.Error(nil, "", "Default error")
		assert.Equal([]byte("Default error"), result)
	})

	t.Run("simple path injection", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"message": {Type: "string"},
			},
		}
		result := gen.Error(errSchema, "message", "User not found")

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("User not found", decoded["message"])
	})

	t.Run("nested path injection", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"error": {
					Type: "object",
					Properties: map[string]*schema.Schema{
						"message": {Type: "string"},
						"code":    {Type: "integer"},
					},
				},
			},
		}
		result := gen.Error(errSchema, "error.message", "Invalid request")

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)

		errorObj, ok := decoded["error"].(map[string]any)
		assert.True(ok)
		assert.Equal("Invalid request", errorObj["message"])
	})

	t.Run("deep nested path injection", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"data": {
					Type: "object",
					Properties: map[string]*schema.Schema{
						"errors": {
							Type: "object",
							Properties: map[string]*schema.Schema{
								"validation": {
									Type: "object",
									Properties: map[string]*schema.Schema{
										"field": {Type: "string"},
									},
								},
							},
						},
					},
				},
			},
		}
		result := gen.Error(errSchema, "data.errors.validation.field", "Invalid email")

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)

		data, ok := decoded["data"].(map[string]any)
		assert.True(ok)
		errors, ok := data["errors"].(map[string]any)
		assert.True(ok)
		validation, ok := errors["validation"].(map[string]any)
		assert.True(ok)
		assert.Equal("Invalid email", validation["field"])
	})

	t.Run("path creates missing nested structure", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type:       "object",
			Properties: map[string]*schema.Schema{},
		}
		result := gen.Error(errSchema, "error.details.message", "Not found")

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)

		e, ok := decoded["error"].(map[string]any)
		assert.True(ok)
		details, ok := e["details"].(map[string]any)
		assert.True(ok)
		assert.Equal("Not found", details["message"])
	})

	t.Run("non-object schema returns error string", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type: "string",
		}
		result := gen.Error(errSchema, "message", "Invalid type")
		assert.Equal([]byte("Invalid type"), result)
	})

	t.Run("nil content from schema creates empty object", func(t *testing.T) {
		errSchema := &schema.Schema{
			Type:       "object",
			Properties: map[string]*schema.Schema{},
		}
		result := gen.Error(errSchema, "message", "Empty schema error")

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("Empty schema error", decoded["message"])
	})

	t.Run("nil content from recursive schema creates empty object", func(t *testing.T) {
		// Test line 139-140: content == nil branch
		// Use a recursive schema that returns nil from generateContentFromSchema
		recursiveSchema := &schema.Schema{
			Type:      "object",
			Recursive: true, // This will cause generateContentFromSchema to return nil
		}

		result := gen.Error(recursiveSchema, "error.message", "Recursive error")
		// When content is nil, it creates an empty object and injects the error
		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		// The error should be injected at the path
		errorObj, ok := decoded["error"].(map[string]any)
		assert.True(ok)
		assert.Equal("Recursive error", errorObj["message"])
	})

	t.Run("encode error returns error string", func(t *testing.T) {
		// Test line 156-157: encodeContent error returns error string
		// Create a generator with a valueReplacer that returns a channel (unmarshalable)
		unmarshalableReplacer := func(s any, state *replacer.ReplaceState) any {
			return make(chan int) // channels can't be marshaled to JSON
		}

		customGen := &ResponseGenerator{
			valueReplacer: unmarshalableReplacer,
		}

		errSchema := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"error": {Type: "string"},
			},
		}

		result := customGen.Error(errSchema, "error.message", "Encode failed")
		// When encodeContent fails, it returns the error string directly
		assert.Equal([]byte("Encode failed"), result)
	})
}

func TestNewGenerator(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)
	assert.NotNil(gen.valueReplacer)
}

func TestGenerator_Request(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)

	t.Run("static resource with nil operation", func(t *testing.T) {
		req := &api.GenerateRequest{
			Path:   "/users/{id}/posts/{postId}",
			Method: "GET",
		}

		result := gen.Request(req, nil, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "path")

		// Path should be returned (placeholders may or may not be replaced depending on context)
		path := decoded["path"].(string)
		assert.NotEmpty(path)
		assert.Contains(path, "/users/")
	})

	t.Run("operation with path only", func(t *testing.T) {
		req := &api.GenerateRequest{
			Path:   "/users/{id}",
			Method: "GET",
		}
		op := &schema.Operation{
			Path:   "/users/{id}",
			Method: "GET",
			PathParams: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"id": {Type: "string", Enum: []any{"123"}},
				},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Equal("/users/123", decoded["path"])
	})

	t.Run("operation with headers", func(t *testing.T) {
		req := &api.GenerateRequest{
			Path:   "/users",
			Method: "GET",
		}
		op := &schema.Operation{
			Path:   "/users",
			Method: "GET",
			Headers: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"Authorization": {Type: "string", Enum: []any{"Bearer token123"}},
					"Content-Type":  {Type: "string", Enum: []any{"application/json"}},
				},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "headers")

		headers := decoded["headers"].(map[string]any)
		assert.Equal("Bearer token123", headers["Authorization"])
		assert.Equal("application/json", headers["Content-Type"])
	})

	t.Run("operation with body", func(t *testing.T) {
		req := &api.GenerateRequest{
			Path:   "/users",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:        "/users",
			Method:      "POST",
			ContentType: "application/json",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"name":  {Type: "string", Enum: []any{"John"}},
					"email": {Type: "string", Enum: []any{"john@example.com"}},
				},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "body")
		assert.Equal("application/json", decoded["contentType"])

		body := decoded["body"].(map[string]any)
		assert.Equal("John", body["name"])
		assert.Equal("john@example.com", body["email"])
	})

	t.Run("operation with custom context", func(t *testing.T) {
		req := &api.GenerateRequest{
			Path:   "/users",
			Method: "GET",
			Context: map[string]any{
				"custom_value": "test123",
			},
		}
		op := &schema.Operation{
			Path:   "/users",
			Method: "GET",
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "path")
	})

	t.Run("wildcard context matches any field", func(t *testing.T) {
		// Create generator with wildcard context
		abc := []string{"apple", "banana", "cherry"}
		contexts := []map[string]any{
			{
				"*": abc,
			},
		}
		genWithWildcard, err := NewGenerator(contexts, nil)
		assert.NoError(err)

		req := &api.GenerateRequest{
			Path:   "/items",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:   "/items",
			Method: "POST",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"random_field_1": {Type: "string"},
					"random_field_2": {Type: "string"},
					"another_field":  {Type: "string"},
				},
			},
		}

		result := genWithWildcard.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "body")

		body := decoded["body"].(map[string]any)

		// All fields should get values from the wildcard context
		assert.Contains(abc, body["random_field_1"])
		assert.Contains(abc, body["random_field_2"])
		assert.Contains(abc, body["another_field"])
	})

	t.Run("operation with form-urlencoded body", func(t *testing.T) {
		req := &api.GenerateRequest{
			Path:   "/login",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:        "/login",
			Method:      "POST",
			ContentType: "application/x-www-form-urlencoded",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"username": {Type: "string", Enum: []any{"testuser"}},
					"password": {Type: "string", Enum: []any{"secret123"}},
				},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "body")
		assert.Equal("application/x-www-form-urlencoded", decoded["contentType"])

		// Body should be form-encoded string
		bodyStr, ok := decoded["body"].(string)
		assert.True(ok, "body should be a string for form-urlencoded")
		assert.Contains(bodyStr, "username=testuser")
		assert.Contains(bodyStr, "password=secret123")
	})

	t.Run("operation with form-urlencoded body encoding error fallback", func(t *testing.T) {
		// Test the fallback when EncodeFormData fails (line 82-84)
		// EncodeFormData fails when body is not a map
		req := &api.GenerateRequest{
			Path:   "/login",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:        "/login",
			Method:      "POST",
			ContentType: "application/x-www-form-urlencoded",
			Body: &schema.Schema{
				Type: "string",
				Enum: []any{"raw-string-body"},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err := json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "body")
		// Body should be the original string (fallback), not form-encoded
		assert.Equal("raw-string-body", decoded["body"])
	})

	t.Run("request rebuilds replacer from service contexts", func(t *testing.T) {
		// Request() rebuilds valueReplacer from serviceContexts,
		// so a custom replacer set directly on the struct gets overridden.
		customGen := &ResponseGenerator{}

		req := &api.GenerateRequest{
			Path:   "/test",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:   "/test",
			Method: "POST",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"data": {Type: "string", Enum: []any{"hello"}},
				},
			},
		}

		result := customGen.Request(req, op, nil)
		assert.NotNil(result)
	})

	t.Run("user context overrides service context in request", func(t *testing.T) {
		serviceCtx := []map[string]any{
			{"name": "service-name"},
		}
		gen, err := NewGenerator(serviceCtx, nil)
		assert.NoError(err)

		req := &api.GenerateRequest{
			Path:   "/users",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:   "/users",
			Method: "POST",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"name": {Type: "string"},
				},
			},
		}

		userCtx := map[string]any{"name": "user-override"}
		result := gen.Request(req, op, userCtx)
		assert.NotNil(result)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		body := decoded["body"].(map[string]any)
		assert.Equal("user-override", body["name"])
	})

	t.Run("user context overrides service context in response", func(t *testing.T) {
		serviceCtx := []map[string]any{
			{"status": "from-service"},
		}
		gen, err := NewGenerator(serviceCtx, nil)
		assert.NoError(err)

		respSchema := &schema.ResponseSchema{
			ContentType: "application/json",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"status": {Type: "string"},
				},
			},
		}

		userCtx := map[string]any{"status": "from-user"}
		res := gen.Response(respSchema, userCtx)
		assert.NotNil(res.Body)

		var decoded map[string]any
		err = json.Unmarshal(res.Body, &decoded)
		assert.NoError(err)
		assert.Equal("from-user", decoded["status"])
	})

	t.Run("in-header context replaces request headers", func(t *testing.T) {
		serviceCtx := []map[string]any{
			{
				"in-header": map[string]any{
					"X-Merchant": "WLT0001",
					"X-Type":     "JSON",
				},
			},
		}
		gen, err := NewGenerator(serviceCtx, nil)
		assert.NoError(err)

		req := &api.GenerateRequest{
			Path:   "/payment/execute",
			Method: "POST",
		}
		op := &schema.Operation{
			Path:   "/payment/execute",
			Method: "POST",
			Headers: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"X-Merchant": {Type: "string"},
					"X-Type":     {Type: "string"},
				},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "headers")

		headers := decoded["headers"].(map[string]any)
		assert.Equal("WLT0001", headers["X-Merchant"])
		assert.Equal("JSON", headers["X-Type"])
	})

	t.Run("in-request-header context replaces request headers", func(t *testing.T) {
		serviceCtx := []map[string]any{
			{
				"in-request-header": map[string]any{
					"X-Api-Key": "secret-key",
				},
			},
		}
		gen, err := NewGenerator(serviceCtx, nil)
		assert.NoError(err)

		req := &api.GenerateRequest{
			Path:   "/api/data",
			Method: "GET",
		}
		op := &schema.Operation{
			Path:   "/api/data",
			Method: "GET",
			Headers: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"X-Api-Key": {Type: "string"},
				},
			},
		}

		result := gen.Request(req, op, nil)
		assert.NotNil(result)

		var decoded map[string]any
		err = json.Unmarshal(result, &decoded)
		assert.NoError(err)
		assert.Contains(decoded, "headers")

		headers := decoded["headers"].(map[string]any)
		assert.Equal("secret-key", headers["X-Api-Key"])
	})

	t.Run("user context with func prefix is resolved", func(t *testing.T) {
		gen, err := NewGenerator(nil, nil)
		assert.NoError(err)

		respSchema := &schema.ResponseSchema{
			ContentType: "application/json",
			Body: &schema.Schema{
				Type: "object",
				Properties: map[string]*schema.Schema{
					"count": {Type: "integer"},
				},
			},
		}

		userCtx := map[string]any{"count": "func:int_between:1,100"}
		res := gen.Response(respSchema, userCtx)
		assert.NotNil(res.Body)

		var decoded map[string]any
		err = json.Unmarshal(res.Body, &decoded)
		assert.NoError(err)
		count, ok := decoded["count"].(float64)
		assert.True(ok)
		assert.GreaterOrEqual(count, float64(1))
		assert.LessOrEqual(count, float64(100))
	})
}
