package osmtf

import (
	"fmt"
	"strings"
)

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

// rule is one compiled expression.
type rule struct {
	types ruleTypes
	key   strMatcher
	value strMatcher // kind matchAny for key-only rules
	want  bool       // false when inverted
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

// parseExpr compiles one expression, [TYPES/]REST, with osmium's grammar and
// interns its patterns. The only failure is an unknown type letter, which is
// always reported as a *ParseError.
func (c *compiler) parseExpr(expr string) (rule, error) {
	types := nodes | ways | relations
	rest := expr
	switch p := strings.IndexByte(expr, '/'); {
	case p == 0:
		rest = expr[1:]
	case p > 0:
		types = 0
		for i := 0; i < p; i++ {
			switch expr[i] {
			case 'n':
				types |= nodes
			case 'w':
				types |= ways
			case 'r':
				types |= relations
			case 'a':
				types |= areas
			default:
				// %c formats the byte as the rune of the same value, so Msg is
				// valid UTF-8 even for a non-ASCII byte.
				return rule{}, &ParseError{
					Expr: expr,
					Pos:  i,
					Msg:  fmt.Sprintf("unknown object type '%c' (allowed are 'n', 'w', 'r', and 'a')", expr[i]),
				}
			}
		}
		rest = expr[p+1:]
	}

	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		// Key-only: any value matches, and a trailing '!' is part of the key.
		return rule{
			types: types,
			key:   c.stringMatcher(rest),
			value: strMatcher{kind: matchAny},
			want:  true,
		}, nil
	}
	// The '!' of "!=" is looked for on the raw key, before stringMatcher trims
	// it, so "k !=v" is inverted and "k! =v" is not.
	key, inverted := strings.CutSuffix(rest[:eq], "!")
	return rule{
		types: types,
		key:   c.stringMatcher(key),
		value: c.stringMatcher(rest[eq+1:]),
		want:  !inverted,
	}, nil
}
