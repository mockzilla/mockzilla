package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireSpecArg(t *testing.T) {
	t.Run("one arg is allowed", func(t *testing.T) {
		assert.NoError(t, requireSpecArg(nil, []string{"spec.yml"}))
	})

	t.Run("zero args produces a friendly message", func(t *testing.T) {
		err := requireSpecArg(nil, nil)
		require.Error(t, err)
		msg := err.Error()
		assert.Contains(t, msg, "missing <spec> argument")
		assert.Contains(t, msg, "mockzilla simplify --help")
	})

	t.Run("too many args reports the actual count", func(t *testing.T) {
		err := requireSpecArg(nil, []string{"a.yml", "b.yml", "c.yml"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "got 3")
	})
}

// TestBuildOptionalConfig exercises every reachable combination of the three
// optional-property flags. The flags reach the function via pflag.Changed,
// so we drive them by Parse()-ing real argv against a freshly-built command.
func TestBuildOptionalConfig(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		wantNil bool
		wantMin int
		wantMax int
	}{
		{
			name:    "no flags = nil (keep all optional)",
			argv:    []string{"spec.yml"},
			wantNil: true,
		},
		{
			name:    "--optional 5 = exactly 5",
			argv:    []string{"--optional", "5", "spec.yml"},
			wantMin: 5,
			wantMax: 5,
		},
		{
			name:    "--optional 0 = drop all (reachable, unlike the old CLI)",
			argv:    []string{"--optional", "0", "spec.yml"},
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "range mode",
			argv:    []string{"--optional-min", "1", "--optional-max", "3", "spec.yml"},
			wantMin: 1,
			wantMax: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, fixed, min, max := newParsedSimplifyCommand(t, tc.argv)
			got := buildOptionalConfig(cmd, fixed, min, max)
			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.wantMin, got.Min)
			assert.Equal(t, tc.wantMax, got.Max)
		})
	}
}

// newParsedSimplifyCommand returns a simplifyCommand() with argv already
// parsed, so callers can introspect pflag.Changed() the way the real RunE does.
// The bound flag pointers are returned for assertions.
func newParsedSimplifyCommand(t *testing.T, argv []string) (*cobra.Command, int, int, int) {
	t.Helper()
	cmd := simplifyCommand()
	// Suppress RunE — we only need the flag parser to run, not the simplify pipeline.
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	cmd.SetArgs(argv)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	fixed, err := cmd.Flags().GetInt("optional")
	require.NoError(t, err)
	min, err := cmd.Flags().GetInt("optional-min")
	require.NoError(t, err)
	max, err := cmd.Flags().GetInt("optional-max")
	require.NoError(t, err)
	return cmd, fixed, min, max
}

func TestMutuallyExclusiveFlags(t *testing.T) {
	cmd := simplifyCommand()
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	cmd.SetArgs([]string{"--optional", "5", "--optional-min", "1", "--optional-max", "3", "spec.yml"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none of the others can be")
}

func TestOptionalRangeRequiresBothFlags(t *testing.T) {
	cmd := simplifyCommand()
	cmd.RunE = func(*cobra.Command, []string) error { return nil }
	cmd.SetArgs([]string{"--optional-min", "1", "spec.yml"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must all be set")
}

// TestSimplifyEndToEnd runs the real command over a small inline spec and
// verifies the documented behavior end-to-end: required-union flattening,
// optional-union removal, schema-level x-* stripping, and source-indent
// preservation. Examples are deliberately preserved (see simplify_spec.go:205).
func TestSimplifyEndToEnd(t *testing.T) {
	const spec = `openapi: 3.0.3
info:
  title: Test
  version: 1.0.0
paths:
  /things:
    get:
      operationId: getThings
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Thing'
components:
  schemas:
    Thing:
      type: object
      x-internal-marker: should-be-stripped-from-schema
      required:
        - id
        - status
      properties:
        id:
          type: string
        status:
          anyOf:
            - type: string
            - type: integer
        metadata:
          oneOf:
            - type: string
            - type: object
        keep_me:
          type: string
`

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yml")
	require.NoError(t, os.WriteFile(specPath, []byte(spec), 0o644))
	outPath := filepath.Join(dir, "out.yml")

	cmd := simplifyCommand()
	cmd.SetArgs([]string{"--output", outPath, specPath})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	out := string(got)

	t.Run("strips x-* from schemas", func(t *testing.T) {
		assert.NotContains(t, out, "x-internal-marker")
	})
	t.Run("flattens required unions to first variant", func(t *testing.T) {
		// status keeps its property (it's required) but anyOf is gone.
		assert.NotContains(t, out, "anyOf")
		assert.Contains(t, out, "status:")
	})
	t.Run("drops optional union properties entirely", func(t *testing.T) {
		assert.NotContains(t, out, "oneOf")
		assert.NotContains(t, out, "metadata:")
	})
	t.Run("preserves source 2-space indent", func(t *testing.T) {
		// Under the unfixed renderer this would be `    title:` (4 spaces).
		assert.True(t,
			strings.Contains(out, "\n  title:"),
			"expected 2-space indent under 'info:'; got:\n%s", out)
	})
}

func TestReadSpecFromStdin(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	const payload = "openapi: 3.0.0\n"
	go func() {
		defer func() { _ = w.Close() }()
		_, _ = io.WriteString(w, payload)
	}()

	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })

	got, err := readSpec("-")
	require.NoError(t, err)
	assert.Equal(t, payload, string(got))
}

func TestWriteOutputToFileAndStdout(t *testing.T) {
	t.Run("file path writes bytes and reports to stderr", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "out.yml")

		stderr := captureStderr(t, func() {
			require.NoError(t, writeOutput([]byte("hello"), out))
		})

		got, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))
		assert.Contains(t, stderr, out)
	})

	t.Run("dash means stdout (no file written)", func(t *testing.T) {
		stdout := captureStdout(t, func() {
			require.NoError(t, writeOutput([]byte("stdout-bytes"), "-"))
		})
		assert.Equal(t, "stdout-bytes", stdout)
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	return captureFD(t, &os.Stdout, fn)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	return captureFD(t, &os.Stderr, fn)
}

func captureFD(t *testing.T, fd **os.File, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := *fd
	*fd = w
	defer func() { *fd = orig }()

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	fn()
	require.NoError(t, w.Close())
	return string(<-done)
}
