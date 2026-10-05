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

Select footways, paths, pedestrian streets, and steps with a way-only
expression and a comma-separated list of highway values. Compile once and
reuse a single matcher:

```go
f := osmtf.MustCompile("w/highway=footway,path,pedestrian,steps")
m := f.Matcher()

// Start every way with its actual geometry, before feeding tags.
refs := []int64{101, 102, 103}
n := len(refs)
closed := n > 0 && refs[0] == refs[n-1] // Also safe for an empty way.
m.BeginWay(n, closed)
for _, tag := range [][2]string{
	{"highway", "footway"},
	{"name", "Riverside Walk"},
	{"surface", "gravel"},
} {
	m.TagString(tag[0], tag[1])
}
fmt.Println(m.Matches()) // true
```

`w/` limits the expression to ways; the values after `=` are alternatives.
`TagString` consumes strings without copying; use `Tag` for byte slices.

This selects highway classes without evaluating access restrictions.
[OSMnx's full `walk` filter](https://github.com/gboeing/osmnx/blob/74e68ce2200b23c04f6ec2a864a6c24859bbf08d/osmnx/_overpass.py#L96-L108)
also combines access, area, and sidewalk exclusions across tags. Osmium
expressions combine with OR, so one filter cannot express that full predicate.

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

`Tag` and `TagString` return nothing. A true `Matches` result stays true until
the next object starts; a false result is final only after every tag. Once
the matcher returns true, it needs no further tags.

Always advance the decoder to the next object and check decoding errors,
even after a match. A dense-node tag scanner may span many objects and need
to be drained. Early stopping is optional; it is not a promise of faster
decoding.

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

- Multiple expressions are combined with OR: an object matches if any tag
  matches any expression.
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
