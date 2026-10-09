package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCommand(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "openapi.yml")
	require.NoError(t, os.WriteFile(spec, []byte(`
openapi: 3.0.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      type: object
      properties:
        name: {type: string}
`), 0o644))
	out := filepath.Join(dir, "api", "gen.go")

	tests := []struct {
		name     string
		argv     []string
		wantCode int
		wantErr  string
		wantFile bool
	}{
		{name: "writes models", argv: []string{"generate", spec, "-o", out, "-package", "api"}, wantFile: true},
		{
			name:     "messages name mockzilla",
			argv:     []string{"generate", "a.yml", "b.yml"},
			wantCode: 2,
			wantErr:  "mockzilla: generate takes at most one spec, got 2\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runGenerate(tc.argv, &stdout, &stderr)

			assert.Equal(t, tc.wantCode, code)
			assert.Equal(t, tc.wantErr, stderr.String())
			if !tc.wantFile {
				return
			}
			got, err := os.ReadFile(out)
			require.NoError(t, err)
			assert.Contains(t, string(got), "package api\n")
			assert.Contains(t, string(got), "type Pet struct {")
		})
	}
}
