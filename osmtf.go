// Package osmtf matches OpenStreetMap objects against tag filter
// expressions, mirroring the expression language and matching rules of the
// osmium tags-filter command. Matcher is the streaming API: it takes an
// object's tags one key/value pair at a time, as byte slices, records which
// rule groups match, and allocates nothing.
//
// The caller combines the matched groups with geometry it already knows.
// With h the Hits of a Matcher m after an object's last tag, n a way's node
// count, and closed whether the way's first and last node IDs are the same,
// osmium's result for each kind of object is:
//
//	nodeMatches     := h&Nodes != 0
//	wayMatches      := h&Ways != 0 || (h&Areas != 0 && IsAreaWay(n, closed))
//	relationMatches := h&Relations != 0 || (h&Areas != 0 && m.Multipolygon())
package osmtf

import "fmt"

// Types is a bitmask of rule groups.
type Types uint8

const (
	// Nodes is the group of rules for nodes (type letter n).
	Nodes Types = 1 << iota
	// Ways is the group of rules for ways (type letter w).
	Ways
	// Relations is the group of rules for relations (type letter r).
	Relations
	// Areas is the group of rules for areas (type letter a). Osmium applies
	// them to ways that IsAreaWay accepts and to relations whose first type
	// tag is multipolygon or boundary.
	Areas
)

// Kind is the kind of object being matched.
type Kind uint8

const (
	// Node is an OpenStreetMap node, matched against the Nodes group.
	Node Kind = iota
	// Way is an OpenStreetMap way, matched against the Ways and Areas groups.
	Way
	// Relation is an OpenStreetMap relation, matched against the Relations
	// and Areas groups.
	Relation
)

// Filter is a compiled set of expressions. Immutable and safe for
// concurrent use once Compile returns.
type Filter struct {
	rules []rule
	blob  []byte      // all pattern bytes, copied once
	core  [3][]uint32 // indices into rules for each Kind's own group, indexed by Kind
	area  []uint32    // indices into rules of the rules with the Areas bit
	types Types       // union of the rules' types
}

// Compile parses expressions in the osmium tags-filter syntax,
// [TYPES/]KEY[=VALUE] or [TYPES/]KEY!=VALUE, and returns a Filter holding
// all of them. Wildcards, comma lists, and the default types follow the
// osmium-tags-filter man page. Compile with zero expressions succeeds and
// yields a filter that matches nothing. Duplicate expressions are allowed.
//
// The only parse failure, as in osmium, is an unknown type letter: a byte
// other than n, w, r, or a before an expression's first '/'. Compile then
// returns a nil Filter and a *ParseError for the first such expression.
func Compile(exprs ...string) (*Filter, error) {
	// An expression interns at most its own length in pattern bytes, so the
	// blob never reallocates and every pattern shares one backing array.
	size := 0
	for _, expr := range exprs {
		size += len(expr)
	}
	c := compiler{blob: make([]byte, 0, size)}
	f := &Filter{rules: make([]rule, len(exprs))}
	for i, expr := range exprs {
		r, err := c.parseExpr(expr)
		if err != nil {
			return nil, err
		}
		f.rules[i] = r
	}
	f.blob = c.blob

	for i, r := range f.rules {
		for k := range f.core {
			// Kind k's own group is bit 1<<k: Nodes, Ways, or Relations.
			if r.types&(Types(1)<<k) != 0 {
				f.core[k] = append(f.core[k], uint32(i))
			}
		}
		if r.types&Areas != 0 {
			f.area = append(f.area, uint32(i))
		}
		f.types |= r.types
	}
	return f, nil
}

// MustCompile is like Compile but panics if an expression cannot be parsed,
// with the *ParseError as the panic value. It simplifies safe initialization
// of global variables holding filters.
func MustCompile(exprs ...string) *Filter {
	f, err := Compile(exprs...)
	if err != nil {
		panic(err)
	}
	return f
}

// Types returns the union of the groups any rule applies to, so a decoder
// can skip object kinds that cannot match, as osmium does: nodes when
// Types()&Nodes == 0, ways when Types()&(Ways|Areas) == 0, and relations
// when Types()&(Relations|Areas) == 0. Areas is a group, not a kind:
// MustCompile("a/building").Types() is Areas, and its matches are ways and
// relations.
func (f *Filter) Types() Types {
	return f.types
}

// IsAreaWay is osmium's rule for when a way counts as an area. It reports
// whether the way is closed and has at least 4 nodes, where nodeCount is the
// length of the way's node list and closed says whether its first and last
// node IDs are the same.
func IsAreaWay(nodeCount int, closed bool) bool {
	return closed && nodeCount >= 4
}

// ParseError is returned by Compile for an invalid expression.
type ParseError struct {
	// Expr is the expression as given.
	Expr string
	// Pos is the byte offset of the offending byte in Expr.
	Pos int
	// Msg describes the problem, without repeating Expr.
	Msg string
}

// Error returns Msg prefixed with the package name and the quoted Expr, for
// example:
//
//	osmtf: expression "x/amenity": unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')
func (e *ParseError) Error() string {
	return fmt.Sprintf("osmtf: expression %q: %s", e.Expr, e.Msg)
}
