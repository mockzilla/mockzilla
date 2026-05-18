package replacer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jaswdr/faker/v2"
)

// patternFaker is the package-local random source for pattern.go.
// Faker's threadSafeRand wrapper makes it safe to share across
// goroutines, so we don't keep a per-call instance like factories.go
// does for the replace context.
var patternFaker = faker.New()

// simpleCharClassRE matches OpenAPI patterns built around a single
// character class, with optional literal prefix and suffix surrounding
// it. Examples: `^[0-9a-fA-F]+$`, `[A-Z]{5}`, `tagValues/[0-9]+`,
// `urn:uuid:[0-9a-f-]+`. Anything fancier (alternation, multiple
// classes, anchors mid-pattern, escapes in the literal parts) falls
// through and the caller keeps its existing padding behaviour.
//
// Captures: 1=literal prefix, 2=character class body, 3=quantifier
// (one of `+`, `*`, `{n}`, `{n,m}`, or empty), 4=literal suffix. The
// literal regions deliberately disallow `\` and `[`; anything with
// escapes or further classes is left to the fallback path.
var simpleCharClassRE = regexp.MustCompile(`^\^?([^\\[$]*)\[([^\]]+)\]([+*]|\{\d+(?:,\d*)?\})?([^\\[$]*)\$?$`)

// generateForPattern returns a random string whose characters satisfy
// the pattern. The result begins with the pattern's literal prefix,
// ends with the literal suffix, and fills the middle with characters
// drawn at random from the character class. The body length honours
// the pattern's quantifier when present (e.g. `{40}` produces exactly
// 40 class characters); otherwise it uses the caller-supplied length
// minus the literal regions, with a floor of one class character.
// Reports ok=false when the pattern is too complex to derive a class
// from.
func generateForPattern(pattern string, length int) (string, bool) {
	if pattern == "" || length <= 0 {
		return "", false
	}
	m := simpleCharClassRE.FindStringSubmatch(pattern)
	if m == nil {
		return "", false
	}

	prefix, classBody, quantifier, suffix := m[1], m[2], m[3], m[4]
	chars := expandCharClass(classBody)
	if len(chars) == 0 {
		return "", false
	}

	classLen := classBodyLen(quantifier, length-len(prefix)-len(suffix))
	if classLen < 1 {
		classLen = 1
	}

	body := make([]byte, classLen)
	for i := range body {
		body[i] = chars[patternFaker.IntBetween(0, len(chars)-1)]
	}

	return prefix + string(body) + suffix, true
}

// classBodyLen returns the number of class characters to emit for the
// given quantifier and target body length (length minus literal
// regions). It respects fixed `{n}` and range `{n,m}` quantifiers and
// otherwise falls back to the target length.
func classBodyLen(quantifier string, target int) int {
	if quantifier == "" || quantifier == "+" || quantifier == "*" {
		return target
	}
	if !strings.HasPrefix(quantifier, "{") || !strings.HasSuffix(quantifier, "}") {
		return target
	}

	body := quantifier[1 : len(quantifier)-1]
	var minStr, maxStr string
	if i := strings.IndexByte(body, ','); i >= 0 {
		minStr, maxStr = body[:i], body[i+1:]
	} else {
		minStr = body
		maxStr = body
	}

	lo, err := strconv.Atoi(minStr)
	if err != nil {
		return target
	}
	hi := lo
	if maxStr != "" {
		if v, err := strconv.Atoi(maxStr); err == nil {
			hi = v
		} else {
			hi = lo
		}
	}

	switch {
	case target < lo:
		return lo
	case target > hi:
		return hi
	default:
		return target
	}
}

// generateForKnownPattern dispatches on a small set of well-known
// pattern shapes (IPv4, IPv4 CIDR, UUID) that are too structurally
// complex for [generateForPattern] to derive a character class from.
// Detection works by trying canonical sample values against the spec
// pattern: if the pattern accepts the sample, returning a fresh value
// of the same shape will also satisfy it. Reports ok=false when no
// known shape fits.
func generateForKnownPattern(pattern string) (string, bool) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", false
	}

	// Check CIDR before plain IPv4: a CIDR-required pattern won't
	// accept a bare IPv4, but a permissive IPv4 pattern may accept a
	// CIDR string as a substring. Trying CIDR first only succeeds when
	// the slash and prefix length are required by the pattern.
	if cidr := randomIPv4CIDR(); re.MatchString(cidr) && !re.MatchString(randomIPv4()) {
		return cidr, true
	}

	if ipv4 := randomIPv4(); re.MatchString(ipv4) {
		return ipv4, true
	}
	if u := patternFaker.UUID().V4(); re.MatchString(u) {
		return u, true
	}
	if tz := randomTZOffset(); re.MatchString(tz) {
		return tz, true
	}
	if d := randomISODate(); re.MatchString(d) {
		return d, true
	}

	// Generic alphanumeric identifier with both cases and digits.
	// Catches patterns like `^[0-9a-zA-Z]*?[a-zA-Z]+[0-9a-zA-Z]*$`
	// (alphanumeric with at least one letter) or `^\w+$`. Anything
	// stricter (digits only, single case, fixed length) is already
	// handled by the simple character-class path before this fallback.
	if word := randomAlphanumWord(); re.MatchString(word) {
		return word, true
	}

	return "", false
}

func randomIPv4() string {
	return patternFaker.Internet().Ipv4()
}

func randomIPv4CIDR() string {
	return fmt.Sprintf("%s/%d", randomIPv4(), patternFaker.IntBetween(1, 32))
}

// randomISODate returns an ISO 8601 calendar date (`YYYY-MM-DD`),
// matching patterns like `^\d{4}-[01]\d-[0-3]\d$` and date-range
// patterns where the second date is optional (agrimetrics'
// `^\d{4}-[01]\d-[0-3]\d(?:-\d{4}-[01]\d-[0-3]\d)?$`).
func randomISODate() string {
	return patternFaker.Time().Time(time.Now()).Format("2006-01-02")
}

// randomTZOffset returns a string in `+HH:MM` / `-HH:MM` form, the
// shape used by ISO 8601 timezone offsets in OpenAPI patterns (e.g.
// Redfish's `([-+][0-1][0-9]:[0-5][0-9])`).
func randomTZOffset() string {
	sign := "+"
	if patternFaker.Bool() {
		sign = "-"
	}
	return fmt.Sprintf("%s%02d:%02d", sign, patternFaker.IntBetween(0, 14), patternFaker.IntBetween(0, 59))
}

// randomAlphanumWord returns an alphanumeric string with at least one
// letter and one digit. It's the fallback sample for OpenAPI patterns
// that compose multiple character classes around a required-letter
// constraint (`^[0-9a-zA-Z]*?[a-zA-Z]+[0-9a-zA-Z]*$`, `^\w+$`, etc.)
// which the simple character-class path doesn't try to derive.
//
// Uses Bothify's `?` (letter) / `#` (digit) template so faker drives
// the randomness consistently with the rest of the package.
func randomAlphanumWord() string {
	return patternFaker.Bothify("?#???#???")
}

// expandCharClass expands a regex character class body (the part inside
// the brackets) into a flat list of allowed bytes. Supports ranges like
// `0-9`, `a-z`, common shorthand escapes (\d, \w, \s) and literal
// characters; ignores negation (`^` at start) since real specs almost
// never use it for length-constrained string formats.
func expandCharClass(class string) string {
	var b strings.Builder
	i := 0
	if strings.HasPrefix(class, "^") {
		i = 1
	}
	for ; i < len(class); i++ {
		if class[i] == '\\' && i+1 < len(class) {
			switch class[i+1] {
			case 'd':
				b.WriteString("0123456789")
			case 'w':
				b.WriteString("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_")
			case 's':
				b.WriteByte(' ')
			default:
				b.WriteByte(class[i+1])
			}
			i++
			continue
		}

		if i+2 < len(class) && class[i+1] == '-' && class[i] <= class[i+2] {
			for c := class[i]; c <= class[i+2]; c++ {
				b.WriteByte(c)
			}
			i += 2
			continue
		}
		b.WriteByte(class[i])
	}
	return b.String()
}
