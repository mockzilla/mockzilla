package factory

import (
	"embed"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

//go:embed testdata/**
var testDataFS embed.FS

func loadTestSpec(t *testing.T, fileName string) []byte {
	t.Helper()
	specContents, err := testDataFS.ReadFile(filepath.Join("testdata", fileName))
	if err != nil {
		t.Fatalf("Error reading file: %v", err)
	}
	return specContents
}

func TestNewFactory(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)
	assert.NotNil(f)
}

func TestNewFactory_WithServiceContext(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	ctx := []byte(`
name: custom-pet
`)
	f, err := NewFactory(spec, WithServiceContext(ctx))
	assert.NoError(err)
	assert.NotNil(f)
}

func TestNewFactory_WithCodegenConfig(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	cfg := codegen.NewDefaultConfiguration()
	f, err := NewFactory(spec, WithCodegenConfig(cfg))
	assert.NoError(err)
	assert.NotNil(f)
}

func TestNewFactory_WithSpecOptions(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec, WithSpecOptions(&config.SpecOptions{Simplify: false}))
	assert.NoError(err)
	assert.NotNil(f)
}

func TestNewFactory_InvalidSpec(t *testing.T) {
	assert := assert2.New(t)

	f, err := NewFactory([]byte(`invalid yaml: [`))
	assert.Error(err)
	assert.Nil(f)
}

func TestFactory_Operations(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	ops := f.Operations()
	assert.Len(ops, 3) // listPets, createPet, getPet
}

func TestFactory_Response(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	t.Run("generates response for valid path", func(t *testing.T) {
		resp, err := f.Response("/pets/{petId}", "GET", nil)
		assert.NoError(err)
		assert.NotEmpty(resp.Body)
	})

	t.Run("generates response for list endpoint", func(t *testing.T) {
		resp, err := f.Response("/pets", "GET", nil)
		assert.NoError(err)
		assert.NotEmpty(resp.Body)
	})

	t.Run("returns error for unknown path", func(t *testing.T) {
		_, err := f.Response("/unknown", "GET", nil)
		assert.Error(err)
		assert.Contains(err.Error(), "no operation found")
	})

	t.Run("with custom context", func(t *testing.T) {
		resp, err := f.Response("/pets/{petId}", "GET", map[string]any{
			"name": "Buddy",
		})
		assert.NoError(err)
		assert.NotEmpty(resp.Body)
	})
}

func TestFactory_Request(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	t.Run("generates request for valid path", func(t *testing.T) {
		req, err := f.Request("/pets", "POST", nil)
		assert.NoError(err)
		assert.NotEmpty(req.Path)
	})

	t.Run("generates request with path params", func(t *testing.T) {
		req, err := f.Request("/pets/{petId}", "GET", nil)
		assert.NoError(err)
		assert.NotEmpty(req.Path)
	})

	t.Run("returns error for unknown path", func(t *testing.T) {
		_, err := f.Request("/unknown", "GET", nil)
		assert.Error(err)
		assert.Contains(err.Error(), "no operation found")
	})
}

func TestFactory_ResponseBody(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	t.Run("returns body bytes for valid path", func(t *testing.T) {
		body, err := f.ResponseBody("/pets/{petId}", "GET", nil)
		assert.NoError(err)
		assert.NotEmpty(body)
	})

	t.Run("returns error for unknown path", func(t *testing.T) {
		_, err := f.ResponseBody("/unknown", "GET", nil)
		assert.Error(err)
	})
}

func TestFactory_RequestBody(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	t.Run("returns body bytes for POST", func(t *testing.T) {
		body, err := f.RequestBody("/pets", "POST", nil)
		assert.NoError(err)
		assert.NotEmpty(body)
	})

	t.Run("returns error for unknown path", func(t *testing.T) {
		_, err := f.RequestBody("/unknown", "POST", nil)
		assert.Error(err)
	})
}

func TestFactory_ResponseBodyFromRequest(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	t.Run("returns body bytes for matched request", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/pets/42", nil)
		body, err := f.ResponseBodyFromRequest(r, nil)
		assert.NoError(err)
		assert.NotEmpty(body)
	})

	t.Run("returns error for unmatched request", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/unknown", nil)
		_, err := f.ResponseBodyFromRequest(r, nil)
		assert.Error(err)
	})
}

func TestFactory_ResponseFromRequest(t *testing.T) {
	assert := assert2.New(t)

	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(err)

	t.Run("matches concrete path to spec pattern", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/pets/42", nil)
		resp, err := f.ResponseFromRequest(r, nil)
		assert.NoError(err)
		assert.NotEmpty(resp.Body)
	})

	t.Run("matches exact path", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/pets", nil)
		resp, err := f.ResponseFromRequest(r, nil)
		assert.NoError(err)
		assert.NotEmpty(resp.Body)
	})

	t.Run("returns error for unmatched path", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/unknown/path", nil)
		_, err := f.ResponseFromRequest(r, nil)
		assert.Error(err)
		assert.Contains(err.Error(), "no matching operation")
	})

	t.Run("with custom context", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/pets/42", nil)
		resp, err := f.ResponseFromRequest(r, map[string]any{
			"name": "Buddy",
		})
		assert.NoError(err)
		assert.NotEmpty(resp.Body)
	})
}
