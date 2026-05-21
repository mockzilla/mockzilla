package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSpec drops a YAML body into a temp file and returns its path.
// Shared with rules_test.go.
func writeSpec(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// TestSpec_CleanSpec confirms the engine returns no defects for a well-formed
// spec. Failures here mean a rule fires on something innocuous; fix the
// rule, don't relax this test.
func TestSpec_CleanSpec(t *testing.T) {
	spec := writeSpec(t, "ok.yml", `
openapi: 3.0.0
info: {title: t, version: "1"}
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  name: {type: string}
`)
	defects, err := Spec(spec)
	require.NoError(t, err)
	assert.Empty(t, defects)
}

// TestSpec_UnparseableSpec confirms the engine surfaces parse errors rather
// than silently returning zero defects (which would let a broken spec slip
// through the lint gate as if it were clean).
func TestSpec_UnparseableSpec(t *testing.T) {
	spec := writeSpec(t, "bad.yml", "this: is: not: yaml: at: all\n  - and: a: list")
	_, err := Spec(spec)
	require.Error(t, err)
}

// TestSpec_MissingFile checks the explicit "file not readable" error path.
func TestSpec_MissingFile(t *testing.T) {
	_, err := Spec(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	require.Error(t, err)
}

// TestSpec_RealValidationSpecs spot-checks the spec-defect cases we observed
// during validation runs to make sure each lands on the expected rule.
// Skipped when testdata isn't present (e.g. running outside the repo).
func TestSpec_RealValidationSpecs(t *testing.T) {
	cases := []struct {
		spec     string
		wantRule string
	}{
		{"testdata/specs/3.0/misc/dracoon.team.yml", "array-enum-scalars"},
		{"testdata/specs/3.0/misc/stark-bank.yml", "pattern-unicode-circumflex"},
		{"testdata/specs/3.0/misc/chargebee.yml", "additional-props-false-with-oneof"},
		{"testdata/specs/3.0/misc/motaword.com.yml", "allof-non-overlapping-enums"},
		{"testdata/specs/3.0/misc/zuora.yml", "allof-additional-props-conflicts-sibling"},
		{"testdata/specs/3.0/windows.net/graphrbac.1.6.yml", "allof-additional-props-conflicts-sibling"},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			path := filepath.Join("..", "..", tc.spec)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("spec not present: %s", path)
			}
			defects, err := Spec(path)
			require.NoError(t, err)
			require.NotEmpty(t, defects, "expected defects in %s", tc.spec)
			seen := false
			for _, d := range defects {
				if d.Rule == tc.wantRule {
					seen = true
					break
				}
			}
			assert.True(t, seen, "expected to find rule %q among defects: %v", tc.wantRule, defects)
		})
	}
}
