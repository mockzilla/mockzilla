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
