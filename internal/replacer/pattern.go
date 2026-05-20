package replacer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jaswdr/faker/v2"
)

var patternFaker = faker.New()

var simpleCharClassRE = regexp.MustCompile(`^\^?([^\\[$]*)(?:\[([^\]]+)\]|\\([dws]))([+*]|\{\d+(?:,\d*)?\})?([^\\[$]*)\$?$`)

var jsRegexLiteralRE = regexp.MustCompile(`^/.+/[gimsuy]+$`)

// isJSRegexLiteralPattern reports whether the pattern was authored as a
// JavaScript regex literal (e.g. `/^[0-9]{5}$/i`) rather than a JSON Schema
// regex source. The validator compiles the whole `/source/flags` string as
// the source, which is unsatisfiable for any pattern with internal anchors
// and contains no useful value for the rest.
func isJSRegexLiteralPattern(p string) bool {
	return jsRegexLiteralRE.MatchString(p)
}

// patternHasInternalAnchors reports unsatisfiable `^`/`$` placements (anywhere
// but the start/end). Handles `\^`/`\$` escapes and class negation `[^...]`.
// Splits on top-level `|` so a typo'd branch doesn't taint a satisfiable one.
func patternHasInternalAnchors(p string) bool {
	for _, alt := range splitTopLevelAlternatives(p) {
		if !singleBranchHasInternalAnchors(alt) {
			return false
		}
	}
	return true
}

func singleBranchHasInternalAnchors(p string) bool {
	inClass := false
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			if i+1 < len(p) {
				i++
			}
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '^':
			if !inClass && i != 0 {
				return true
			}
		case '$':
			if !inClass && i != len(p)-1 {
				return true
			}
		}
	}
	return false
}

// patternAllowsEmptyString reports whether the empty string satisfies the
// pattern (e.g. `^(...)?$`, `^[A-Z]*$`). Used to let `""` flow through the
// replacer chain for fields where omission and empty are both spec-valid.
func patternAllowsEmptyString(p string) bool {
	if p == "" {
		return false
	}
	return patternMatches(p, "")
}

func generateForPattern(pattern string, length int) (string, bool) {
	if pattern == "" || length <= 0 {
		return "", false
	}
	if v, ok := generateForSingleCharClassPattern(pattern, length); ok && patternMatches(pattern, v) {
		return v, true
	}
	if atoms := tokenizePattern(pattern); atoms != nil {
		if v, ok := generateFromAtoms(atoms); ok && patternMatches(pattern, v) {
			return v, true
		}
	}
	if v, ok := generateForAlternationPattern(pattern); ok && patternMatches(pattern, v) {
		return v, true
	}
	if expanded, ok := expandGroupsInPattern(pattern); ok {
		if atoms := tokenizePattern(expanded); atoms != nil {
			if v, ok := generateFromAtoms(atoms); ok && patternMatches(pattern, v) {
				return v, true
			}
		}
	}
	return "", false
}

// generateForAlternationPattern handles `^(A|B|C)$`-shaped patterns where
// each alternative is itself tokenizable (e.g. Brazilian taxId formats:
// `^((\d{3}\.\d{3}\.\d{3}\-\d{2})|(\d{11})|...)$`). Also handles
// `^(A)$|^(B)$|^(C)$` where each branch carries its own anchors. Picks
// one alternative at random and emits a value matching it.
func generateForAlternationPattern(pattern string) (string, bool) {
	p := strings.TrimPrefix(pattern, "^")
	p = strings.TrimSuffix(p, "$")
	p = stripOuterParens(p)
	if !strings.Contains(p, "|") {
		return "", false
	}
	alts := splitTopLevelAlternatives(p)
	if len(alts) < 2 {
		return "", false
	}
	start := patternFaker.IntBetween(0, len(alts)-1)
	for i := 0; i < len(alts); i++ {
		alt := alts[(start+i)%len(alts)]
		alt = strings.TrimPrefix(alt, "^")
		alt = strings.TrimSuffix(alt, "$")
		alt = stripOuterParens(alt)
		atoms := tokenizePattern(alt)
		if atoms == nil {
			continue
		}
		if v, ok := generateFromAtoms(atoms); ok {
			return v, true
		}
	}
	return "", false
}

// expandGroupsInPattern flattens parenthesized groups for the tokenizer:
// innermost first, alternation picks a branch, quantifiers collapse to finite
// emission, optional groups drop content half the time. Returns false on
// unsupported syntax (lookarounds, unbalanced parens).
func expandGroupsInPattern(pattern string) (string, bool) {
	p := pattern
	for steps := 0; steps < 64; steps++ {
		start, end, ok := findInnermostGroup(p)
		if !ok {
			return p, true
		}
		content := p[start+1 : end]
		if strings.HasPrefix(content, "?:") {
			content = content[2:]
		} else if strings.HasPrefix(content, "?<") {
			if closes := strings.IndexByte(content, '>'); closes >= 0 {
				content = content[closes+1:]
			} else {
				return "", false
			}
		} else if strings.HasPrefix(content, "?=") || strings.HasPrefix(content, "?!") {
			return "", false
		}

		if strings.Contains(content, "|") {
			alts := splitTopLevelAlternatives(content)
			content = alts[patternFaker.IntBetween(0, len(alts)-1)]
		}

		after := end + 1
		repeat := 1
		drop := false
		if after < len(p) {
			switch p[after] {
			case '?':
				if patternFaker.Bool() {
					drop = true
				}
				after++
			case '+', '*':
				after++
			case '{':
				closes := strings.IndexByte(p[after:], '}')
				if closes < 0 {
					return "", false
				}
				body := p[after+1 : after+closes]
				var minStr string
				if idx := strings.IndexByte(body, ','); idx >= 0 {
					minStr = body[:idx]
				} else {
					minStr = body
				}
				if n, err := strconv.Atoi(minStr); err == nil && n > 0 {
					repeat = n
				}
				after += closes + 1
			}
		}
		var rep string
		if !drop {
			if repeat == 1 {
				rep = content
			} else {
				var b strings.Builder
				for i := 0; i < repeat; i++ {
					b.WriteString(content)
				}
				rep = b.String()
			}
		}
		p = p[:start] + rep + p[after:]
	}
	return "", false
}

func findInnermostGroup(p string) (start, end int, ok bool) {
	stack := []int{}
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			if i+1 < len(p) {
				i++
			}
		case '[':
			j := i + 1
			for j < len(p) && p[j] != ']' {
				if p[j] == '\\' && j+1 < len(p) {
					j += 2
				} else {
					j++
				}
			}
			i = j
		case '(':
			stack = append(stack, i)
		case ')':
			if len(stack) > 0 {
				return stack[len(stack)-1], i, true
			}
		}
	}
	return 0, 0, false
}

func splitTopLevelAlternatives(p string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			if i+1 < len(p) {
				i++
			}
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '|':
			if depth == 0 {
				out = append(out, p[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, p[start:])
	return out
}

// stripOuterParens removes a single pair of parentheses that wraps the
// entire string. Returns the input unchanged if the outer parens close
// before the end (i.e. they don't actually wrap everything).
func stripOuterParens(p string) string {
	if len(p) < 2 || p[0] != '(' || p[len(p)-1] != ')' {
		return p
	}
	depth := 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '\\':
			if i+1 < len(p) {
				i++
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i < len(p)-1 {
				return p
			}
		}
	}
	return p[1 : len(p)-1]
}

// patternMatches is a post-generation sanity check. The single-class regex
// permits special chars (`(`, `)?`, etc.) in prefix/suffix that it can't
// reproduce verbatim, so a generated value may not actually satisfy the
// pattern. Verify before returning.
func patternMatches(pattern, value string) bool {
	matched, err := regexp.MatchString(pattern, value)
	return err == nil && matched
}

func generateForSingleCharClassPattern(pattern string, length int) (string, bool) {
	m := simpleCharClassRE.FindStringSubmatch(pattern)
	if m == nil {
		return "", false
	}

	prefix, classBody, shorthand, quantifier, suffix := m[1], m[2], m[3], m[4], m[5]
	if classBody == "" && shorthand != "" {
		classBody = `\` + shorthand
	}
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

// patternAtom is one element of a tokenized regex: either a literal char
// (must appear verbatim) or a char-class (one of `chars`), optionally
// followed by a quantifier.
type patternAtom struct {
	chars      string
	quantifier string
	literal    bool
}

// tokenizePattern parses a JSON Schema pattern into atoms+quantifiers. Returns
// nil on unsupported constructs (alternation, groups, lookarounds, non-edge
// anchors). Handles `[...]`, `\d`/`\w`/`\s`, escaped and plain literals.
func tokenizePattern(p string) []patternAtom {
	p = strings.TrimPrefix(p, "^")
	p = strings.TrimSuffix(p, "$")
	var out []patternAtom
	i := 0

	for i < len(p) {
		var a patternAtom
		switch c := p[i]; c {
		case '[':
			end := strings.IndexByte(p[i+1:], ']')
			if end < 0 {
				return nil
			}
			a.chars = expandCharClass(p[i+1 : i+1+end])
			if a.chars == "" {
				return nil
			}
			i += end + 2
		case '\\':
			if i+1 >= len(p) {
				return nil
			}
			switch nx := p[i+1]; nx {
			case 'd':
				a.chars = "0123456789"
			case 'w':
				a.chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
			case 's':
				a.chars = " "
			default:
				a.chars = string(nx)
				a.literal = true
			}
			i += 2
		case '(', ')', '|', '^', '$', '.', '?':
			return nil
		default:
			a.chars = string(c)
			a.literal = true
			i++
		}
		if i < len(p) {
			switch p[i] {
			case '+', '*':
				a.quantifier = string(p[i])
				i++
			case '{':
				end := strings.IndexByte(p[i:], '}')
				if end < 0 {
					return nil
				}
				a.quantifier = p[i : i+end+1]
				i += end + 1
			}
		}
		out = append(out, a)
	}
	return out
}

func generateFromAtoms(atoms []patternAtom) (string, bool) {
	if len(atoms) == 0 {
		return "", false
	}
	var out []byte
	for _, a := range atoms {
		if len(a.chars) == 0 {
			return "", false
		}
		repeats := 1
		if a.quantifier != "" {
			repeats = classBodyLen(a.quantifier, 1)
			if repeats < 1 {
				repeats = 1
			}
		}
		if a.literal {
			for i := 0; i < repeats; i++ {
				out = append(out, a.chars...)
			}
			continue
		}
		for i := 0; i < repeats; i++ {
			out = append(out, a.chars[patternFaker.IntBetween(0, len(a.chars)-1)])
		}
	}
	return string(out), true
}

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

func generateForKnownPattern(pattern string) (string, bool) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", false
	}

	if v, ok := generateForLiteralAlternation(pattern); ok && re.MatchString(v) {
		return v, true
	}

	for _, dt := range randomISODateTimes() {
		if re.MatchString(dt) {
			return dt, true
		}
	}

	if strings.Contains(pattern, "@") {
		if e := randomEmail(); re.MatchString(e) {
			return e, true
		}
	}

	// CIDR must be tried before plain IPv4: a permissive IPv4 pattern
	// may also match a CIDR substring, but a CIDR-required pattern
	// won't accept a bare IPv4.
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
	if d := randomSlashDate(); re.MatchString(d) {
		return d, true
	}
	for _, t := range randomClockTimes() {
		if re.MatchString(t) {
			return t, true
		}
	}
	if t := randomClockHM(); re.MatchString(t) {
		return t, true
	}
	if v := randomSemVer(); re.MatchString(v) {
		return v, true
	}
	if u := randomVersionedURL(); re.MatchString(u) {
		return u, true
	}
	if d := randomDecimalString(); re.MatchString(d) {
		return d, true
	}
	if d := randomIntString(); re.MatchString(d) {
		return d, true
	}
	if word := randomAlphanumWord(); re.MatchString(word) {
		return word, true
	}
	for _, length := range []int{15, 18, 24, 32} {
		if id := randomAlphanumID(length); re.MatchString(id) {
			return id, true
		}
	}
	if phone := randomE164Phone(); re.MatchString(phone) {
		return phone, true
	}

	return "", false
}

// generateForLiteralAlternation handles patterns used as enums:
// `ECOMMERCE|MOTO|IN_STORE|TELESALES`, `^(foo|bar|baz)$`, and the
// degenerate single-literal case `^warning$` (pattern-as-const). Returns
// false unless every alternative is a plain literal with no regex
// metacharacters.
func generateForLiteralAlternation(pattern string) (string, bool) {
	p := strings.TrimPrefix(pattern, "^")
	p = strings.TrimSuffix(p, "$")
	if strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")") {
		p = p[1 : len(p)-1]
		p = strings.TrimPrefix(p, "?:")
	}
	parts := strings.Split(p, "|")
	for _, part := range parts {
		if !isPlainLiteral(part) {
			return "", false
		}
	}
	return parts[patternFaker.IntBetween(0, len(parts)-1)], true
}

func isPlainLiteral(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '^', '$', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|':
			return false
		}
	}
	return true
}

func randomIPv4() string {
	return patternFaker.Internet().Ipv4()
}

func randomIPv4CIDR() string {
	return fmt.Sprintf("%s/%d", randomIPv4(), patternFaker.IntBetween(1, 32))
}

func randomISODate() string {
	return patternFaker.Time().Time(time.Now()).Format("2006-01-02")
}

func randomISODateTimes() []string {
	t := patternFaker.Time().Time(time.Now()).UTC()
	return []string{
		t.Format("2006-01-02T15:04:05.000Z"),
		t.Format("2006-01-02T15:04:05Z"),
		t.Format("2006-01-02T15:04:05.000"),
		t.Format("2006-01-02T15:04:05"),
	}
}

func randomEmail() string {
	return patternFaker.Internet().Email()
}

func randomSlashDate() string {
	return patternFaker.Time().Time(time.Now()).Format("01/02/2006")
}

func randomClockTimes() []string {
	hh, mm, ss := patternFaker.IntBetween(0, 23), patternFaker.IntBetween(0, 59), patternFaker.IntBetween(0, 59)
	mmm := patternFaker.IntBetween(0, 999)
	tzh := patternFaker.IntBetween(0, 23)
	tzm := patternFaker.IntBetween(0, 59)
	base := fmt.Sprintf("%02d:%02d:%02d", hh, mm, ss)
	baseMs := fmt.Sprintf("%s.%03d", base, mmm)
	offset := fmt.Sprintf("+%02d:%02d", tzh, tzm)
	return []string{
		baseMs,
		baseMs + "Z",
		baseMs + offset,
		base,
		base + "Z",
		base + offset,
	}
}

func randomClockHM() string {
	return fmt.Sprintf("%02d:%02d", patternFaker.IntBetween(0, 23), patternFaker.IntBetween(0, 59))
}

func randomSemVer() string {
	return fmt.Sprintf("%d.%d.%d", patternFaker.IntBetween(0, 9), patternFaker.IntBetween(0, 99), patternFaker.IntBetween(0, 999))
}

func randomVersionedURL() string {
	return fmt.Sprintf("https://api.example.com/v%d.%d", patternFaker.IntBetween(0, 1), patternFaker.IntBetween(0, 9))
}

func randomDecimalString() string {
	return fmt.Sprintf("%d.%d", patternFaker.IntBetween(0, 9), patternFaker.IntBetween(0, 9))
}

func randomIntString() string {
	return strconv.Itoa(patternFaker.IntBetween(0, 9))
}

func randomTZOffset() string {
	sign := "+"
	if patternFaker.Bool() {
		sign = "-"
	}
	return fmt.Sprintf("%s%02d:%02d", sign, patternFaker.IntBetween(0, 14), patternFaker.IntBetween(0, 59))
}

func randomAlphanumWord() string {
	return patternFaker.Bothify("?#???#???")
}

func randomE164Phone() string {
	digits := patternFaker.IntBetween(7, 14)
	out := []byte{'+', byte('1' + patternFaker.IntBetween(0, 8))}
	for i := 0; i < digits; i++ {
		out = append(out, byte('0'+patternFaker.IntBetween(0, 9)))
	}
	return string(out)
}

func randomAlphanumID(length int) string {
	if length <= 0 {
		return ""
	}
	b := make([]byte, length)
	for i := range b {
		if i%3 == 0 {
			b[i] = '#'
		} else {
			b[i] = '?'
		}
	}
	return patternFaker.Bothify(string(b))
}

// expandCharClass ignores negation (`^` at start of class body); none of
// the OpenAPI string patterns we see in the wild rely on negated classes
// for length-constrained samples.
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
