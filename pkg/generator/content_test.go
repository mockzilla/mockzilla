package generator

import (
	"embed"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	assert2 "github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

//go:embed testdata/**
var testDataFS embed.FS

func loadSpec(t *testing.T, fileName string, maxRecursionDepth int) *typedef.TypeDefinitionRegistry {
	t.Helper()

	cfg := codegen.NewDefaultConfiguration()

	specContents, err := testDataFS.ReadFile(filepath.Join("testdata", fileName))
	if err != nil {
		t.Errorf("Error reading file: %v", err)
		t.FailNow()
	}

	parseCtx, errs := codegen.CreateParseContext(specContents, cfg)
	if len(errs) > 0 {
		// errExit("Error parsing OpenAPI spec: %v", errs[0])
		t.Errorf("Error parsing OpenAPI spec: %v", errs[0])
		t.FailNow()
	}

	return typedef.NewTypeDefinitionRegistry(parseCtx, maxRecursionDepth, specContents)
}

func createSchemaFromString(t *testing.T, value string) *schema.Schema {
	t.Helper()

	target := &schema.Schema{}
	if err := yaml.Unmarshal([]byte(value), &target); err != nil {
		t.Errorf("Error parsing schema: %v", err)
		t.FailNow()
	}

	return target
}

func TestNestedRefsResponse(t *testing.T) {
	assert := assert2.New(t)

	// Load the spec with nested $refs - use maxRecursionDepth=0 like the runtime does
	spec := loadSpec(t, "test-nested-refs.yaml", 0)

	// Get the operation and response schema
	op := spec.FindOperation("/test", http.MethodPost)
	assert.NotNil(op, "Operation should not be nil")
	assert.NotNil(op.Response, "Response should not be nil")

	responseSchema := op.Response.GetSuccess().Content
	assert.NotNil(responseSchema, "Response schema should not be nil")

	// Create a generator to get the value replacer
	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)

	// Generate content for the response (read-only mode)
	state := replacer.NewReplaceState(replacer.WithReadOnly())
	content := generateContentFromSchema(responseSchema, gen.valueReplacer, state)

	assert.NotNil(content, "Generated content should not be nil")

	// Verify it marshals to JSON without error
	_, err = json.Marshal(content)
	assert.NoError(err)

	// Verify the structure
	contentMap, ok := content.(map[string]any)
	assert.True(ok, "Content should be a map")

	features, ok := contentMap["features"]
	assert.True(ok, "features field should exist")
	assert.NotNil(features, "features should not be nil")

	featuresMap, ok := features.(map[string]any)
	assert.True(ok, "features should be a map")

	invoiceHistory, ok := featuresMap["invoice_history"]
	assert.True(ok, "invoice_history field should exist")
	assert.NotNil(invoiceHistory, "invoice_history should not be nil")

	invoiceHistoryMap, ok := invoiceHistory.(map[string]any)
	assert.True(ok, "invoice_history should be a map")

	enabled, ok := invoiceHistoryMap["enabled"]
	assert.True(ok, "enabled field should exist")
	assert.NotNil(enabled, "enabled should not be nil")
}

func TestGenerateContentFromSchema(t *testing.T) {
	assert := assert2.New(t)

	t.Run("static-content", func(t *testing.T) {
		staticJSON := `{"id":"123","name":"Static User"}`
		s := &schema.Schema{
			Type:          "object",
			StaticContent: staticJSON,
		}

		result := generateContentFromSchema(s, nil, nil)
		assert.NotNil(result)

		// Should return json.RawMessage with the static content
		rawMsg, ok := result.(json.RawMessage)
		assert.True(ok)
		assert.Equal(staticJSON, string(rawMsg))
	})

	t.Run("base-case", func(t *testing.T) {
		valueResolver := func(content any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "id":
				return 21
			case "score":
				return 11.5
			case "limit":
				return 100
			case "tag1":
				return "#dice"
			case "tag2":
				return "#nice"
			case "offset":
				return -1
			case "query":
				return "games"
			case "first":
				return 10
			case "second":
				return 20
			case "last":
				return 30
			}
			return nil
		}

		spec := loadSpec(t, "users.yml", 0)
		s := spec.FindOperation("/users/{id}", http.MethodGet).Response.GetSuccess().Content

		res := generateContentFromSchema(s, valueResolver, nil)

		expected := map[string]any{
			"user": map[string]any{"id": 21, "score": 11.5},
			"pages": []any{
				map[string]any{
					"limit":  100,
					"tag1":   "#dice",
					"tag2":   "#nice",
					"offset": -1,
					"first":  10,
					"second": 20,
				},
			},
		}
		assert.Equal(expected, res)
	})

	t.Run("with-empty-not-nullable-array", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			return replacer.NULL
		}
		s := createSchemaFromString(t, `
type: array
items:
  type: string
`)
		res := generateContentFromSchema(s, valueResolver, nil)

		expected := make([]any, 0)
		assert.Equal(expected, res)
	})

	t.Run("with-empty-nullable-object-top-level", func(t *testing.T) {
		// Top-level nullable objects with no properties should return {}
		// This is important for response bodies - we should never return null
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			return replacer.NULL
		}
		s := createSchemaFromString(t, `
type: object
nullable: true
properties: {}
`)
		res := generateContentFromSchema(s, valueResolver, nil)

		expected := map[string]any{}
		assert.Equal(expected, res)
	})

	t.Run("with-empty-but-nullable-array", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			return replacer.NULL
		}

		s := createSchemaFromString(t, `
type: array
nullable: true
items:
  type: string
`)
		res := generateContentFromSchema(s, valueResolver, nil)
		assert.Nil(res)
	})

	t.Run("fast-track-resolve-null-string", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			return replacer.NULL
		}
		s := createSchemaFromString(t, `
type: string
`)
		res := generateContentFromSchema(s, valueResolver, replacer.NewReplaceStateWithName("name"))
		assert.Nil(res)
	})

	t.Run("uuid-format-takes-precedence-over-enum", func(t *testing.T) {
		// Test that format: uuid generates UUIDs even when context has matching field name
		spec := loadSpec(t, "notification-with-uuid.yml", 10)

		op := spec.FindOperation("/api/v1/notifications", http.MethodGet)

		// Create generator with context that has "status" array - this is the bug!
		contexts := []map[string]any{
			{
				"status": []string{"success", "pending", "failed"},
			},
		}
		gen, err := NewGenerator(contexts, nil)
		assert.NoError(err)

		result := generateContentFromSchema(op.Response.GetSuccess().Content, gen.valueReplacer, nil)

		jsonBytes, _ := json.Marshal(result)
		var parsed []map[string]any
		_ = json.Unmarshal(jsonBytes, &parsed)

		notification := parsed[0]

		// Both account.id and status.id should be UUIDs (36 chars), not enum values
		account := notification["account"].(map[string]any)
		assert.Len(account["id"].(string), 36, "account.id should be UUID")

		status := notification["status"].(map[string]any)
		statusID := status["id"].(string)
		assert.Len(statusID, 36, "status.id should be UUID, not enum value like 'failed'")

		// acct field without format should use enum
		assert.Contains([]string{"pending", "failed", "success"}, status["acct"].(string))
	})

	t.Run("with-nested-all-of", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "name":
				return "Jane Doe"
			case "age":
				return 30
			case "tag":
				return "#doe"
			case "league":
				return "premier"
			case "rating":
				return 345.6
			}
			return nil
		}

		spec := loadSpec(t, "nested-all-of.yml", 0)
		s := spec.FindOperation("/foo", http.MethodGet).Response.GetSuccess().Content

		expected := map[string]any{
			"name":   "Jane Doe",
			"age":    30,
			"tag":    "#doe",
			"league": "premier",
			"rating": 345.6,
		}

		res := generateContentFromSchema(s, valueResolver, nil)
		assert.Equal(expected, res)
	})

	t.Run("fast-track-not-used-with-object", func(t *testing.T) {
		dice := map[string]string{"nice": "very nice", "rice": "good rice"}

		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "nice":
				return "not so nice"
			case "rice":
				return "not a rice"
			case "dice":
				return dice
			}
			return nil
		}

		s := createSchemaFromString(t, `
type: object
properties:
  dice:
    type: object
    properties:
      nice:
        type: string
      rice:
        type: string
`)
		res := generateContentFromSchema(s, valueResolver, nil)

		expected := map[string]any{"dice": map[string]any{
			"nice": "not so nice",
			"rice": "not a rice",
		}}
		assert.Equal(expected, res)
	})

	t.Run("with-circular-array-references", func(t *testing.T) {
		// With maxRecursionDepth=0, the first nested Node (children.Items) is blocked
		// because it's the first recursion. The children array should be empty.
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "id":
				return 123
			case "name":
				return "noda-123"
			}
			return nil
		}

		spec := loadSpec(t, "circular-array.yml", 0)
		s := spec.FindOperation("/nodes/{id}", http.MethodGet).Response.GetSuccess().Content
		res := generateContentFromSchema(s, valueResolver, nil)

		// With maxRecursionDepth=0, children array items are blocked (Recursive=true)
		// so the children property is omitted (optional property with nil value)
		expected := map[string]any{
			"id":   123,
			"name": "noda-123",
		}
		assert.Equal(expected, res)
	})

	t.Run("with-circular-object-references", func(t *testing.T) {
		// With maxRecursionDepth=0, the first nested Node (parent) is blocked
		// because it's the first recursion. The parent property should be omitted.
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "id":
				return 123
			case "name":
				return "noda-123"
			}
			return nil
		}

		spec := loadSpec(t, "circular-with-references.yml", 0)
		s := spec.FindOperation("/nodes/{id}", http.MethodGet).Response.GetSuccess().Content
		res := generateContentFromSchema(s, valueResolver, nil)

		// With maxRecursionDepth=0, parent is blocked (Recursive=true)
		// so the parent property is omitted (optional property with nil value)
		expected := map[string]any{
			"id":   123,
			"name": "noda-123",
		}
		assert.Equal(expected, res)
	})

	t.Run("with-circular-object-references-inlined", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			switch state.NamePath[len(state.NamePath)-1] {
			case "id":
				return 123
			case "name":
				return "noda-123"
			}
			return nil
		}
		spec := loadSpec(t, "circular-with-inline.yml", 0)
		s := spec.FindOperation("/nodes/{id}", http.MethodGet).Response.GetSuccess().Content

		res := generateContentFromSchema(s, valueResolver, nil)

		expected := map[string]any{
			"id":   123,
			"name": "noda-123",
			"parent": map[string]any{
				"id":     123,
				"name":   "noda-123",
				"parent": map[string]any{},
			},
		}
		assert.Equal(expected, res)
	})

	t.Run("with-circular-level-1", func(t *testing.T) {
		valueReplacer := replacer.CreateValueReplacer(replacer.Replacers, nil)
		spec := loadSpec(t, "circular-ucr.yml", 1)

		s := spec.FindOperation("/api/org-api/v1/organization/{acctStructureCode}", http.MethodGet).Response.GetSuccess().Content
		res := generateContentFromSchema(s, valueReplacer, nil)

		orgs := []string{"Division", "Department", "Organization"}
		v := res.(map[string]any)

		assert.NotNil(res)
		assert.Contains([]bool{true, false}, v["success"])

		r := v["response"].(map[string]any)

		// With maxRecursionDepth=1, we allow:
		// - Depth 0: response (OrgModel) - full properties
		// - Depth 1: response.parent (OrgModel) - full properties
		// - Depth 2: response.parent.parent (OrgModel) - blocked (empty)
		parent := r["parent"].(map[string]any)
		assert.NotEmpty(parent, "Parent should have properties at recursion level 1")
		assert.Contains(orgs, parent["type"])

		// Parent's parent should be nil (blocked at depth 2)
		assert.Nil(parent["parent"])

		typ := r["type"]
		assert.Contains(orgs, typ)

		children := r["children"].([]any)
		assert.Equal(1, len(children))
		kid := children[0].(map[string]any)
		assert.Contains(orgs, kid["type"])
		// Kid's parent should be nil (blocked at depth 2)
		assert.Nil(kid["parent"])
	})
}

func TestGenerateContentFromSchema_IndirectRecursionWithRequiredField(t *testing.T) {
	assert := assert2.New(t)

	// Test indirect recursion: Chat -> pinned_message (optional) -> Message -> chat (required) -> Chat (recursive)
	// With maxRecursionDepth=0, the nested Chat should be skipped, and since chat is required in Message,
	// Message should return nil, and since pinned_message is optional, it should be skipped entirely.

	valueResolver := func(schema any, state *replacer.ReplaceState) any {
		name := state.NamePath[len(state.NamePath)-1]
		switch name {
		case "id", "message_id", "date":
			return 123
		case "type":
			return "private"
		}
		return nil
	}

	spec := loadSpec(t, "telegram-chat.yml", 0)
	op := spec.FindOperation("/getChat", "POST")
	assert.NotNil(op, "Operation should not be nil")

	s := op.Response.GetSuccess().Content
	res := generateContentFromSchema(s, valueResolver, nil)

	// pinned_message should NOT be in the result because:
	// 1. chat is required in Message
	// 2. chat hits recursion and returns nil
	// 3. Message returns nil because required field is nil
	// 4. pinned_message is optional, so it's skipped
	resMap := res.(map[string]any)
	resultMap := resMap["result"].(map[string]any)
	assert.Nil(resultMap["pinned_message"], "pinned_message should be nil because it contains required recursive field")
}

func TestGenerateContentFromSchema_ReadWrite(t *testing.T) {
	assert := assert2.New(t)

	t.Run("read-only-complete-object-when-write-only-requested", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			name := state.NamePath[len(state.NamePath)-1]
			return name + "-value"
		}
		s := createSchemaFromString(t, `
        {
            "type":"object",
            "properties": {
                "product": {
                    "type": "object",
                    "properties": {
                        "nice": {
                            "type": "string"
                        },
                        "rice": {
                            "type": "string"
                        },
						"price": {
							"type": "string"
						}
                    }
                }
            },
			"readOnly": true
        }`)
		state := replacer.NewReplaceState(replacer.WithWriteOnly())
		res := generateContentFromSchema(s, valueResolver, state)

		assert.Nil(res)
	})

	t.Run("read-only-inner-object", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			name := state.NamePath[len(state.NamePath)-1]
			return name + "-value"
		}

		s := createSchemaFromString(t, `
type: object
properties:
  product:
    type: object
    readOnly: true
    properties:
      nice:
        type: string
      rice:
        type: string
      price:
        type: string
`)
		state := replacer.NewReplaceState(replacer.WithWriteOnly())
		res := generateContentFromSchema(s, valueResolver, state)

		expected := map[string]any{}
		assert.Equal(expected, res)
	})

	t.Run("read-only-properties", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			name := state.NamePath[len(state.NamePath)-1]
			return name + "-value"
		}

		s := createSchemaFromString(t, `
type: object
properties:
  product:
    type: object
    properties:
      nice:
        type: string
        readOnly: true
      rice:
        type: string
        writeOnly: true
      price:
        type: string
`)
		state := replacer.NewReplaceState(replacer.WithReadOnly())

		res := generateContentFromSchema(s, valueResolver, state)

		// only ro included
		expected := map[string]any{
			"product": map[string]any{
				"nice":  "nice-value",
				"price": "price-value",
			},
		}
		assert.Equal(expected, res)
	})

	t.Run("nested-object-with-all-readonly-required-fields-omitted-in-request", func(t *testing.T) {
		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			name := state.NamePath[len(state.NamePath)-1]
			return name + "-value"
		}

		// Simulates a schema like PayPal's authorization with fmf_details
		// where fmf_details has only readOnly required fields
		s := createSchemaFromString(t, `
type: object
properties:
  amount:
    type: string
  fmf_details:
    type: object
    properties:
      filter_type:
        type: string
        readOnly: true
      filter_id:
        type: string
        readOnly: true
    required:
      - filter_type
      - filter_id
required:
  - amount
`)
		state := replacer.NewReplaceState(replacer.WithWriteOnly())
		res := generateContentFromSchema(s, valueResolver, state)

		// fmf_details should be omitted entirely because all its required fields are readOnly
		expected := map[string]any{
			"amount": "amount-value",
		}
		assert.Equal(expected, res)
	})

	t.Run("required-fields-with-real-value-replacer", func(t *testing.T) {
		// Test with the actual default value replacer to see what it generates
		gen, err := NewGenerator(nil, nil)
		assert.NoError(err)

		s := createSchemaFromString(t, `
type: object
properties:
  features:
    type: object
    properties:
      invoice_history:
        type: object
        properties:
          enabled:
            type: boolean
        required:
          - enabled
    required:
      - invoice_history
required:
  - features
`)
		state := replacer.NewReplaceState(replacer.WithReadOnly())
		res := generateContentFromSchema(s, gen.valueReplacer, state)

		// Should generate actual values
		assert.NotNil(res)
		resMap := res.(map[string]any)
		assert.Contains(resMap, "features")

		features := resMap["features"].(map[string]any)
		assert.Contains(features, "invoice_history")

		invoiceHistory := features["invoice_history"].(map[string]any)
		assert.Contains(invoiceHistory, "enabled")
		// enabled should be a boolean, not nil
		_, ok := invoiceHistory["enabled"].(bool)
		assert.True(ok, "enabled should be a boolean")
	})

	t.Run("required-boolean-field-should-not-be-nil", func(t *testing.T) {
		// Verify that boolean fields always get a value, never nil
		gen, err := NewGenerator(nil, nil)
		assert.NoError(err)

		s := createSchemaFromString(t, `
type: object
properties:
  enabled:
    type: boolean
required:
  - enabled
`)
		state := replacer.NewReplaceState(replacer.WithReadOnly())
		res := generateContentFromSchema(s, gen.valueReplacer, state)

		assert.NotNil(res)
		resMap := res.(map[string]any)
		assert.Contains(resMap, "enabled")

		// enabled should be a boolean value (not nil)
		// We use faker to generate random boolean values
		enabled := resMap["enabled"]
		assert.NotNil(enabled, "enabled should not be nil")
		_, ok := enabled.(bool)
		assert.True(ok, "enabled should be a boolean type")
	})
}

func TestGenerateContentFromSchema_UnionTypes(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)

	t.Run("oneOf in request body picks first element", func(t *testing.T) {
		spec := loadSpec(t, "with-unions.yml", 0)
		op := spec.FindOperation("/payment", http.MethodPost)
		assert.NotNil(op)
		assert.NotNil(op.Body)

		// Response content from the request body schema
		res := generateContentFromSchema(op.Body, gen.valueReplacer, nil)
		assert.NotNil(res)

		// Should be a map (object)
		resMap, ok := res.(map[string]any)
		assert.True(ok, "Result should be a map")

		// Should have properties from ONLY CreditCard (first union element)
		// CreditCard has: cardNumber, expiryDate, cvv
		assert.Contains(resMap, "cardNumber", "Should have cardNumber from CreditCard")
		assert.Contains(resMap, "expiryDate", "Should have expiryDate from CreditCard")
		assert.Contains(resMap, "cvv", "Should have cvv from CreditCard")

		// Should NOT have BankAccount properties
		assert.NotContains(resMap, "accountNumber", "Should NOT have accountNumber from BankAccount")
		assert.NotContains(resMap, "routingNumber", "Should NOT have routingNumber from BankAccount")
		assert.NotContains(resMap, "bankName", "Should NOT have bankName from BankAccount")
	})

	t.Run("oneOf in array items picks first element", func(t *testing.T) {
		spec := loadSpec(t, "with-unions.yml", 0)
		op := spec.FindOperation("/pets", http.MethodGet)
		assert.NotNil(op)

		success := op.Response.GetSuccess()
		assert.NotNil(success)
		assert.NotNil(success.Content)

		// Response content from the response schema
		res := generateContentFromSchema(success.Content, gen.valueReplacer, nil)
		assert.NotNil(res)

		// Should be an array
		resArray, ok := res.([]any)
		assert.True(ok, "Result should be an array")
		assert.Greater(len(resArray), 0, "Array should have at least one item")

		// Check first item
		firstItem, ok := resArray[0].(map[string]any)
		assert.True(ok, "First item should be a map")

		// Should have properties from ONLY Dog (first union element)
		// Dog has: name, breed, barkVolume
		assert.Contains(firstItem, "name", "Should have name from Dog")
		assert.Contains(firstItem, "breed", "Should have breed from Dog")
		assert.Contains(firstItem, "barkVolume", "Should have barkVolume from Dog")

		// Should NOT have Cat-specific properties
		assert.NotContains(firstItem, "color", "Should NOT have color from Cat")
		assert.NotContains(firstItem, "meowPitch", "Should NOT have meowPitch from Cat")
	})
}

func TestGenerateContentObject(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	t.Run("GenerateContentObject", func(t *testing.T) {
		spec := loadSpec(t, "schema-with-name-obj-and-age.yml", 0)
		s := spec.FindOperation("/me", http.MethodGet).Response.GetSuccess().Content

		valueResolver := func(schema any, state *replacer.ReplaceState) any {
			namePath := state.NamePath
			for _, name := range namePath {
				switch name {
				case "first":
					return "Jane"
				case "last":
					return "Doe"
				case "age":
					return 21
				}
			}
			return nil
		}
		res := generateContentObject(s, valueResolver, nil)

		expected := `{"age":21,"name":{"first":"Jane","last":"Doe"}}`
		resJs, _ := json.Marshal(res)
		assert.Equal(expected, string(resJs))
	})

	t.Run("with-no-properties", func(t *testing.T) {
		s := createSchemaFromString(t, `{"type": "object"}`)
		res := generateContentObject(s, nil, nil)
		assert.Nil(res)
	})

	t.Run("with-no-resolved-values", func(t *testing.T) {
		s := createSchemaFromString(t, `
        {
            "type":"object",
            "properties": {
                "name": {
                    "type": "object",
                    "properties": {
                        "first": {"type": "string"}
                    }
                }
            }
        }`)
		expected := map[string]any{
			"name": map[string]any{},
		}
		res := generateContentObject(s, nil, nil)
		assert.Equal(expected, res)
	})

	t.Run("with-additional-properties", func(t *testing.T) {
		var extraNames []string
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			name := state.NamePath[0]
			if name != "name" && name != "address" {
				extraNames = append(extraNames, name)
			}
			return name + "-value"
		}
		s := createSchemaFromString(t, `
type: object
properties:
  name:
    type: string
  address:
    type: string
additionalProperties:
  type: string
`)

		res := generateContentObject(s, valueReplacer, nil)

		expected := map[string]any{
			"name":    "name-value",
			"address": "address-value",
		}
		for _, e := range extraNames {
			expected[e] = e + "-value"
		}

		assert.Equal(expected, res)
	})

	t.Run("with-max-properties", func(t *testing.T) {
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			return state.NamePath[0] + "-value"
		}
		s := createSchemaFromString(t, `
type: object
maxProperties: 1
properties:
  name:
    type: string
  address:
    type: string
`)

		res := generateContentObject(s, valueReplacer, nil)
		assert.Equal(1, len(res.(map[string]any)))
	})

	t.Run("with-no-properties-but-additionalProperties", func(t *testing.T) {
		// This tests the bug fix: objects with no properties but additionalProperties: true
		// should generate additional properties, not return nil/empty map
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			return state.NamePath[0] + "-value"
		}
		s := createSchemaFromString(t, `
type: object
additionalProperties:
  type: string
properties: {}
`)

		res := generateContentObject(s, valueReplacer, nil)
		assert.NotNil(res)
		resMap, ok := res.(map[string]any)
		assert.True(ok)
		assert.Equal(3, len(resMap), "Should generate 3 additional properties")
	})

	t.Run("with-no-properties-and-no-additionalProperties", func(t *testing.T) {
		// Objects with no properties and no additionalProperties should return nil
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			return state.NamePath[0] + "-value"
		}
		s := createSchemaFromString(t, `
type: object
properties: {}
`)

		res := generateContentObject(s, valueReplacer, nil)
		assert.Nil(res)
	})

	t.Run("with-max-properties-limits-additional-properties", func(t *testing.T) {
		valueReplacer := func(schema any, state *replacer.ReplaceState) any {
			return state.NamePath[0] + "-value"
		}
		s := createSchemaFromString(t, `
type: object
maxProperties: 2
properties:
  name:
    type: string
additionalProperties:
  type: string
`)

		res := generateContentObject(s, valueReplacer, nil)
		resMap, ok := res.(map[string]any)
		assert.True(ok)
		// Should have at most 2 properties (1 defined + 1 additional)
		assert.LessOrEqual(len(resMap), 2)
		assert.Contains(resMap, "name")
	})

	t.Run("required-array-property-with-recursion-returns-empty-array", func(t *testing.T) {
		// Test line 151-155: required property that hits recursion and is an array type
		// should return empty array instead of nil
		// We need to create a scenario where:
		// 1. The array property is required
		// 2. The array items hit recursion (return nil)
		// 3. The array itself should become empty array

		// Create a schema where the array items reference the parent
		parentSchema := &schema.Schema{
			Type:     "object",
			Required: []string{"items"},
			Properties: map[string]*schema.Schema{
				"name": {Type: "string", Enum: []any{"test"}},
			},
		}
		// The items array contains the parent schema itself
		parentSchema.Properties["items"] = &schema.Schema{
			Type:  "array",
			Items: parentSchema,
		}

		// Use a valueReplacer that returns values for name
		valueReplacer := func(s any, state *replacer.ReplaceState) any {
			if len(state.NamePath) > 0 && state.NamePath[len(state.NamePath)-1] == "name" {
				return "test"
			}
			return nil
		}

		state := replacer.NewReplaceState()
		res := generateContentObject(parentSchema, valueReplacer, state)

		// The result should have name and items
		resMap, ok := res.(map[string]any)
		assert.True(ok)
		assert.Equal("test", resMap["name"])
		// items should be present (required) and contain nested objects
		items, hasItems := resMap["items"]
		assert.True(hasItems, "items should be present")
		// The items array should have at least one element (the nested object)
		// but the nested object's items should be empty due to recursion
		itemsArr, ok := items.([]any)
		assert.True(ok)
		assert.Greater(len(itemsArr), 0)
		// Check that nested items are empty arrays due to recursion
		nestedObj, ok := itemsArr[0].(map[string]any)
		assert.True(ok)
		nestedItems, hasNestedItems := nestedObj["items"]
		assert.True(hasNestedItems)
		assert.Equal([]any{}, nestedItems, "nested items should be empty array due to recursion")
	})

	t.Run("required-non-array-property-with-recursion-returns-nil", func(t *testing.T) {
		// Test line 151-153: required property that hits recursion and is NOT an array type
		// should return nil (propagate recursion)
		selfRefSchema := &schema.Schema{
			Type:     "object",
			Required: []string{"parent"},
			Properties: map[string]*schema.Schema{
				"name": {Type: "string", Enum: []any{"test"}},
			},
		}
		// Create a self-referencing object property (not array)
		selfRefSchema.Properties["parent"] = selfRefSchema

		valueReplacer := func(s any, state *replacer.ReplaceState) any {
			if len(state.NamePath) > 0 && state.NamePath[len(state.NamePath)-1] == "name" {
				return "test"
			}
			return nil
		}

		state := replacer.NewReplaceState()
		res := generateContentObject(selfRefSchema, valueReplacer, state)

		// Should return nil because required non-array property hit recursion
		assert.Nil(res)
		assert.True(state.RecursionHit)
	})
}

func TestGenerateContentFromArray(t *testing.T) {
	assert := assert2.New(t)
	t.Parallel()

	gen, err := NewGenerator(nil, nil)
	assert.NoError(err)
	assert.NotNil(gen)

	t.Run("nil schema returns nil", func(t *testing.T) {
		res := generateContentFromSchema(nil, gen.valueReplacer, nil)
		assert.Nil(res)
	})

	t.Run("any type in array items generates empty objects", func(t *testing.T) {
		// This tests the case where oapi-codegen generates []struct{} for empty item schemas
		// We need to generate data that can be unmarshaled into struct{}, which is {}
		s := createSchemaFromString(t, `
type: array
items:
  type: any
`)
		res := generateContentArray(s, gen.valueReplacer, nil)
		assert.NotNil(res)

		// Should generate an array with at least one item
		resArray, ok := res.([]any)
		assert.True(ok)
		assert.Greater(len(resArray), 0)

		// Each item should be an empty object that can unmarshal into struct{}
		for _, item := range resArray {
			itemMap, ok := item.(map[string]any)
			assert.True(ok, "Item should be a map (empty object)")
			assert.Equal(0, len(itemMap), "Item should be an empty object")
		}
	})

	t.Run("schema with no type returns nil", func(t *testing.T) {
		// When there's no type specified, we shouldn't generate data
		s := &schema.Schema{}
		res := generateContentFromSchema(s, gen.valueReplacer, nil)
		// With no type, it defaults to "string" and tries valueReplacer
		// If valueReplacer returns nil, result should be nil
		assert.Nil(res)
	})

	t.Run("array with MinItems generates correct number of items", func(t *testing.T) {
		minItems := int64(3)
		s := &schema.Schema{
			Type:     "array",
			MinItems: &minItems,
			Items:    &schema.Schema{Type: "string", Enum: []any{"test"}},
		}
		res := generateContentArray(s, gen.valueReplacer, nil)
		assert.NotNil(res)
		resArray, ok := res.([]any)
		assert.True(ok)
		assert.Len(resArray, 3)
	})

	t.Run("array with MinItems=0 generates 1 item", func(t *testing.T) {
		minItems := int64(0)
		s := &schema.Schema{
			Type:     "array",
			MinItems: &minItems,
			Items:    &schema.Schema{Type: "string", Enum: []any{"test"}},
		}
		res := generateContentArray(s, gen.valueReplacer, nil)
		assert.NotNil(res)
		resArray, ok := res.([]any)
		assert.True(ok)
		assert.Len(resArray, 1)
	})

	t.Run("array with nil items returns nil", func(t *testing.T) {
		s := &schema.Schema{
			Type:  "array",
			Items: nil,
		}
		res := generateContentArray(s, gen.valueReplacer, nil)
		assert.Nil(res)
	})

	t.Run("runtime circular reference detection via SchemaStack", func(t *testing.T) {
		// Create a schema that references itself at runtime (same pointer)
		// This triggers the SchemaStack check (lines 41-45)
		selfRefSchema := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"name": {Type: "string", Enum: []any{"test"}},
			},
		}
		// Make the schema reference itself
		selfRefSchema.Properties["self"] = selfRefSchema

		state := replacer.NewReplaceState()
		res := generateContentFromSchema(selfRefSchema, gen.valueReplacer, state)

		// Should return an object with "name" but "self" should be nil due to circular detection
		resMap, ok := res.(map[string]any)
		assert.True(ok)
		assert.Equal("test", resMap["name"])
		// "self" should be omitted because it hit circular reference
		_, hasSelf := resMap["self"]
		assert.False(hasSelf, "self should be omitted due to circular reference")
	})

	t.Run("valueReplacer returns NULL for primitive", func(t *testing.T) {
		// Test when valueReplacer returns replacer.NULL (lines 77-79)
		nullReplacer := func(s any, state *replacer.ReplaceState) any {
			return replacer.NULL
		}

		s := &schema.Schema{
			Type: "string",
		}
		state := replacer.NewReplaceState().WithOptions(replacer.WithName("field"))
		res := generateContentFromSchema(s, nullReplacer, state)
		assert.Nil(res, "should return nil when valueReplacer returns NULL")
	})

	t.Run("valueReplacer returns NULL at end of function", func(t *testing.T) {
		// Test the NULL check at the end (lines 112-114)
		nullReplacer := func(s any, state *replacer.ReplaceState) any {
			return replacer.NULL
		}

		// Use an empty string type with no namePath to skip the early check
		s := &schema.Schema{
			Type: "string",
		}
		state := replacer.NewReplaceState() // No name in path
		res := generateContentFromSchema(s, nullReplacer, state)
		assert.Nil(res, "should return nil when valueReplacer returns NULL")
	})
}
