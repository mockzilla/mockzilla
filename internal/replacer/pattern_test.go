package replacer

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateForPattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		length  int
		want    string // a regex the result must match
		ok      bool
		// skipLen lets a test case exercise the "prefix+suffix don't fit
		// in length" branch where generateForPattern grows the output
		// past length to keep the pattern satisfiable.
		skipLen bool
	}{
		{
			name:    "hex with anchors and quantifier",
			pattern: "^[0-9a-fA-F]+$",
			length:  40,
			want:    `^[0-9a-fA-F]{40}$`,
			ok:      true,
		},
		{
			name:    "hex without anchors",
			pattern: "[0-9a-fA-F]+",
			length:  16,
			want:    `^[0-9a-fA-F]{16}$`,
			ok:      true,
		},
		{
			name:    "numeric class",
			pattern: "^[0-9]+$",
			length:  10,
			want:    `^[0-9]{10}$`,
			ok:      true,
		},
		{
			name:    "fixed length quantifier",
			pattern: "^[A-Z]{5}$",
			length:  5,
			want:    `^[A-Z]{5}$`,
			ok:      true,
		},
		{
			name:    "fixed length quantifier ignores supplied length",
			pattern: "[a-f0-9]{40}",
			length:  16,
			want:    `^[a-f0-9]{40}$`,
			ok:      true,
			skipLen: true,
		},
		{
			name:    "range quantifier",
			pattern: "[a-z]{3,10}",
			length:  7,
			want:    `^[a-z]{7}$`,
			ok:      true,
		},
		{
			name:    "slug-style class",
			pattern: "^[a-zA-Z0-9_-]+$",
			length:  12,
			want:    `^[a-zA-Z0-9_-]{12}$`,
			ok:      true,
		},
		{
			name:    "shorthand escape \\d",
			pattern: `^[\d]+$`,
			length:  8,
			want:    `^[0-9]{8}$`,
			ok:      true,
		},
		{
			name:    "shorthand \\d without brackets",
			pattern: `^\d{6}$`,
			length:  6,
			want:    `^\d{6}$`,
			ok:      true,
		},
		{
			name:    "shorthand \\w with quantifier",
			pattern: `^\w+$`,
			length:  8,
			want:    `^\w{8}$`,
			ok:      true,
		},
		{
			name:    "literal prefix before class",
			pattern: "tagValues/[0-9]+",
			length:  14,
			want:    `^tagValues/[0-9]{4}$`,
			ok:      true,
		},
		{
			name:    "anchored literal prefix before class",
			pattern: "^abc[0-9]+$",
			length:  10,
			want:    `^abc[0-9]{7}$`,
			ok:      true,
		},
		{
			name:    "literal suffix after class",
			pattern: "[a-z]+@example.com",
			length:  20,
			want:    `^[a-z]+@example\.com$`,
			ok:      true,
		},
		{
			name:    "prefix longer than length still satisfies pattern",
			pattern: "tagValues/[0-9]+",
			length:  5,
			want:    `^tagValues/[0-9]+$`,
			ok:      true,
			skipLen: true,
		},
		{
			name:    "alternation falls through",
			pattern: "^(foo|bar)$",
			ok:      false,
		},
		{
			name:    "escape in literal prefix falls through",
			pattern: `^\d{4}-\d{2}-\d{2}$`,
			ok:      false,
		},
		{
			name:    "empty pattern returns false",
			pattern: "",
			length:  10,
			ok:      false,
		},
		{
			name:    "zero length returns false",
			pattern: "^[0-9]+$",
			length:  0,
			ok:      false,
		},
		{
			name:    "negative length returns false",
			pattern: "^[0-9]+$",
			length:  -1,
			ok:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := generateForPattern(tc.pattern, tc.length)
			assert.Equal(t, tc.ok, ok)
			if !tc.ok {
				return
			}
			if !tc.skipLen {
				assert.Len(t, got, tc.length)
			}
			assert.Regexp(t, tc.want, got)
			specMatch, err := regexp.MatchString(tc.pattern, got)
			assert.NoError(t, err)
			assert.True(t, specMatch, "result %q should satisfy spec pattern %q", got, tc.pattern)
		})
	}
}

func TestGenerateForKnownPattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		want    string // a regex the result must match
		ok      bool
	}{
		{
			name:    "google IPv4 pattern",
			pattern: `[0-9]{1,3}(?:\.[0-9]{1,3}){3}`,
			want:    `^(?:\d{1,3}\.){3}\d{1,3}$`,
			ok:      true,
		},
		{
			name:    "anchored IPv4 pattern",
			pattern: `^[0-9]{1,3}(?:\.[0-9]{1,3}){3}$`,
			want:    `^(?:\d{1,3}\.){3}\d{1,3}$`,
			ok:      true,
		},
		{
			name:    "IPv4 CIDR pattern",
			pattern: `[0-9]{1,3}(?:\.[0-9]{1,3}){3}/[0-9]{1,2}`,
			want:    `^(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}$`,
			ok:      true,
		},
		{
			name:    "UUID pattern",
			pattern: `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			want:    `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			ok:      true,
		},
		{
			name:    "ISO 8601 timezone offset",
			pattern: `([-+][0-1][0-9]:[0-5][0-9])`,
			want:    `^[-+][0-1][0-9]:[0-5][0-9]$`,
			ok:      true,
		},
		{
			name:    "alphanumeric with required letter",
			pattern: `^[0-9a-zA-Z]*?[a-zA-Z]+[0-9a-zA-Z]*$`,
			want:    `^[0-9a-zA-Z]+$`,
			ok:      true,
		},
		{
			name:    "ISO 8601 calendar date",
			pattern: `^\d{4}-[01]\d-[0-3]\d$`,
			want:    `^\d{4}-[01]\d-[0-3]\d$`,
			ok:      true,
		},
		{
			name:    "ISO 8601 date with optional range tail",
			pattern: `^\d{4}-[01]\d-[0-3]\d(?:-\d{4}-[01]\d-[0-3]\d)?$`,
			want:    `^\d{4}-[01]\d-[0-3]\d$`,
			ok:      true,
		},
		{
			name:    "US-style slash date MM/DD/YYYY",
			pattern: `(?:[0-9]{1,2})/([0-9]{1,2})/([0-9]{4})`,
			want:    `^\d{1,2}/\d{1,2}/\d{4}$`,
			ok:      true,
		},
		{
			name:    "Salesforce-style 15/18 char ID",
			pattern: `^[a-zA-Z0-9]{15}|[a-zA-Z0-9]{18}$`,
			want:    `^[a-zA-Z0-9]+$`,
			ok:      true,
		},
		{
			name:    "E.164 phone number",
			pattern: `^\+?[1-9]\d{1,14}$`,
			want:    `^\+?[1-9]\d+$`,
			ok:      true,
		},
		{
			name:    "clock time HH:MM:SS",
			pattern: `^([01]\d|2[0-3]):[0-5]\d:[0-5]\d$`,
			want:    `^[0-2]\d:[0-5]\d:[0-5]\d$`,
			ok:      true,
		},
		{
			name:    "clock time with optional MM:SS / HH:MM:SS",
			pattern: `^(?:(?:([01]?\d|2[0-3]):)?([0-5]?\d):)?([0-5]?\d)$`,
			want:    `^\d{1,2}(?::\d{1,2})*$`,
			ok:      true,
		},
		{
			name:    "clock time HH:MM only",
			pattern: `^([01]\d|2[0-3]):[0-5]\d$`,
			want:    `^[0-2]\d:[0-5]\d$`,
			ok:      true,
		},
		{
			name:    "literal alternation used as enum",
			pattern: `ECOMMERCE|MOTO|IN_STORE|TELESALES`,
			want:    `^(?:ECOMMERCE|MOTO|IN_STORE|TELESALES)$`,
			ok:      true,
		},
		{
			name:    "anchored literal alternation",
			pattern: `^(foo|bar|baz)$`,
			want:    `^(?:foo|bar|baz)$`,
			ok:      true,
		},
		{
			name:    "single literal anchor (pattern-as-const)",
			pattern: `^warning$`,
			want:    `^warning$`,
			ok:      true,
		},
		{
			name:    "single literal anchor with slash",
			pattern: `^/$`,
			want:    `^/$`,
			ok:      true,
		},
		{
			name:    "SemVer with optional pre-release and build",
			pattern: `^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`,
			want:    `^\d+\.\d+\.\d+$`,
			ok:      true,
		},
		{
			name:    "versioned URL with /v[0-1] suffix",
			pattern: `.+/v[0-1](\.[0-9]+)*/?$`,
			want:    `^https?://.+/v[0-1](\.\d+)?/?$`,
			ok:      true,
		},
		{
			name:    "bounded decimal string",
			pattern: `^\d{0,13}(?:\.\d{0,2})?$`,
			want:    `^\d+(\.\d+)?$`,
			ok:      true,
		},
		{
			name:    "alternation with metacharacter matches via fallback generator",
			pattern: `^(foo|\d+)$`,
			want:    `^(foo|\d+)$`,
			ok:      true,
		},
		{
			name:    "unrelated literal falls through",
			pattern: `^foo-bar-[0-9]+$`,
			ok:      false,
		},
		{
			name:    "invalid regex falls through",
			pattern: `[`,
			ok:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := generateForKnownPattern(tc.pattern)
			assert.Equal(t, tc.ok, ok)
			if !tc.ok {
				return
			}
			assert.Regexp(t, tc.want, got)
			specMatch, err := regexp.MatchString(tc.pattern, got)
			assert.NoError(t, err)
			assert.True(t, specMatch, "result %q should satisfy spec pattern %q", got, tc.pattern)
		})
	}
}

func TestGenerateForPatternMultiAtom(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"slack user id", `^[UW][A-Z0-9]{2,}$`},
		{"slack timestamp", `^\d{10}\.\d{6}$`},
		{"prefix and digits", `^foo-bar-[a-z]+\.\d{2}$`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := generateForPattern(tc.pattern, 10)
			assert.True(t, ok, "should generate for %q", tc.pattern)
			matched, err := regexp.MatchString(tc.pattern, got)
			assert.NoError(t, err)
			assert.True(t, matched, "%q should match %q", got, tc.pattern)
		})
	}
}

func TestGenerateForPatternRejectsLeakedSpecialChars(t *testing.T) {
	// The single-class regex would accept `(A` as prefix and `)?` as suffix,
	// producing `(AQ)?` which doesn't satisfy the actual pattern. The
	// post-generation match check must reject any value that contains raw
	// `(` or `)` even if a generator path emits them.
	pattern := `^(A[A-Z0-9]{1,})?$`
	for i := 0; i < 50; i++ {
		got, ok := generateForPattern(pattern, 10)
		if !ok {
			continue
		}
		assert.NotContains(t, got, "(", "value %q must not contain raw `(`", got)
		assert.NotContains(t, got, ")", "value %q must not contain raw `)`", got)
	}
}

func TestGenerateForAlternationPattern(t *testing.T) {
	patterns := []string{
		`^((\d{3}\.\d{3}\.\d{3}\-\d{2})|(\d{11})|(\d{2}\.\d{3}\.\d{3}\/\d{4}\-\d{2})|(\d{14}))$`,
		`^\d{1,20}$|^\d{1,19}-\d{1}$`,
	}
	for _, p := range patterns {
		t.Run(p, func(t *testing.T) {
			got, ok := generateForPattern(p, 10)
			assert.True(t, ok, "should generate for %q", p)
			matched, err := regexp.MatchString(p, got)
			assert.NoError(t, err)
			assert.True(t, matched, "%q should match %q", got, p)
		})
	}
}

func TestPatternHasInternalAnchors(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{`ˆ^\d{1,8}$`, true},
		{`ˆ^\d{4}(\-\d{1})?$`, true},
		{`^[A-Z]+$`, false},
		{`[^a-z]+`, false},
		{`\$10`, false},
		{`^foo$bar$`, true},
		// alternation: at least one branch satisfiable means overall is.
		{`ˆ^\d+$|^\d+$`, false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			assert.Equal(t, tc.want, patternHasInternalAnchors(tc.pattern))
		})
	}
}

func TestPatternAllowsEmptyString(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{`^(A[A-Z0-9]{1,})?$`, true},
		{`^[A-Z]*$`, true},
		{``, false},
		{`^[A-Z]+$`, false},
		{`^\d{10}$`, false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			assert.Equal(t, tc.want, patternAllowsEmptyString(tc.pattern))
		})
	}
}

func TestIsJSRegexLiteralPattern(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{"/^[0-9]{5}$/i", true},
		{"/abc/i", true},
		{"/abc/gi", true},
		{"/.+$/m", true},
		{"^[0-9]{5}$", false},
		{"^/.+/$", false},
		{"/abc", false},
		{"/abc/", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			assert.Equal(t, tc.want, isJSRegexLiteralPattern(tc.pattern))
		})
	}
}

func TestExpandCharClass(t *testing.T) {
	cases := []struct {
		name  string
		class string
		want  string
	}{
		{"hex range", "0-9a-fA-F", "0123456789abcdefABCDEF"},
		{"single chars", "_-+.", "_-+."},
		{"escape d", `\d`, "0123456789"},
		{"mixed range and literal", "a-z_", "abcdefghijklmnopqrstuvwxyz_"},
		{"negation marker is stripped", "^abc", "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, expandCharClass(tc.class))
		})
	}
}
