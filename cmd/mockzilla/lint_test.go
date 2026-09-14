package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintCommand(t *testing.T) {
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.yml")
	require.NoError(t, os.WriteFile(clean, []byte(`
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
`), 0o644))
	unparseable := filepath.Join(dir, "unparseable.yml")
	require.NoError(t, os.WriteFile(unparseable, []byte("this: is: not: yaml: at: all\n  - and: a: list"), 0o644))
	broken := filepath.Join(dir, "broken.yml")
	require.NoError(t, os.WriteFile(broken, []byte(`
openapi: 3.0.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Tags:
      type: array
      items: {type: string}
      enum: [a, b]
`), 0o644))

	tests := []struct {
		name    string
		argv    []string
		wantOut string
		wantErr string
	}{
		{name: "clean text", argv: []string{clean}, wantOut: "No defects found\n"},
		{name: "clean json", argv: []string{"--format", "json", clean}, wantOut: `{"defects":[]}` + "\n"},
		{name: "explicit text", argv: []string{"--format=text", clean}, wantOut: "No defects found\n"},
		{
			name:    "defects text",
			argv:    []string{broken},
			wantOut: "components.schemas.Tags: type: array with scalar enum: arrays can never equal a scalar, schema is unsatisfiable [array-enum-scalars]\n",
			wantErr: "1 defect(s) found",
		},
		{
			name:    "defects json",
			argv:    []string{"--format", "json", broken},
			wantOut: `{"defects":[{"rule":"array-enum-scalars","path":"components.schemas.Tags","detail":"type: array with scalar enum: arrays can never equal a scalar, schema is unsatisfiable"}]}` + "\n",
			wantErr: "1 defect(s) found",
		},
		{name: "missing file", argv: []string{filepath.Join(dir, "nope.yml")}, wantErr: "reading spec"},
		{name: "unparseable spec", argv: []string{unparseable}, wantErr: "parse spec"},
		{name: "unknown format", argv: []string{"--format", "sarif", clean}, wantErr: `--format must be text or json, got "sarif"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := lintCommand()
			cmd.SetArgs(tc.argv)
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
			assert.Equal(t, tc.wantOut, out.String())
		})
	}

	t.Run("json write error", func(t *testing.T) {
		cmd := lintCommand()
		cmd.SetArgs([]string{"--format", "json", clean})
		cmd.SetOut(failingWriter{})
		cmd.SetErr(io.Discard)

		err := cmd.Execute()
		require.ErrorIs(t, err, errWriteFailed)
	})
}

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriteFailed }
