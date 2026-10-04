# osmtf

osmtf is a Go library that matches OpenStreetMap objects against tag filter
expressions, mirroring the expression language and matching rules of
`osmium tags-filter`. A differential test that runs the `osmium` binary
verifies it against osmium-tool 1.19.0. It is built for the hot loop of a PBF
or XML decoder: expressions are compiled once into a `Filter`, and each
object's tags are streamed into a `Matcher` one key/value pair at a time, as
byte slices, with `Tag` returning the hits so far, so the caller can stop
early. Matching allocates nothing and never retains the caller's slices. A
`Filter` is immutable and safe to share across goroutines, each with its own
`Matcher`.

## Install

```sh
go get github.com/invisiblefunnel/osmtf
```

## Usage

```go
f := osmtf.MustCompile("w/highway", "a/building")
m := f.Matcher()

// A way tagged building=yes, fed one tag at a time by a decoder.
m.Begin(osmtf.Way)
m.Tag([]byte("building"), []byte("yes"))
h := m.Hits()

// The caller combines hits with geometry it already knows.
closedWith5Nodes := osmtf.IsAreaWay(5, true)
openWith5Nodes := osmtf.IsAreaWay(5, false)
fmt.Println(h&osmtf.Ways != 0 || (h&osmtf.Areas != 0 && closedWith5Nodes)) // true
fmt.Println(h&osmtf.Ways != 0 || (h&osmtf.Areas != 0 && openWith5Nodes))   // false

// In general, osmium's result for each kind of object is as follows, where n
// is a way's node count and closed says whether its first and last node are
// the same.
nodeMatches := h&osmtf.Nodes != 0
wayMatches := h&osmtf.Ways != 0 || (h&osmtf.Areas != 0 && osmtf.IsAreaWay(n, closed))
relationMatches := h&osmtf.Relations != 0 || (h&osmtf.Areas != 0 && m.Multipolygon())
```

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
  tagged `type=multipolygon` or `type=boundary`. `Hits` reports an area rule
  hit as `Areas`, and the caller checks the way with `IsAreaWay` or the
  relation with `Multipolygon`, as in the formula above.
- Everything is case sensitive, and there is no escaping.
- Only ASCII spaces are trimmed, from both ends of each key, value, and list
  item. Tabs and other whitespace are kept.

## Not in scope

- Expression files (`osmium tags-filter -e`). Read the file yourself and pass
  its expressions to `Compile`.
- Referenced-object completion, the CLI's default of also writing the nodes of
  matching ways and the members of matching relations. Each object is matched
  on its own, as with `osmium tags-filter -R`.
- `--invert-match`. Negate the result with `!` at the call site.

## Design

The design, including the osmium semantics as verified from source, is in
[docs/superpowers/specs/2026-10-04-osm-tag-filter-design.md](docs/superpowers/specs/2026-10-04-osm-tag-filter-design.md).
