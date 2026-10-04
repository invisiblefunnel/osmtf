# osm-tag-filter: design

Date: 2026-10-04
Status: approved in conversation, awaiting written review

## Purpose

A Go library that decides whether an OpenStreetMap object matches a set of tag
filter expressions, using exactly the expression language and matching rules
of `osmium tags-filter`. It is meant to sit inside a PBF or XML decoder's hot
loop: compile once, then test millions of objects with no heap allocation.

Success means:

- Any expression accepted by `osmium tags-filter` is accepted here with the
  same meaning, and vice versa, verified against the osmium binary.
- `Begin`, `Tag`, `Hits`, and `Multipolygon` perform zero allocations.
- The caller never has to build a slice of tags. Tags are fed one pair at a
  time as byte slices, and the caller decides when to stop.
- One compiled filter is safe to share across goroutines.

## Scope

In scope for v1:

- Type prefixes `n`, `w`, `r`, `a` in any combination, or none.
- Key-only, key=value, comma lists, `*` any, `prefix*`, `*substring`,
  `*substring*`, and `!=` negation.
- Area semantics: area rules apply to closed ways with 4 or more nodes and to
  relations tagged `type=multipolygon` or `type=boundary`.

Out of scope for v1:

- Expression files (`-e`). Callers can split lines and call `Compile`.
- Referenced-object completion (the default second pass of the CLI).
- `--invert-match`. It is a `!` at the call site.
- Convenience slice-based matching. The streaming API is the only API.

## Osmium semantics (verified from source)

Verified against osmium-tool `src/util.cpp`, `src/command_tags_filter.cpp`,
and libosmium `tags/matcher.hpp` and `util/string_matcher.hpp` on 2026-10-04,
plus the `osmium-tags-filter` man page. The installed binary used for
differential testing is osmium-tool 1.19.0.

### Expression grammar

An expression is `[TYPES/]REST`.

- Find the first `/`. If there is none, types are `nwr` and the whole string
  is REST. If it is at position 0, types are `nwr` and REST follows it.
  Otherwise every byte before it must be one of `n w r a`; anything else is an
  error. Repeated letters are allowed. Later slashes are ordinary bytes.
- Find the first `=` in REST. If none, the rule is key-only: the value matcher
  is "any". Otherwise the key is everything before and the value everything
  after. If the raw key is non-empty and its last byte is `!`, remove it and
  mark the rule inverted. This check happens on the raw key, before trimming,
  so `highway !=primary` is inverted but `highway! =primary` is not.

### String matcher construction

Applied to the key and to the value independently.

1. Trim ASCII space (0x20) from both ends. Tabs and other whitespace are not
   trimmed.
2. If the result is exactly `*`: **any**.
3. If the result is empty, or neither its first nor last byte is `*`:
   - no comma: **equal** to the literal, which may be empty;
   - otherwise **list** of the comma-separated items, each trimmed of spaces,
     empty items kept. Items are literal, `*` inside a list is not special.
4. If the last byte is `*` and the first is not: **prefix** with the `*`
   removed.
5. Otherwise the first byte is `*`: remove it, then remove a trailing `*` if
   still present: **substring**. An empty substring pattern matches
   everything, as C `strstr` does, so `**` behaves like `*`.

### Tag and object matching

- A tag matches a rule when the key matcher accepts the key and the value
  matcher's result equals the rule's `want` flag. `want` is true for normal
  rules and false for inverted rules. Consequently `highway!=primary` matches
  objects that have a `highway` tag with another value, and does not match
  objects without a `highway` tag.
- An object matches when any of its tags matches any rule that applies to its
  type. Keys are unique within an object, so the order tags are tested in does
  not affect the result.
- Nodes use node rules. Ways use way rules, plus area rules when the way has
  4 or more nodes and its first and last node are the same. Relations use
  relation rules, plus area rules when the relation has a `type` tag whose
  value is `multipolygon` or `boundary`.
- Everything is case sensitive. There is no escaping.

## Public API

Package `osmtagfilter`. Standard library only.

Module path assumed to be `github.com/danielwhalen/osm-tag-filter`. There is
no git remote yet, so this must be confirmed before `go mod init`.

```go
// Types is a bitmask of rule groups.
type Types uint8

const (
    Nodes Types = 1 << iota
    Ways
    Relations
    Areas
)

// Kind is the kind of object being matched.
type Kind uint8

const (
    Node Kind = iota
    Way
    Relation
)

// Filter is a compiled set of expressions. Immutable and safe for
// concurrent use once Compile returns.
type Filter struct { /* unexported */ }

func Compile(exprs ...string) (*Filter, error)
func MustCompile(exprs ...string) *Filter

// Types returns the union of the groups any rule applies to, so a decoder
// can skip object kinds that cannot match.
func (f *Filter) Types() Types

// Matcher is per-object state. It is a value type and allocates nothing.
// The zero Matcher is not usable; obtain one from Filter.Matcher.
type Matcher struct { /* f *Filter, kind, hits, multipolygon */ }

func (f *Filter) Matcher() Matcher

// Begin resets the state for a new object of the given kind.
func (m *Matcher) Begin(kind Kind)

// Tag tests one key/value pair and returns the hits so far, identical to
// what Hits would return. Neither slice is retained.
func (m *Matcher) Tag(key, value []byte) Types

// Hits reports which rule groups have matched since Begin: the kind's own
// bit (Nodes, Ways, or Relations) for a core hit, and Areas for an area
// rule hit.
func (m *Matcher) Hits() Types

// Multipolygon reports whether, since Begin, a relation tag
// type=multipolygon or type=boundary was seen. Always false for other kinds.
func (m *Matcher) Multipolygon() bool

// IsAreaWay is osmium's rule for when a way counts as an area.
func IsAreaWay(nodeCount int, closed bool) bool

// ParseError is returned by Compile for an invalid expression.
type ParseError struct {
    Expr string // the expression as given
    Pos  int    // byte offset of the offending byte
    Msg  string
}
```

### How a caller combines results

The library records hits; the caller decides what counts as a match. The
osmium result for each kind is:

```go
h := m.Hits()
nodeMatches     := h&Nodes != 0
wayMatches      := h&Ways != 0 || (h&Areas != 0 && IsAreaWay(n, closed))
relationMatches := h&Relations != 0 || (h&Areas != 0 && m.Multipolygon())
```

Way geometry may be supplied at any time, before, after, or instead of the
tags, because it is a pure function the library never needs. A caller with a
different idea of "area" substitutes its own predicate for `IsAreaWay`.

### Early exit

Exit is never forced. Tag returns the hits bitmask so the caller writes its
own stopping rule, for example:

```go
if m.Tag(k, v)&Ways != 0 { break }           // stop on a core hit
if m.Tag(k, v)&(Ways|Areas) != 0 { break }   // way already known to be a ring
m.Tag(k, v)                                   // never stop, want complete Hits
```

If the caller keeps feeding tags, every group whose bit is still unset keeps
being evaluated, so Hits and Multipolygon stay complete. If the caller stops
early, only bits already set are meaningful.

## Compilation

Compile processes each expression with the steps in the semantics section,
in the same order osmium does, and produces:

```go
type matchKind uint8 // any, equal, prefix, substring, list

type strMatcher struct {
    kind matchKind
    pat  []byte   // equal, prefix, substring; slice into Filter.blob
    list [][]byte // list; each a slice into Filter.blob
}

type rule struct {
    types Types
    key   strMatcher
    value strMatcher // kind any for key-only rules
    want  bool       // false when inverted
}

type Filter struct {
    rules []rule
    blob  []byte      // all pattern bytes, copied once
    core  [3][]uint16 // rule indices per Kind
    area  []uint16    // rule indices with the Areas bit
    types Types       // union of rule.types
}
```

All pattern bytes are copied into `blob` so the Filter holds no reference to
the caller's strings. The index slices are built at Compile so Begin is a few
field assignments and Tag loops only over rules that apply.

### Errors

The only parse failure, matching osmium, is an unknown type letter. Compile
returns a `*ParseError` whose `Pos` is the offset of that byte and whose
message reads `unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')`.
`Error()` returns exactly `osmtagfilter: expression %q: %s` with the
expression and message substituted, for example
`osmtagfilter: expression "x/amenity": unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')`.

Compile with zero expressions succeeds and yields a filter that matches
nothing. Duplicate expressions are allowed.

## Matching

`Filter.Matcher()` returns a Matcher by value holding the Filter pointer. The
caller keeps it on the stack or in its own struct.

`Begin(kind)` stores the kind and zeroes `hits` and `multipolygon`.

`Tag(key, value)` evaluates three independent groups, each only while its
result is still unset:

1. **Core rules** for the kind: scan `core[kind]`; on the first hit set the
   kind's bit and stop scanning.
2. **Area rules**, for ways and relations only: scan `area`; on the first hit
   set `Areas`.
3. **Multipolygon flag**, for relations only: set when key is `type` and
   value is `multipolygon` or `boundary`.

It then returns `hits`.

Rule evaluation is `key.match(k) && (value.match(v) == want)`. String
matching is a switch on kind with no interfaces or closures: any returns
true; equal uses `bytes.Equal`; prefix uses `bytes.HasPrefix`; substring uses
`bytes.Contains`; list loops `bytes.Equal` over its items.

Nothing on this path allocates, converts to string, or retains the caller's
slices.

## Testing

1. **Parse tests.** Table from expression to compiled shape: type mask, key
   and value matcher kind and pattern, `want`. Covers every man page example
   plus `/note`, `n/`, `*`, `**`, `a*,b`, `*a,b*`, `highway !=primary`,
   `highway! =primary`, `=foo`, and the empty string. Error cases check
   `ParseError.Pos` and message.
2. **Semantic conformance table.** Each case: expressions, kind, tags, and
   expected hits and multipolygon flag. Includes every man page example,
   negation against a missing key, area rules against open and 3-node closed
   ways, relations whose `type` tag arrives after the area hit, and list
   items with spaces.
3. **Differential test against osmium.** When an `osmium` binary is on PATH,
   generate small OSM XML files, run `osmium tags-filter -R`, and compare the
   surviving object IDs with ours. Skip cleanly when the binary is absent.
4. **Reference oracle and fuzzing.** A direct transliteration of osmium's
   nested loops over a tag slice lives in the test file. A fuzz test feeds
   random expressions and tags to both the streaming matcher and the oracle
   and requires identical results. A second fuzz target asserts Compile never
   panics and fails only on bad type letters.
5. **Allocation and ordering properties.** `testing.AllocsPerRun` asserts zero
   allocations for `Matcher`, `Begin`, `Tag`, `Hits`, and `Multipolygon`.
   Property tests check that final hits are independent of tag order and that
   stopping early never changes bits already set.
6. **Benchmarks.** A dozen rules against a typical ten-tag object, reporting
   ns per tag and allocs per op.

## Repository layout

```
go.mod
osmtagfilter.go        // types, Compile, Filter, ParseError
parse.go               // expression and string matcher parsing
match.go               // Matcher, Tag, string matching
parse_test.go
match_test.go
osmium_test.go         // differential test, skipped without binary
fuzz_test.go
bench_test.go
docs/superpowers/specs/2026-10-04-osm-tag-filter-design.md
```

Files are split so each stays small enough to hold in context. Names are
indicative; the implementation plan may adjust them.
