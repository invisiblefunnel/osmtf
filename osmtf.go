// Package osmtf matches OpenStreetMap objects against tag filter
// expressions, mirroring the expression language and matching rules of the
// osmium tags-filter command. Matcher is the streaming API: it takes an
// object's tags one key/value pair at a time, as byte slices, records which
// rule groups match, and allocates nothing.
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
	// them to ways that IsAreaWay accepts and to relations tagged
	// type=multipolygon or type=boundary.
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

// IsAreaWay is osmium's rule for when a way counts as an area. It reports
// whether the way is closed and has at least 4 nodes, where nodeCount is the
// length of the way's node list and closed says whether its first and last
// node are the same.
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
