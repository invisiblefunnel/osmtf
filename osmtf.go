// Package osmtf matches OpenStreetMap objects against tag filter
// expressions, mirroring the expression language and matching rules of the
// osmium tags-filter command. Matcher is the streaming API: it takes an
// object's tags one key/value pair at a time, as byte slices or strings,
// and allocates nothing. BeginNode, BeginWay, or BeginRelation starts each
// object; Matches gives osmium's result after any tag. A true result is final;
// a false result is final only after the last tag.
//
// BeginWay requires the way's reference count and whether its first and last
// node IDs are the same. Osmium applies area rules to closed ways with at
// least four references and to relations whose first type tag is multipolygon
// or boundary. Matcher tracks relation type tags itself.
package osmtf

import "fmt"

// ruleTypes is a bitmask of rule groups.
type ruleTypes uint8

const (
	// nodes is the group of rules for nodes (type letter n).
	nodes ruleTypes = 1 << iota
	// ways is the group of rules for ways (type letter w).
	ways
	// relations is the group of rules for relations (type letter r).
	relations
	// areas is the group of rules for areas (type letter a). Osmium applies
	// them to closed ways with 4 or more nodes and to relations whose first
	// type tag is multipolygon or boundary.
	areas
)

// Kind is the kind of object being matched.
type Kind uint8

const (
	// Node is an OpenStreetMap node.
	Node Kind = iota
	// Way is an OpenStreetMap way.
	Way
	// Relation is an OpenStreetMap relation.
	Relation
)

// kindTypes[k] is the groups whose rules osmium applies to objects of Kind k.
var kindTypes = [...]ruleTypes{Node: nodes, Way: ways | areas, Relation: relations | areas}

// String returns the constant's name, Node, Way, or Relation, or Kind(n) for
// any other value.
func (k Kind) String() string {
	switch k {
	case Node:
		return "Node"
	case Way:
		return "Way"
	case Relation:
		return "Relation"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}

// Filter is a compiled set of expressions. Immutable and safe for
// concurrent use once Compile returns.
type Filter struct {
	rules []rule
	blob  []byte      // all pattern bytes, copied once
	core  [3][]uint32 // indices into rules for each Kind's own group, indexed by Kind
	area  []uint32    // indices into rules of the rules with the areas bit
	types ruleTypes   // union of the rules' types
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
			// Kind k's own group is bit 1<<k: nodes, ways, or relations.
			if r.types&(ruleTypes(1)<<k) != 0 {
				f.core[k] = append(f.core[k], uint32(i))
			}
		}
		if r.types&areas != 0 {
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

// CanMatch reports whether any rule can match an object of the given kind,
// so a decoder can skip the kinds that cannot, as osmium does. Area rules
// count for ways and relations: MustCompile("a/building").CanMatch(Way) is
// true. It panics with "osmtf: invalid Kind" if kind is not Node, Way, or
// Relation.
func (f *Filter) CanMatch(kind Kind) bool {
	if kind > Relation {
		panic("osmtf: invalid Kind")
	}
	return f.types&kindTypes[kind] != 0
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
