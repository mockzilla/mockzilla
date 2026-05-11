package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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

// TestBuildOptionalConfig drives the three optional-property flags through
// real argv parsing so pflag.Changed() reflects the user's intent. The
// underlying optional-property semantics are exercised by the pure-logic tests
// in internal/simplify; here we only verify the flag → config mapping.
func TestBuildOptionalConfig(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		wantNil bool
		wantMin int
		wantMax int
	}{
		{name: "no flags = nil", argv: []string{"spec.yml"}, wantNil: true},
		{name: "--optional 5", argv: []string{"--optional", "5", "spec.yml"}, wantMin: 5, wantMax: 5},
		{name: "--optional 0", argv: []string{"--optional", "0", "spec.yml"}, wantMin: 0, wantMax: 0},
		{name: "range mode", argv: []string{"--optional-min", "1", "--optional-max", "3", "spec.yml"}, wantMin: 1, wantMax: 3},
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

func newParsedSimplifyCommand(t *testing.T, argv []string) (*cobra.Command, int, int, int) {
	t.Helper()
	cmd := simplifyCommand()
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

// TestSimplifyCommand_Smoke covers the wrapper's happy path: parse a spec from
// a file, simplify it, write to --output. The simplification correctness is
// covered by internal/simplify; this only verifies the wiring.
func TestSimplifyCommand_Smoke(t *testing.T) {
	const spec = `openapi: 3.0.0
info:
  title: T
  version: 1.0.0
paths:
  /x:
    get:
      operationId: x
      responses:
        '200':
          description: ok
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
	assert.Contains(t, string(got), "openapi:")
	assert.Contains(t, string(got), "/x:")
}

func TestSimplifyCommand_WithConfigFile(t *testing.T) {
	const spec = `openapi: 3.0.0
info:
  title: T
  version: 1.0.0
paths:
  /keep:
    get:
      operationId: keepMe
      responses:
        '200':
          description: ok
  /drop:
    get:
      operationId: dropMe
      responses:
        '200':
          description: ok
`
	const cfg = `filter:
  include:
    paths:
      - /keep
`
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yml")
	cfgPath := filepath.Join(dir, "codegen.yml")
	outPath := filepath.Join(dir, "out.yml")
	require.NoError(t, os.WriteFile(specPath, []byte(spec), 0o644))
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o644))

	cmd := simplifyCommand()
	cmd.SetArgs([]string{"--config", cfgPath, "--output", outPath, specPath})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(got), "/keep:")
	assert.NotContains(t, string(got), "/drop:")
}

func TestSimplifyCommand_ErrorPaths(t *testing.T) {
	t.Run("missing spec file", func(t *testing.T) {
		cmd := simplifyCommand()
		cmd.SetArgs([]string{filepath.Join(t.TempDir(), "does-not-exist.yml")})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading spec")
	})

	t.Run("missing config file", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "spec.yml")
		require.NoError(t, os.WriteFile(specPath, []byte("openapi: 3.0.0\ninfo: {title: T, version: '1'}\npaths: {}\n"), 0o644))

		cmd := simplifyCommand()
		cmd.SetArgs([]string{"--config", filepath.Join(dir, "no-cfg.yml"), specPath})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading config")
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

func TestWriteOutput(t *testing.T) {
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

	t.Run("dash means stdout", func(t *testing.T) {
		stdout := captureStdout(t, func() {
			require.NoError(t, writeOutput([]byte("stdout-bytes"), "-"))
		})
		assert.Equal(t, "stdout-bytes", stdout)
	})

	t.Run("unwritable path errors", func(t *testing.T) {
		err := writeOutput([]byte("x"), filepath.Join(t.TempDir(), "missing-dir", "out.yml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "writing output file")
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
