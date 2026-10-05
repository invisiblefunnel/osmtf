# osmtf

osmtf is a Go library that matches OpenStreetMap objects against tag filter
expressions, mirroring the expression language and matching rules of
`osmium tags-filter`. It was verified against osmium-tool 1.19.0 by a
differential test that runs the `osmium` binary on PATH. It is built for the
hot loop of a PBF or XML decoder: expressions are compiled once into a
`Filter`, each object's tags are streamed into a `Matcher` one key/value pair
at a time, as byte slices or strings, and `Matches` gives osmium's result.
Matching allocates nothing and never retains the caller's slices. A `Filter`
is immutable and safe to share across goroutines, each with its own `Matcher`.

## Install

```sh
go get github.com/invisiblefunnel/osmtf
```

## Usage

```go
f := osmtf.MustCompile("w/highway", "a/building")
m := f.Matcher()

// Start every way with its actual geometry, before feeding tags.
refs := []int64{1, 2, 3, 1}
n := len(refs)
closed := n > 0 && refs[0] == refs[n-1] // Also safe for an empty way.
m.BeginWay(n, closed)
m.Tag([]byte("building"), []byte("yes"))
fmt.Println(m.Matches()) // true

// A relation's first type tag determines whether it is an area.
m.BeginRelation()
m.TagString("building", "yes")
fmt.Println(m.Matches()) // false: the relation's type is not yet known
m.TagString("type", "multipolygon")
fmt.Println(m.Matches()) // true

m.BeginNode()
m.TagString("building", "yes")
fmt.Println(m.Matches()) // false: a/building does not apply to nodes
```

Call `BeginNode`, `BeginWay`, or `BeginRelation` before each object's tags.
Every way requires its real reference count and closedness, even for a
filter containing only `w/highway`. `Compile` accepts all osmium expressions,
including `a/` and combined prefixes such as `na/`; the decoder follows the
same path regardless of which expressions a user supplies. Geometry-free
way matching is outside this interface. Decoders that deliver tags before
references must buffer until they know the geometry.

Obtain matchers from `Filter.Matcher`; each start method panics with
`osmtf: zero Matcher; use Filter.Matcher` on a zero matcher. Each worker
needs its own matcher; do not copy one while matching an object.

### Stopping early

`Tag` and `TagString` return nothing. `Matches` can be called after any tag:
a true result stays true until the next object starts; a false result is
final only after every tag. A decoder may stop feeding tags once `Matches`
is true, but must still advance to the next object and check decoding errors.
In particular, a dense-node tag scanner may span many objects and need to
be drained even after a match. Early stopping is optional; it is not a
promise of faster decoding.

### Skipping object kinds

`Filter.CanMatch` tells a decoder which kinds of object it may skip, as
osmium does: skip nodes when `!f.CanMatch(osmtf.Node)`, and likewise for
`osmtf.Way` and `osmtf.Relation`. Area expressions enable ways and relations:
`osmtf.MustCompile("a/building")` can match both, but cannot match nodes.

### Expression files

`ReadExpressions` reads an expressions file as `osmium tags-filter -e` does,
one expression per line with `#` starting a comment, and returns the
expressions for `Compile`, which can take command-line expressions alongside:

```go
exprs, err := osmtf.ReadExpressions(file)
if err != nil {
	return err
}
f, err := osmtf.Compile(append(exprs, cliExprs...)...)
```

Each line is cut at its first `#`, even mid-expression, so `name=a#b` becomes
`name=a`. A line with nothing left is skipped; otherwise one trailing `\r` is
dropped and the rest is an expression, unchanged. So a line of only spaces, or
only `\r`, becomes an empty-key rule, as in osmium.

## Semantics

- Type prefixes `n/`, `w/`, `r/`, and `a/` restrict an expression to nodes,
  ways, relations, or areas, and combine, as in `nw/highway`. With no prefix,
  or a bare `/`, an expression applies to nodes, ways, and relations. Any
  other byte before the first `/` makes `Compile` fail with a `*ParseError`;
  nothing else is an error.
- `highway`, a key alone, matches a `highway` tag with any value.
- `highway=primary` matches that tag exactly.
- `highway!=primary` matches a `highway` tag with any other value. An object
  without a `highway` tag does not match.
- Commas list alternatives, for keys and values alike:
  `name,name:de=Kastanienallee,Kastanienstrasse`. Items are literal, so
  `x=a*,b` matches `a*` or `b`. A key or value that starts or ends with `*` is
  a wildcard pattern instead, never a list.
- `*` alone matches any key or value, so `highway=*` is the same as `highway`.
- `prefix*` matches anything that starts with `prefix`, as in `n/addr:*`.
- `*substring` matches anything that contains `substring`, not only what ends
  with it, as in `n/name=*Paris`. `*substring*` is the same.
- Area rules (`a/`) apply to closed ways with 4 or more nodes and to relations
  whose first `type` tag is `multipolygon` or `boundary`. `BeginWay` supplies
  geometry and `Matches` applies these rules. An empty way can match a `w/`
  rule but never an `a/` rule. Later relation `type` tags are ignored.
- Everything is case sensitive, and there is no escaping.
- Only ASCII spaces are trimmed, from both ends of each key, value, and list
  item. Tabs and other whitespace are kept.

## Not in scope

- Referenced-object completion, the CLI's default of also writing the nodes of
  matching ways and the members of matching relations. Each object is matched
  on its own, as with `osmium tags-filter -R`.
- `--invert-match`. Negate the result with `!` at the call site.
