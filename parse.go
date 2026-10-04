package osmtf

import "strings"

// matchKind is how a strMatcher compares a string with its pattern.
type matchKind uint8

const (
	matchAny       matchKind = iota // anything
	matchEqual                      // exactly pat
	matchPrefix                     // starts with pat
	matchSubstring                  // contains pat
	matchList                       // exactly one of the list items
)

// strMatcher is a compiled key or value pattern. Its byte slices come from
// compiler.intern.
type strMatcher struct {
	kind matchKind
	pat  []byte   // matchEqual, matchPrefix, matchSubstring
	list [][]byte // matchList
}

// compiler holds the state shared while compiling expressions.
type compiler struct {
	blob []byte // every interned pattern byte
}

// intern appends s to c.blob and returns the appended bytes. The result's
// capacity equals its length, so an append through it copies instead of
// overwriting the bytes after it in the blob. Callers give blob enough
// capacity up front that intern never reallocates, so all interned bytes
// share one backing array.
func (c *compiler) intern(s string) []byte {
	n := len(c.blob)
	c.blob = append(c.blob, s...)
	return c.blob[n : n+len(s) : n+len(s)]
}

// trimSpaces removes ASCII spaces (0x20) from both ends of s. Tabs and other
// whitespace are kept, as osmium does.
func trimSpaces(s string) string {
	return strings.Trim(s, " ")
}

// stringMatcher compiles a raw key or value with osmium's construction
// rules, tried in order, and interns the resulting pattern bytes.
func (c *compiler) stringMatcher(raw string) strMatcher {
	s := trimSpaces(raw)
	switch {
	case s == "*":
		return strMatcher{kind: matchAny}
	case s == "" || (s[0] != '*' && s[len(s)-1] != '*'):
		if !strings.Contains(s, ",") {
			return strMatcher{kind: matchEqual, pat: c.intern(s)}
		}
		// List items are literal: a '*' inside one is not special.
		items := strings.Split(s, ",")
		list := make([][]byte, len(items))
		for i, item := range items {
			list[i] = c.intern(trimSpaces(item))
		}
		return strMatcher{kind: matchList, list: list}
	case s[len(s)-1] == '*' && s[0] != '*':
		return strMatcher{kind: matchPrefix, pat: c.intern(s[:len(s)-1])}
	default:
		// The first byte is '*'. Drop it, then a trailing '*' if one is left.
		pat := strings.TrimSuffix(s[1:], "*")
		return strMatcher{kind: matchSubstring, pat: c.intern(pat)}
	}
}
