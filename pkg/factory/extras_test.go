package factory

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewFactory_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec, WithLogger(logger))
	assert.NoError(t, err)
	assert.NotNil(t, f)
}

func TestFactoryMatchPath(t *testing.T) {
	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(t, err)
	_, _ = f.MatchPath("/some/path", "GET")
}

func TestFactoryFindOperation(t *testing.T) {
	spec := loadTestSpec(t, "factory-test.yml")
	f, err := NewFactory(spec)
	assert.NoError(t, err)
	_ = f.FindOperation("/nonexistent", "GET")
}
