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
			name:    "alternation falls through",
			pattern: "^(foo|bar)$",
			ok:      false,
		},
		{
			name:    "literal text outside class falls through",
			pattern: `^\d{4}-\d{2}-\d{2}$`,
			ok:      false,
		},
		{
			name:    "complex with literal prefix falls through",
			pattern: "^abc[0-9]+$",
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
			assert.Len(t, got, tc.length)
			assert.Regexp(t, tc.want, got)
			specMatch, err := regexp.MatchString(tc.pattern, got)
			assert.NoError(t, err)
			assert.True(t, specMatch, "result %q should satisfy spec pattern %q", got, tc.pattern)
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
