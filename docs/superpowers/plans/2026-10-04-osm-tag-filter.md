# osm-tag-filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A standard-library Go package `osmtagfilter` that compiles `osmium tags-filter` expressions once and tests OSM objects tag-by-tag with zero allocations, with results identical to osmium-tool 1.19.0.

**Architecture:** `Compile` parses each expression into a `rule` (type mask, key matcher, value matcher, `want` flag) whose pattern bytes live in one shared `blob`, and builds per-kind index slices so `Tag` only scans applicable rules. `Matcher` is a small value type holding a `*Filter` plus per-object state (`kind`, `hits`, `multipolygon`); `Tag` evaluates three independent groups (core rules, area rules, multipolygon flag), each only while still unset, and returns the hits bitmask. Geometry is left to the caller via `IsAreaWay`.

**Tech Stack:** Go 1.22+ (standard library only, including tests: `testing`, `bytes`, `strings`, `os/exec`, `encoding/xml`, `math/rand`). osmium-tool 1.19.0 on PATH for the optional differential test.

**Spec:** `docs/superpowers/specs/2026-10-04-osm-tag-filter-design.md`

## Global Constraints

- Module path `github.com/invisiblefunnel/osm-tag-filter`; package name `osmtagfilter`; `go 1.22` directive in `go.mod` (the local toolchain is 1.26, which builds it fine).
- Standard library only. No `require` lines in `go.mod`, in tests either.
- The exported API is exactly the spec's: `Types` (`Nodes`, `Ways`, `Relations`, `Areas` = 1, 2, 4, 8), `Kind` (`Node`, `Way`, `Relation` = 0, 1, 2), `Filter`, `Compile`, `MustCompile`, `(*Filter).Types`, `(*Filter).Matcher`, `Matcher`, `(*Matcher).Begin`, `(*Matcher).Tag`, `(*Matcher).Hits`, `(*Matcher).Multipolygon`, `IsAreaWay`, `ParseError`. Every exported identifier has a doc comment.
- `Matcher`, `Begin`, `Tag`, `Hits`, and `Multipolygon` allocate nothing, never convert `[]byte` to `string`, and never retain the caller's slices.
- Only ASCII space (0x20) is trimmed, and only where the spec says. Everything is case sensitive. No escaping.
- The only parse failure is an unknown type letter. `ParseError.Msg` is `unknown object type '%c' (allowed are 'n', 'w', 'r', and 'a')` with the offending byte; `Error()` is `osmtagfilter: expression %q: %s`.
- A `Filter` is immutable after `Compile` and safe for concurrent use.
- Every task ends with `gofmt -l .` printing nothing, `go vet ./...` clean, `go test ./...` passing, then one commit for that task only (AGENTS.md: atomic commits, checks before committing).
- All test files except `example_test.go` are in package `osmtagfilter` (internal tests) so they can inspect compiled rules.

**Deviations from the spec, decided here:**
- Index slices are `[]uint32`, not `[]uint16`. `uint16` silently breaks past 65535 expressions; `uint32` costs nothing measurable. Task 3 pins this with a 70000-rule test.
- `Begin` panics with the message `osmtagfilter: invalid Kind` for a `Kind` above `Relation`, instead of indexing out of range later inside `Tag`.

## Review Focus

Inputs the spec implies but did not list, each pinned by a test in the owning task:

1. `Begin` with a `Kind` outside `Node`/`Way`/`Relation` (a decoder's own enum leaking through): a clear panic at `Begin`, not an index panic deep in `Tag`. Task 5, `TestBeginInvalidKindPanics`.
2. `Tag` called on a fresh `Filter.Matcher()` before any `Begin`: behaves as `Node` (the zero `Kind`) and never panics. Task 5, `TestTagBeforeBegin`.
3. nil or empty key and value slices (decoders hand out nil for absent values): treated as empty strings, so `=` and `k=` rules match them. Task 5, `TestNilKeyValue`.
4. Duplicate keys inside one object (the spec assumes uniqueness, real data breaks it): hits are still the union over all tags and `Multipolygon` stays true once seen. Task 5, `TestDuplicateKeys`.
5. One `Filter` shared by many goroutines, each with its own `Matcher`: no data race under `go test -race`. Task 6, `TestConcurrentMatchers`.

---

## File map

| File | Responsibility |
|---|---|
| `go.mod` | module path and Go floor |
| `osmtagfilter.go` | package doc, `Types`, `Kind`, `Filter`, `Compile`, `MustCompile`, `Types()`, `IsAreaWay`, `ParseError` |
| `parse.go` | `matchKind`, `strMatcher`, `rule`, `compiler` (blob interning), string matcher construction, expression parsing |
| `match.go` | `strMatcher.match`, `rule.match`, `Matcher` and its methods |
| `osmtagfilter_test.go` | tests for constants, `ParseError`, `IsAreaWay`, `Compile` indexes and errors |
| `parse_test.go` | string matcher and expression shape tables |
| `match_test.go` | string matching unit tests, conformance table, allocation/order/early-exit/concurrency properties |
| `example_test.go` | `ExampleFilter_Matcher` (external test package) showing the caller's combination formula |
| `fuzz_test.go` | reference oracle, oracle validation, two fuzz targets |
| `osmium_test.go` | differential test against the `osmium` binary, skipped when absent |
| `bench_test.go` | `BenchmarkTag` |
| `README.md` | usage |

---

### Task 1: Module, public enums, ParseError, IsAreaWay

**Files:**
- Create: `go.mod`, `osmtagfilter.go`, `osmtagfilter_test.go`

**Interfaces:**
- Produces:
  ```go
  type Types uint8
  const ( Nodes Types = 1 << iota; Ways; Relations; Areas )
  type Kind uint8
  const ( Node Kind = iota; Way; Relation )
  type ParseError struct { Expr string; Pos int; Msg string }
  func (e *ParseError) Error() string
  func IsAreaWay(nodeCount int, closed bool) bool
  ```

- [ ] **Step 1: Create the module**

```bash
go mod init github.com/invisiblefunnel/osm-tag-filter && go mod edit -go=1.22
```

- [ ] **Step 2: Write the failing tests in `osmtagfilter_test.go`**

```go
func TestTypesAndKindValues(t *testing.T) {
	if Nodes != 1 || Ways != 2 || Relations != 4 || Areas != 8 {
		t.Fatalf("Types bits = %d %d %d %d", Nodes, Ways, Relations, Areas)
	}
	if Node != 0 || Way != 1 || Relation != 2 {
		t.Fatalf("Kind values = %d %d %d", Node, Way, Relation)
	}
}

func TestParseErrorError(t *testing.T) {
	err := &ParseError{Expr: "x/amenity", Pos: 0, Msg: "unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')"}
	want := `osmtagfilter: expression "x/amenity": unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')`
	if got := err.Error(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestIsAreaWay(t *testing.T) {
	cases := []struct{ n int; closed, want bool }{
		{4, true, true}, {5, true, true}, {100, true, true},
		{3, true, false}, {4, false, false}, {0, false, false}, {0, true, false}, {1, true, false},
	}
	for _, c := range cases {
		if got := IsAreaWay(c.n, c.closed); got != c.want {
			t.Errorf("IsAreaWay(%d, %v) = %v, want %v", c.n, c.closed, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./...`
Expected: build failure, `undefined: Nodes` (or similar).

- [ ] **Step 4: Implement `osmtagfilter.go`**

Package doc comment (one paragraph: what it is, that it mirrors `osmium tags-filter`, and that `Matcher` is the streaming API). `Types`, `Kind` and their constants with the spec's doc comments. `ParseError` with `Error()` using `fmt.Sprintf("osmtagfilter: expression %q: %s", e.Expr, e.Msg)`. `IsAreaWay` returns `closed && nodeCount >= 4`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `ok  	github.com/invisiblefunnel/osm-tag-filter`, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add go.mod osmtagfilter.go osmtagfilter_test.go
git commit -m "Add module, public enums, ParseError, IsAreaWay"
```

---

### Task 2: String matcher construction

**Files:**
- Create: `parse.go`, `parse_test.go`

**Interfaces:**
- Produces:
  ```go
  type matchKind uint8
  const ( matchAny matchKind = iota; matchEqual; matchPrefix; matchSubstring; matchList )
  type strMatcher struct { kind matchKind; pat []byte; list [][]byte }
  type compiler struct { blob []byte }
  func (c *compiler) intern(s string) []byte
  func trimSpaces(s string) string
  func (c *compiler) stringMatcher(raw string) strMatcher
  ```
  `intern` appends `s` to `c.blob` and returns the appended region as a full slice expression (`c.blob[n:n+len(s):n+len(s)]`) so an `append` through the returned slice can never write into the blob. `Compile` (Task 3) pre-sizes `blob` to the total byte length of all expressions, so `intern` never reallocates: every pattern is a sub-slice of its expression, and list items together are shorter than their expression. `trimSpaces` is `strings.Trim(s, " ")`. `stringMatcher` applies the spec's five construction steps, interning `pat` and every list item.

- [ ] **Step 1: Write the failing tests in `parse_test.go`**

```go
type smShape struct {
	kind matchKind
	pat  string
	list []string
}

func shapeOfSM(m strMatcher) smShape {
	s := smShape{kind: m.kind, pat: string(m.pat)}
	for _, item := range m.list {
		s.list = append(s.list, string(item))
	}
	return s
}

func TestStringMatcherShape(t *testing.T) {
	cases := []struct {
		raw  string
		want smShape
	}{
		{"*", smShape{kind: matchAny}},
		{" * ", smShape{kind: matchAny}},
		{"", smShape{kind: matchEqual, pat: ""}},
		{"foo", smShape{kind: matchEqual, pat: "foo"}},
		{" foo ", smShape{kind: matchEqual, pat: "foo"}},
		{"\tfoo", smShape{kind: matchEqual, pat: "\tfoo"}},
		{"foo*", smShape{kind: matchPrefix, pat: "foo"}},
		{"*foo", smShape{kind: matchSubstring, pat: "foo"}},
		{"*foo*", smShape{kind: matchSubstring, pat: "foo"}},
		{"**", smShape{kind: matchSubstring, pat: ""}},
		{"***", smShape{kind: matchSubstring, pat: "*"}},
		{"a,b", smShape{kind: matchList, list: []string{"a", "b"}}},
		{"a , b ,", smShape{kind: matchList, list: []string{"a", "b", ""}}},
		{",", smShape{kind: matchList, list: []string{"", ""}}},
		{"a*,b", smShape{kind: matchList, list: []string{"a*", "b"}}},
		{"*a,b*", smShape{kind: matchSubstring, pat: "a,b"}},
		{"a,b*", smShape{kind: matchPrefix, pat: "a,b"}},
		{"*,", smShape{kind: matchSubstring, pat: ","}},
		{",*", smShape{kind: matchPrefix, pat: ","}},
	}
	for _, c := range cases {
		cmp := compiler{blob: make([]byte, 0, len(c.raw))}
		got := shapeOfSM(cmp.stringMatcher(c.raw))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("stringMatcher(%q) = %+v, want %+v", c.raw, got, c.want)
		}
	}
}

func TestInternSharesBlob(t *testing.T) {
	c := compiler{blob: make([]byte, 0, 4)}
	a, b := c.intern("ab"), c.intern("cd")
	if string(a) != "ab" || string(b) != "cd" || string(c.blob) != "abcd" {
		t.Fatalf("a=%q b=%q blob=%q", a, b, c.blob)
	}
	if &a[0] != &c.blob[0] || &b[0] != &c.blob[2] {
		t.Fatal("interned slices do not alias the blob")
	}
	if cap(a) != len(a) || cap(b) != len(b) {
		t.Fatal("interned slices must have cap == len")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestStringMatcherShape|TestInternSharesBlob' ./...`
Expected: build failure, `undefined: compiler`.

- [ ] **Step 3: Implement `parse.go`**

Types and functions from the Interfaces block. `stringMatcher` in spec order: trim; exactly `*` is `matchAny`; empty or neither end is `*`: no comma is `matchEqual`, else `matchList` of `strings.Split` items each trimmed (empty items kept); last byte `*` and first not: `matchPrefix` without the trailing `*`; otherwise drop the leading `*`, then a trailing `*` if one remains: `matchSubstring`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS, no gofmt output.

- [ ] **Step 5: Commit**

```bash
git add parse.go parse_test.go
git commit -m "Add string matcher construction"
```

---

### Task 3: Expression parsing and Compile

**Files:**
- Modify: `parse.go` (add `rule`, `parseExpr`), `osmtagfilter.go` (add `Filter`, `Compile`, `MustCompile`, `Types()`)
- Test: `parse_test.go`, `osmtagfilter_test.go`

**Interfaces:**
- Consumes: `compiler`, `stringMatcher`, `trimSpaces` (Task 2); `ParseError` (Task 1).
- Produces:
  ```go
  type rule struct { types Types; key, value strMatcher; want bool }
  func (c *compiler) parseExpr(expr string) (rule, error) // error is always *ParseError
  type Filter struct {
      rules []rule
      blob  []byte
      core  [3][]uint32 // indexed by Kind
      area  []uint32
      types Types
  }
  func Compile(exprs ...string) (*Filter, error)
  func MustCompile(exprs ...string) *Filter
  func (f *Filter) Types() Types
  ```
  `parseExpr`: `p := strings.IndexByte(expr, '/')`. `p < 0`: types `Nodes|Ways|Relations`, rest is the whole string. `p == 0`: same types, rest is `expr[1:]`. `p > 0`: every byte of `expr[:p]` must be `n`, `w`, `r`, or `a` (OR-ing the bit; repeats allowed); the first other byte at offset `i` returns `&ParseError{Expr: expr, Pos: i, Msg: fmt.Sprintf("unknown object type '%c' (allowed are 'n', 'w', 'r', and 'a')", expr[i])}`; rest is `expr[p+1:]`. Then `eq := strings.IndexByte(rest, '=')`: `eq < 0` is key-only (`value.kind == matchAny`, `want == true`); otherwise raw key is `rest[:eq]`, value is `rest[eq+1:]`, and if the raw key is non-empty and ends in `!`, strip it and set `want = false`. The `!` check happens on the raw key before trimming.
  `Compile`: sums expression lengths, makes `compiler{blob: make([]byte, 0, total)}`, parses each expression in order (returning the first error with `f == nil`), then builds `core[k]` with the index of every rule whose `types` has bit `1<<k`, `area` with every rule having `Areas`, and `types` as the union. `MustCompile` panics with the error value itself.

- [ ] **Step 1: Write the failing shape table in `parse_test.go`**

```go
type ruleShape struct {
	types      Types
	key, value smShape
	want       bool
}

func shapeOfRule(r rule) ruleShape {
	return ruleShape{r.types, shapeOfSM(r.key), shapeOfSM(r.value), r.want}
}

var (
	eq  = func(p string) smShape { return smShape{kind: matchEqual, pat: p} }
	pre = func(p string) smShape { return smShape{kind: matchPrefix, pat: p} }
	sub = func(p string) smShape { return smShape{kind: matchSubstring, pat: p} }
	lst = func(items ...string) smShape { return smShape{kind: matchList, list: items} }
	anyM = smShape{kind: matchAny}
	nwr = Nodes | Ways | Relations
)

var compileShapeCases = []struct {
	expr string
	want ruleShape
}{
	// man page examples
	{"n/amenity", ruleShape{Nodes, eq("amenity"), anyM, true}},
	{"nw/highway", ruleShape{Nodes | Ways, eq("highway"), anyM, true}},
	{"/note", ruleShape{nwr, eq("note"), anyM, true}},
	{"note", ruleShape{nwr, eq("note"), anyM, true}},
	{"w/highway=primary", ruleShape{Ways, eq("highway"), eq("primary"), true}},
	{"w/highway!=primary", ruleShape{Ways, eq("highway"), eq("primary"), false}},
	{"r/type=multipolygon,boundary", ruleShape{Relations, eq("type"), lst("multipolygon", "boundary"), true}},
	{"w/name,name:de=Kastanienallee,Kastanienstrasse", ruleShape{Ways, lst("name", "name:de"), lst("Kastanienallee", "Kastanienstrasse"), true}},
	{"n/addr:*", ruleShape{Nodes, pre("addr:"), anyM, true}},
	{"n/name=*Paris", ruleShape{Nodes, eq("name"), sub("Paris"), true}},
	{"a/building", ruleShape{Areas, eq("building"), anyM, true}},
	{"r/type=restriction", ruleShape{Relations, eq("type"), eq("restriction"), true}},
	// types
	{"nwra/x", ruleShape{Nodes | Ways | Relations | Areas, eq("x"), anyM, true}},
	{"nn/amenity", ruleShape{Nodes, eq("amenity"), anyM, true}},
	{"n/", ruleShape{Nodes, eq(""), anyM, true}},
	{"n/x/y=z", ruleShape{Nodes, eq("x/y"), eq("z"), true}},
	// wildcards and lists
	{"*", ruleShape{nwr, anyM, anyM, true}},
	{"**", ruleShape{nwr, sub(""), anyM, true}},
	{"a*,b", ruleShape{nwr, lst("a*", "b"), anyM, true}},
	{"*a,b*", ruleShape{nwr, sub("a,b"), anyM, true}},
	{"name=*", ruleShape{nwr, eq("name"), anyM, true}},
	{"name=a*", ruleShape{nwr, eq("name"), pre("a"), true}},
	{"name=*a*", ruleShape{nwr, eq("name"), sub("a"), true}},
	{"name=**", ruleShape{nwr, eq("name"), sub(""), true}},
	{"name=***", ruleShape{nwr, eq("name"), sub("*"), true}},
	{"name=a , b ,", ruleShape{nwr, eq("name"), lst("a", "b", ""), true}},
	// spaces, bang, empties
	{" highway = primary ", ruleShape{nwr, eq("highway"), eq("primary"), true}},
	{"highway=\tprimary", ruleShape{nwr, eq("highway"), eq("\tprimary"), true}},
	{"highway !=primary", ruleShape{nwr, eq("highway"), eq("primary"), false}},
	{"highway! =primary", ruleShape{nwr, eq("highway!"), eq("primary"), true}},
	{"highway!", ruleShape{nwr, eq("highway!"), anyM, true}},
	{"highway!=", ruleShape{nwr, eq("highway"), eq(""), false}},
	{"!=x", ruleShape{nwr, eq(""), eq("x"), false}},
	{"=foo", ruleShape{nwr, eq(""), eq("foo"), true}},
	{"", ruleShape{nwr, eq(""), anyM, true}},
}

func TestCompileShape(t *testing.T) {
	for _, c := range compileShapeCases {
		f, err := Compile(c.expr)
		if err != nil {
			t.Errorf("Compile(%q): %v", c.expr, err)
			continue
		}
		if got := shapeOfRule(f.rules[0]); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Compile(%q) = %+v, want %+v", c.expr, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Write the failing error and index tests in `osmtagfilter_test.go`**

```go
var compileErrorCases = []struct {
	expr string
	pos  int
	b    byte
}{
	{"x/amenity", 0, 'x'},
	{"nx/amenity", 1, 'x'},
	{" n/amenity", 0, ' '},
	{"N/amenity", 0, 'N'},
	{"nwr a/highway", 3, ' '},
	{"highway=primary/x", 0, 'h'},
	{"\xc3\xa9/highway", 0, 0xc3}, // "é/highway"
}

func TestCompileErrors(t *testing.T) {
	for _, c := range compileErrorCases {
		f, err := Compile("n/amenity", c.expr)
		if f != nil || err == nil {
			t.Errorf("Compile(%q) = %v, %v; want nil, error", c.expr, f, err)
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("Compile(%q) error %T is not *ParseError", c.expr, err)
			continue
		}
		wantMsg := fmt.Sprintf("unknown object type '%c' (allowed are 'n', 'w', 'r', and 'a')", c.b)
		if pe.Expr != c.expr || pe.Pos != c.pos || pe.Msg != wantMsg {
			t.Errorf("Compile(%q) = %+v; want Expr %q Pos %d Msg %q", c.expr, pe, c.expr, c.pos, wantMsg)
		}
		if !utf8.ValidString(err.Error()) {
			t.Errorf("Compile(%q) error text is not valid UTF-8: %q", c.expr, err.Error())
		}
	}
}

func TestMustCompile(t *testing.T) {
	if f := MustCompile("n/amenity"); f == nil || f.Types() != Nodes {
		t.Fatalf("MustCompile returned %v", f)
	}
	defer func() {
		if _, ok := recover().(*ParseError); !ok {
			t.Fatal("MustCompile did not panic with *ParseError")
		}
	}()
	MustCompile("x/a")
}

func TestCompileIndexes(t *testing.T) {
	f := MustCompile("n/a", "w/b", "r/c", "a/d", "nwra/e", "wa/f")
	want := Filter{
		core:  [3][]uint32{{0, 4}, {1, 4, 5}, {2, 4}},
		area:  []uint32{3, 4, 5},
		types: Nodes | Ways | Relations | Areas,
	}
	if !reflect.DeepEqual(f.core, want.core) || !reflect.DeepEqual(f.area, want.area) || f.Types() != want.types {
		t.Fatalf("core=%v area=%v types=%d", f.core, f.area, f.Types())
	}
	if d := MustCompile("n/a", "n/a"); !reflect.DeepEqual(d.core[Node], []uint32{0, 1}) {
		t.Fatalf("duplicates: core[Node]=%v", d.core[Node])
	}
	e := MustCompile()
	if e.Types() != 0 || len(e.rules) != 0 || len(e.area) != 0 || len(e.core[Node])+len(e.core[Way])+len(e.core[Relation]) != 0 {
		t.Fatalf("empty filter has rules: %+v", e)
	}
}

func TestCompileManyRules(t *testing.T) {
	exprs := make([]string, 70000)
	for i := range exprs {
		exprs[i] = fmt.Sprintf("n/k%d", i)
	}
	f := MustCompile(exprs...)
	if n := len(f.core[Node]); n != 70000 || f.core[Node][69999] != 69999 {
		t.Fatalf("core[Node] has %d entries, last %d", n, f.core[Node][len(f.core[Node])-1])
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./...`
Expected: build failure, `undefined: Compile`.

- [ ] **Step 4: Implement `rule` and `parseExpr` in `parse.go`, then `Filter`, `Compile`, `MustCompile`, `Types()` in `osmtagfilter.go`**

Per the Interfaces block. Doc comments on `Filter` ("Immutable and safe for concurrent use once Compile returns"), `Compile` (zero expressions yields a filter that matches nothing; duplicates allowed), `MustCompile`, and `Types` ("the union of the groups any rule applies to, so a decoder can skip object kinds that cannot match").

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS, no gofmt output.

- [ ] **Step 6: Commit**

```bash
git add parse.go parse_test.go osmtagfilter.go osmtagfilter_test.go
git commit -m "Add expression parsing and Compile"
```

---

### Task 4: String and rule matching

**Files:**
- Create: `match.go`, `match_test.go`

**Interfaces:**
- Consumes: `strMatcher`, `rule`, `compiler` (Tasks 2–3).
- Produces:
  ```go
  func (s *strMatcher) match(b []byte) bool
  func (r *rule) match(key, value []byte) bool // s.key.match(key) && s.value.match(value) == r.want
  ```
  `match` is a `switch s.kind`: `matchAny` true; `matchEqual` `bytes.Equal`; `matchPrefix` `bytes.HasPrefix`; `matchSubstring` `bytes.Contains` (empty pattern matches everything, like C `strstr`); `matchList` loops `bytes.Equal` over `s.list`. No interfaces, closures, or string conversions.

- [ ] **Step 1: Write the failing tests in `match_test.go`**

```go
func TestStrMatcherMatch(t *testing.T) {
	cases := []struct {
		pattern, input string
		want           bool
	}{
		{"*", "", true}, {"*", "x", true},
		{"", "", true}, {"", "x", false},
		{"abc", "abc", true}, {"abc", "abcd", false}, {"abc", "ab", false}, {"abc", "ABC", false},
		{"abc*", "abc", true}, {"abc*", "abcd", true}, {"abc*", "ab", false}, {"abc*", "xabc", false},
		{"*abc", "abc", true}, {"*abc", "xabcx", true}, {"*abc", "ab", false},
		{"*abc*", "xabcx", true}, {"*abc*", "ab", false},
		{"**", "", true}, {"**", "anything", true},
		{"***", "a*b", true}, {"***", "ab", false},
		{"a,b", "a", true}, {"a,b", "b", true}, {"a,b", "a,b", false}, {"a,b", "", false},
		{"a, ,b", "", true},
		{"a*,b", "a*", true}, {"a*,b", "a", false},
	}
	for _, c := range cases {
		cmp := compiler{blob: make([]byte, 0, len(c.pattern))}
		m := cmp.stringMatcher(c.pattern)
		if got := m.match([]byte(c.input)); got != c.want {
			t.Errorf("pattern %q input %q = %v, want %v", c.pattern, c.input, got, c.want)
		}
	}
}

func TestRuleMatch(t *testing.T) {
	cases := []struct {
		expr, key, value string
		want             bool
	}{
		{"highway!=primary", "highway", "secondary", true},
		{"highway!=primary", "highway", "primary", false},
		{"highway!=primary", "name", "primary", false},
		{"highway", "highway", "", true},
		{"highway", "highways", "", false},
		{"highway=primary,secondary", "highway", "secondary", true},
		{"addr:*=*", "addr:city", "x", true},
		{"x!=*", "x", "anything", false},
	}
	for _, c := range cases {
		cmp := compiler{blob: make([]byte, 0, len(c.expr))}
		r, err := cmp.parseExpr(c.expr)
		if err != nil {
			t.Fatalf("parseExpr(%q): %v", c.expr, err)
		}
		if got := r.match([]byte(c.key), []byte(c.value)); got != c.want {
			t.Errorf("rule %q on %q=%q = %v, want %v", c.expr, c.key, c.value, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestStrMatcherMatch|TestRuleMatch' ./...`
Expected: build failure, `m.match undefined`.

- [ ] **Step 3: Implement `match.go` with `strMatcher.match` and `rule.match`**

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add match.go match_test.go
git commit -m "Add string and rule matching"
```

---

### Task 5: Matcher streaming API and conformance table

**Files:**
- Modify: `match.go`, `match_test.go`

**Interfaces:**
- Consumes: `Filter` fields (Task 3), `rule.match` (Task 4).
- Produces:
  ```go
  type Matcher struct { f *Filter; kind Kind; hits Types; multipolygon bool }
  func (f *Filter) Matcher() Matcher
  func (m *Matcher) Begin(kind Kind)       // zeroes hits and multipolygon; panics "osmtagfilter: invalid Kind" if kind > Relation
  func (m *Matcher) Tag(key, value []byte) Types
  func (m *Matcher) Hits() Types
  func (m *Matcher) Multipolygon() bool
  ```
  Package-level `var keyType = []byte("type")`, `valMultipolygon = []byte("multipolygon")`, `valBoundary = []byte("boundary")` compared with `bytes.Equal`. The kind's own bit is `Types(1) << m.kind`. `Tag` runs three groups, each only while unset: core (`m.hits&kindBit == 0`: scan `f.core[m.kind]`, set `kindBit` on first `rule.match`, stop scanning); area (`m.kind != Node && m.hits&Areas == 0`: scan `f.area`, set `Areas` on first hit); multipolygon (`m.kind == Relation && !m.multipolygon`: set when key is `type` and value is `multipolygon` or `boundary`). Returns `m.hits`. Doc comments are the spec's.
  Test-side, shared with Tasks 6–8:
  ```go
  type tagCase struct { name string; exprs []string; kind Kind; tags [][2]string; hits Types; mp bool }
  var conformance []tagCase
  func kv(pairs ...string) [][2]string
  func runCase(t *testing.T, f *Filter, c tagCase)
  ```

- [ ] **Step 1: Write the failing conformance test in `match_test.go`**

```go
func kv(pairs ...string) [][2]string {
	out := make([][2]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, [2]string{pairs[i], pairs[i+1]})
	}
	return out
}

var conformance = []tagCase{
	// man page examples
	{"n/amenity node", []string{"n/amenity"}, Node, kv("amenity", "cafe"), Nodes, false},
	{"n/amenity way", []string{"n/amenity"}, Way, kv("amenity", "cafe"), 0, false},
	{"nw/highway node", []string{"nw/highway"}, Node, kv("highway", "primary"), Nodes, false},
	{"nw/highway way", []string{"nw/highway"}, Way, kv("highway", "primary"), Ways, false},
	{"nw/highway relation", []string{"nw/highway"}, Relation, kv("highway", "primary"), 0, false},
	{"/note relation", []string{"/note"}, Relation, kv("note", "x"), Relations, false},
	{"note way", []string{"note"}, Way, kv("note", "x"), Ways, false},
	{"w/highway=primary hit", []string{"w/highway=primary"}, Way, kv("highway", "primary"), Ways, false},
	{"w/highway=primary other value", []string{"w/highway=primary"}, Way, kv("highway", "secondary"), 0, false},
	{"w/highway=primary case", []string{"w/highway=primary"}, Way, kv("highway", "Primary"), 0, false},
	{"w/highway!=primary other value", []string{"w/highway!=primary"}, Way, kv("highway", "secondary"), Ways, false},
	{"w/highway!=primary same value", []string{"w/highway!=primary"}, Way, kv("highway", "primary"), 0, false},
	{"w/highway!=primary missing key", []string{"w/highway!=primary"}, Way, kv("name", "x"), 0, false},
	{"w/highway!=primary no tags", []string{"w/highway!=primary"}, Way, nil, 0, false},
	{"r/type list boundary", []string{"r/type=multipolygon,boundary"}, Relation, kv("type", "boundary"), Relations, true},
	{"r/type list route", []string{"r/type=multipolygon,boundary"}, Relation, kv("type", "route"), 0, false},
	{"key list name:de", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name:de", "Kastanienstrasse"), Ways, false},
	{"key list name", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name", "Kastanienallee"), Ways, false},
	{"key list wrong key", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name:en", "Kastanienallee"), 0, false},
	{"key list partial value", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name", "Kastanien"), 0, false},
	{"n/addr:* prefix", []string{"n/addr:*"}, Node, kv("addr:street", "Main"), Nodes, false},
	{"n/addr:* no colon", []string{"n/addr:*"}, Node, kv("addr", "Main"), 0, false},
	{"n/addr:* not at start", []string{"n/addr:*"}, Node, kv("xaddr:street", "Main"), 0, false},
	{"n/name=*Paris substring", []string{"n/name=*Paris"}, Node, kv("name", "Rue de Paris Nord"), Nodes, false},
	{"n/name=*Paris case", []string{"n/name=*Paris"}, Node, kv("name", "paris"), 0, false},
	{"a/building way", []string{"a/building"}, Way, kv("building", "yes"), Areas, false},
	{"a/building node", []string{"a/building"}, Node, kv("building", "yes"), 0, false},
	{"a/building multipolygon", []string{"a/building"}, Relation, kv("type", "multipolygon", "building", "yes"), Areas, true},
	{"a/building boundary", []string{"a/building"}, Relation, kv("type", "boundary", "building", "yes"), Areas, true},
	{"a/building route", []string{"a/building"}, Relation, kv("type", "route", "building", "yes"), Areas, false},
	{"a/building no type", []string{"a/building"}, Relation, kv("building", "yes"), Areas, false},
	{"a/building type after hit", []string{"a/building"}, Relation, kv("building", "yes", "type", "multipolygon"), Areas, true},
	{"r/type=restriction", []string{"r/type=restriction"}, Relation, kv("type", "restriction"), Relations, false},
	// groups
	{"core and area from two rules", []string{"w/highway", "a/building"}, Way, kv("highway", "x", "building", "y"), Ways | Areas, false},
	{"core and area from one rule way", []string{"wa/building"}, Way, kv("building", "yes"), Ways | Areas, false},
	{"core and area from one rule relation", []string{"wa/building"}, Relation, kv("building", "yes"), Areas, false},
	{"multipolygon flag without rules", []string{"n/x"}, Relation, kv("type", "boundary"), 0, true},
	{"multipolygon flag ignored on ways", []string{"w/x"}, Way, kv("type", "multipolygon"), 0, false},
	{"multipolygon flag ignored on nodes", []string{"n/x"}, Node, kv("type", "multipolygon"), 0, false},
	{"no expressions", nil, Node, kv("a", "b"), 0, false},
	{"second expression matches", []string{"n/a", "n/b"}, Node, kv("b", ""), Nodes, false},
	{"later tag matches", []string{"n/amenity=cafe"}, Node, kv("name", "x", "cuisine", "y", "amenity", "cafe"), Nodes, false},
	// wildcards and empties
	{"star any tag", []string{"*"}, Node, kv("foo", "bar"), Nodes, false},
	{"star no tags", []string{"*"}, Node, nil, 0, false},
	{"double star", []string{"**"}, Node, kv("foo", "bar"), Nodes, false},
	{"empty key tag", []string{"=empty"}, Node, kv("", "empty"), Nodes, false},
	{"empty expression matches empty key", []string{""}, Node, kv("", "x"), Nodes, false},
	{"empty expression normal key", []string{""}, Node, kv("k", "x"), 0, false},
	{"n/ empty key", []string{"n/"}, Node, kv("", "x"), Nodes, false},
	{"k= empty value", []string{"k="}, Node, kv("k", ""), Nodes, false},
	{"k= non-empty value", []string{"k="}, Node, kv("k", "x"), 0, false},
	{"k key-only empty value", []string{"k"}, Node, kv("k", ""), Nodes, false},
	{"highway=* any value", []string{"highway=*"}, Node, kv("highway", ""), Nodes, false},
	{"x!=* never", []string{"x!=*"}, Node, kv("x", "c"), 0, false},
	{"highway!= non-empty", []string{"highway!="}, Node, kv("highway", "primary"), Nodes, false},
	{"highway!= empty", []string{"highway!="}, Node, kv("highway", ""), 0, false},
	{"highway! key-only", []string{"highway!"}, Node, kv("highway!", "x"), Nodes, false},
	{"highway! key-only not highway", []string{"highway!"}, Node, kv("highway", "x"), 0, false},
	// lists, spaces, tabs, bang placement
	{"list with spaces", []string{"highway=primary , residential"}, Node, kv("highway", "residential"), Nodes, false},
	{"list literal star", []string{"x=a*,b"}, Node, kv("x", "a*"), Nodes, false},
	{"list literal star b", []string{"x=a*,b"}, Node, kv("x", "b"), Nodes, false},
	{"list literal star no prefix", []string{"x=a*,b"}, Node, kv("x", "a"), 0, false},
	{"substring with comma", []string{"x=*a,b*"}, Node, kv("x", "za,bz"), Nodes, false},
	{"substring with comma not list", []string{"x=*a,b*"}, Node, kv("x", "a"), 0, false},
	{"spaces trimmed", []string{" highway = primary "}, Node, kv("highway", "primary"), Nodes, false},
	{"tab not trimmed", []string{"highway=\tprimary"}, Node, kv("highway", "primary"), 0, false},
	{"tab kept literally", []string{"highway=\tprimary"}, Node, kv("highway", "\tprimary"), Nodes, false},
	{"inverted list other", []string{"x!=a,b"}, Node, kv("x", "c"), Nodes, false},
	{"inverted list member", []string{"x!=a,b"}, Node, kv("x", "a"), 0, false},
	{"inverted list missing key", []string{"x!=a,b"}, Node, kv("y", "c"), 0, false},
	{"space before bang inverted", []string{"highway !=primary"}, Node, kv("highway", "secondary"), Nodes, false},
	{"space after bang not inverted", []string{"highway! =primary"}, Node, kv("highway", "secondary"), 0, false},
	{"later slash literal", []string{"n/x/y=z"}, Node, kv("x/y", "z"), Nodes, false},
	{"triple star substring star", []string{"x=***"}, Node, kv("x", "a*b"), Nodes, false},
	{"triple star no star", []string{"x=***"}, Node, kv("x", "ab"), 0, false},
}

// runCase feeds every tag, checks that each Tag return equals Hits, then
// checks the final Hits and Multipolygon against the case.
func runCase(t *testing.T, f *Filter, c tagCase) {
	t.Helper()
	m := f.Matcher()
	m.Begin(c.kind)
	for _, tg := range c.tags {
		if r := m.Tag([]byte(tg[0]), []byte(tg[1])); r != m.Hits() {
			t.Errorf("%s: Tag returned %d but Hits is %d", c.name, r, m.Hits())
		}
	}
	if m.Hits() != c.hits || m.Multipolygon() != c.mp {
		t.Errorf("%s: hits=%d mp=%v, want hits=%d mp=%v", c.name, m.Hits(), m.Multipolygon(), c.hits, c.mp)
	}
}

func TestConformance(t *testing.T) {
	for _, c := range conformance {
		runCase(t, MustCompile(c.exprs...), c)
	}
}
```

- [ ] **Step 2: Write the failing state and edge tests in `match_test.go`**

```go
func TestBeginResets(t *testing.T) {
	m := MustCompile("w/highway", "a/building").Matcher()
	m.Begin(Way)
	m.Tag([]byte("highway"), []byte("x"))
	m.Tag([]byte("building"), []byte("y"))
	m.Begin(Way)
	if m.Hits() != 0 {
		t.Fatalf("hits after Begin = %d", m.Hits())
	}
	m.Begin(Relation)
	m.Tag([]byte("type"), []byte("boundary"))
	m.Begin(Relation)
	if m.Multipolygon() {
		t.Fatal("multipolygon survived Begin")
	}
}

func TestBeginInvalidKindPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != "osmtagfilter: invalid Kind" {
			t.Fatalf("recovered %v", r)
		}
	}()
	m := MustCompile("n/a").Matcher()
	m.Begin(Kind(3))
}

func TestTagBeforeBegin(t *testing.T) {
	m := MustCompile("n/a", "w/a").Matcher()
	if got := m.Tag([]byte("a"), nil); got != Nodes {
		t.Fatalf("Tag before Begin = %d, want Nodes", got)
	}
}

func TestNilKeyValue(t *testing.T) {
	m := MustCompile("=").Matcher()
	m.Begin(Node)
	if m.Tag(nil, nil) != Nodes {
		t.Fatal("nil key and value did not match the empty rule")
	}
	m = MustCompile("k").Matcher()
	m.Begin(Node)
	if m.Tag([]byte("k"), nil) != Nodes {
		t.Fatal("nil value did not match the key-only rule")
	}
	m.Begin(Node)
	if m.Tag([]byte{}, []byte{}) != 0 {
		t.Fatal("empty key matched a rule for key k")
	}
}

func TestDuplicateKeys(t *testing.T) {
	m := MustCompile("w/highway=primary").Matcher()
	m.Begin(Way)
	m.Tag([]byte("highway"), []byte("secondary"))
	m.Tag([]byte("highway"), []byte("primary"))
	if m.Hits() != Ways {
		t.Fatalf("hits = %d, want Ways", m.Hits())
	}
	m = MustCompile("n/x").Matcher()
	m.Begin(Relation)
	m.Tag([]byte("type"), []byte("multipolygon"))
	m.Tag([]byte("type"), []byte("route"))
	if !m.Multipolygon() {
		t.Fatal("multipolygon flag was cleared by a later type tag")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./...`
Expected: build failure, `f.Matcher undefined`.

- [ ] **Step 4: Implement `Matcher` and its methods in `match.go`**

Per the Interfaces block. The `tagCase` type goes in `match_test.go` next to `conformance`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add match.go match_test.go
git commit -m "Add Matcher streaming API with conformance table"
```

---

### Task 6: Allocation, ordering, early-exit, concurrency, example

**Files:**
- Modify: `match_test.go`
- Create: `example_test.go` (package `osmtagfilter_test`)

**Interfaces:**
- Consumes: `conformance`, `runCase`, `kv` (Task 5), full public API.

- [ ] **Step 1: Write the failing property tests in `match_test.go`**

```go
var sink Types

func TestZeroAllocs(t *testing.T) {
	f := MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "r/type=multipolygon,boundary",
		"w/name,name:de=Kastanienallee,Kastanienstrasse", "n/addr:*", "n/name=*Paris", "a/building")
	tags := [][2][]byte{
		{[]byte("highway"), []byte("residential")}, {[]byte("name"), []byte("Main Street")},
		{[]byte("surface"), []byte("asphalt")}, {[]byte("type"), []byte("multipolygon")},
		{[]byte("building"), []byte("yes")}, {[]byte("addr:street"), []byte("x")},
	}
	for _, kind := range []Kind{Node, Way, Relation} {
		allocs := testing.AllocsPerRun(1000, func() {
			m := f.Matcher()
			m.Begin(kind)
			for _, tg := range tags {
				sink |= m.Tag(tg[0], tg[1])
			}
			sink |= m.Hits()
			if m.Multipolygon() {
				sink |= Areas
			}
		})
		if allocs != 0 {
			t.Errorf("kind %d: %v allocs per run, want 0", kind, allocs)
		}
	}
}

func TestHitsIndependentOfTagOrder(t *testing.T) {
	for _, c := range conformance {
		if len(c.tags) < 2 {
			continue
		}
		f := MustCompile(c.exprs...)
		reversed := make([][2]string, len(c.tags))
		for i, tg := range c.tags {
			reversed[len(c.tags)-1-i] = tg
		}
		rotated := append(append([][2]string{}, c.tags[1:]...), c.tags[0])
		for _, order := range [][][2]string{reversed, rotated} {
			runCase(t, f, tagCase{c.name + " reordered", c.exprs, c.kind, order, c.hits, c.mp})
		}
	}
}

func TestEarlyStopPreservesBits(t *testing.T) {
	for _, c := range conformance {
		m := MustCompile(c.exprs...).Matcher()
		m.Begin(c.kind)
		var prev Types
		for _, tg := range c.tags {
			r := m.Tag([]byte(tg[0]), []byte(tg[1]))
			if prev&^r != 0 {
				t.Errorf("%s: bits %d cleared by a later tag", c.name, prev&^r)
			}
			prev = r
		}
		if prev&^c.hits != 0 {
			t.Errorf("%s: early bits %d not in final hits %d", c.name, prev, c.hits)
		}
	}
}

func TestConcurrentMatchers(t *testing.T) {
	f := MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "a/building", "r/type=multipolygon,boundary")
	cases := []tagCase{
		{"node", nil, Node, kv("amenity", "cafe"), Nodes, false},
		{"way", nil, Way, kv("highway", "secondary", "building", "yes"), Ways | Areas, false},
		{"relation", nil, Relation, kv("building", "yes", "type", "boundary"), Relations | Areas, true},
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				for _, c := range cases {
					runCase(t, f, c)
				}
			}
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Write `example_test.go`**

```go
package osmtagfilter_test

import (
	"fmt"

	osmtagfilter "github.com/invisiblefunnel/osm-tag-filter"
)

func ExampleFilter_Matcher() {
	f := osmtagfilter.MustCompile("w/highway", "a/building")
	m := f.Matcher()

	// A way tagged building=yes, fed one tag at a time by a decoder.
	m.Begin(osmtagfilter.Way)
	m.Tag([]byte("building"), []byte("yes"))
	h := m.Hits()

	// The caller combines hits with geometry it already knows.
	closedWith5Nodes := osmtagfilter.IsAreaWay(5, true)
	openWith5Nodes := osmtagfilter.IsAreaWay(5, false)
	fmt.Println(h&osmtagfilter.Ways != 0 || (h&osmtagfilter.Areas != 0 && closedWith5Nodes))
	fmt.Println(h&osmtagfilter.Ways != 0 || (h&osmtagfilter.Areas != 0 && openWith5Nodes))
	// Output:
	// true
	// false
}
```

- [ ] **Step 3: Run the tests**

Run: `gofmt -l . && go vet ./... && go test -race ./...`
Expected: PASS including `ExampleFilter_Matcher`, zero allocations reported, no race. If `TestZeroAllocs` fails, the cause is in `Tag` or `Matcher()` (a string conversion, an escaping `Matcher`, or a closure); fix the library, never the test.

- [ ] **Step 4: Commit**

```bash
git add match_test.go example_test.go
git commit -m "Add allocation, ordering, concurrency tests and example"
```

---

### Task 7: Reference oracle and fuzz tests

**Files:**
- Create: `fuzz_test.go`

**Interfaces:**
- Consumes: `conformance`, `compileShapeCases`, `compileErrorCases`, `shapeOfRule` (Tasks 3, 5), public API.
- Produces (test-only; the oracle is written from the spec's semantics section with the `strings` package on `string` values and shares no parsing or matching code with `parse.go` or `match.go`; it may reuse the `matchKind` constants as labels):
  ```go
  type oracleMatcher struct { kind matchKind; pat string; list []string }
  func oracleStringMatcher(s string) oracleMatcher
  func (o oracleMatcher) match(s string) bool
  type oracleRule struct { types Types; key, value oracleMatcher; want bool }
  func oracleParse(expr string) (oracleRule, int)   // int is the offset of the bad type byte, -1 when ok
  func oracleMatch(rules []oracleRule, kind Kind, tags [][2]string) (hits Types, mp bool)
  ```
  `oracleMatch` is osmium's loop shape: for each tag, for each rule whose `types` has the kind's bit, if `key.match && value.match == want` set the kind bit; if kind is not `Node`, same over rules with `Areas` setting `Areas`; if kind is `Relation` and the tag is `type=multipolygon` or `type=boundary`, set `mp`.

- [ ] **Step 1: Write the oracle validation tests**

```go
func oracleShape(r oracleRule) ruleShape {
	conv := func(o oracleMatcher) smShape { return smShape{kind: o.kind, pat: o.pat, list: o.list} }
	return ruleShape{r.types, conv(r.key), conv(r.value), r.want}
}

func TestOracleParse(t *testing.T) {
	for _, c := range compileShapeCases {
		r, bad := oracleParse(c.expr)
		if bad != -1 {
			t.Errorf("oracleParse(%q) reported error at %d", c.expr, bad)
		} else if got := oracleShape(r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("oracleParse(%q) = %+v, want %+v", c.expr, got, c.want)
		}
	}
	for _, c := range compileErrorCases {
		if _, bad := oracleParse(c.expr); bad != c.pos {
			t.Errorf("oracleParse(%q) error at %d, want %d", c.expr, bad, c.pos)
		}
	}
}

func TestOracleConformance(t *testing.T) {
	for _, c := range conformance {
		var rules []oracleRule
		for _, e := range c.exprs {
			r, _ := oracleParse(e)
			rules = append(rules, r)
		}
		if hits, mp := oracleMatch(rules, c.kind, c.tags); hits != c.hits || mp != c.mp {
			t.Errorf("%s: oracle hits=%d mp=%v, want hits=%d mp=%v", c.name, hits, mp, c.hits, c.mp)
		}
	}
}
```

- [ ] **Step 2: Write the two fuzz targets**

```go
func FuzzMatchAgainstOracle(f *testing.F) {
	for _, c := range conformance {
		var e [2]string
		copy(e[:], c.exprs)
		var tv [6]string
		for i, t := range c.tags {
			if i == 3 {
				break
			}
			tv[2*i], tv[2*i+1] = t[0], t[1]
		}
		f.Add(e[0], e[1], uint8(c.kind), tv[0], tv[1], tv[2], tv[3], tv[4], tv[5])
	}
	f.Fuzz(func(t *testing.T, e1, e2 string, kind uint8, k1, v1, k2, v2, k3, v3 string) {
		k := Kind(kind % 3)
		// Keys are unique within an object: keep the first of any duplicate key.
		var tags [][2]string
		seen := map[string]bool{}
		for _, tg := range [][2]string{{k1, v1}, {k2, v2}, {k3, v3}} {
			if !seen[tg[0]] {
				seen[tg[0]] = true
				tags = append(tags, tg)
			}
		}
		exprs := []string{e1, e2}
		var rules []oracleRule
		oracleBad := -1
		for _, e := range exprs {
			r, bad := oracleParse(e)
			if bad != -1 {
				oracleBad = bad
				break
			}
			rules = append(rules, r)
		}
		fl, err := Compile(exprs...)
		if (err != nil) != (oracleBad != -1) {
			t.Fatalf("Compile(%q) err=%v, oracle bad=%d", exprs, err, oracleBad)
		}
		if err != nil {
			if err.(*ParseError).Pos != oracleBad {
				t.Fatalf("Compile(%q) Pos=%d, oracle %d", exprs, err.(*ParseError).Pos, oracleBad)
			}
			return
		}
		m := fl.Matcher()
		m.Begin(k)
		for _, tg := range tags {
			m.Tag([]byte(tg[0]), []byte(tg[1]))
		}
		wantHits, wantMP := oracleMatch(rules, k, tags)
		if m.Hits() != wantHits || m.Multipolygon() != wantMP {
			t.Fatalf("exprs %q kind %d tags %q: hits=%d mp=%v, oracle hits=%d mp=%v",
				exprs, k, tags, m.Hits(), m.Multipolygon(), wantHits, wantMP)
		}
	})
}

func FuzzCompile(f *testing.F) {
	for _, c := range compileShapeCases {
		f.Add(c.expr)
	}
	for _, c := range compileErrorCases {
		f.Add(c.expr)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		wantPos := -1
		if p := strings.IndexByte(expr, '/'); p > 0 {
			for i := 0; i < p; i++ {
				if !strings.ContainsRune("nwra", rune(expr[i])) {
					wantPos = i
					break
				}
			}
		}
		fl, err := Compile(expr)
		if wantPos == -1 {
			if err != nil || fl == nil || len(fl.rules) != 1 {
				t.Fatalf("Compile(%q) = %v, %v", expr, fl, err)
			}
			return
		}
		pe, ok := err.(*ParseError)
		if !ok || fl != nil || pe.Expr != expr || pe.Pos != wantPos {
			t.Fatalf("Compile(%q) = %v, %v; want ParseError at %d", expr, fl, err, wantPos)
		}
	})
}
```

- [ ] **Step 3: Run the oracle tests to verify they fail**

Run: `go test -run 'TestOracle' ./...`
Expected: build failure, `undefined: oracleParse`.

- [ ] **Step 4: Implement the oracle in `fuzz_test.go`**

Per the Interfaces block. `oracleParse` mirrors the grammar with `strings.IndexByte`, `strings.Trim(s, " ")`, `strings.Split`; `oracleMatcher.match` uses `==`, `strings.HasPrefix`, `strings.Contains`, and a loop for lists.

- [ ] **Step 5: Run the seeds, then fuzz each target**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: PASS (seed corpus only).

Run: `go test -run '^$' -fuzz=FuzzMatchAgainstOracle -fuzztime=30s ./...`
Expected: ends with `ok` and no `testdata/fuzz` failure directory created.

Run: `go test -run '^$' -fuzz=FuzzCompile -fuzztime=20s ./...`
Expected: same. A crasher means a real divergence: fix the library (or the oracle if the spec says the library is right), keep the generated `testdata/fuzz/...` file, and re-run.

- [ ] **Step 6: Commit**

```bash
git add fuzz_test.go testdata 2>/dev/null; git add fuzz_test.go
git commit -m "Add reference oracle and fuzz tests"
```

---

### Task 8: Differential test against osmium

**Files:**
- Create: `osmium_test.go`

**Interfaces:**
- Consumes: public API, `IsAreaWay`.
- Produces (test-only):
  ```go
  type osmObject struct { kind Kind; id int; tags [][2]string; nodeRefs []int } // nodeRefs for ways only
  func writeOSMXML(w io.Writer, objs []osmObject) error
  func runOsmium(osmium, file string, exprs []string) (map[string]bool, error) // keys "n1", "w10", "r20"
  func matchLocally(f *Filter, objs []osmObject) map[string]bool
  func diffCorpus() []osmObject
  func diffExpressionSets() [][]string
  ```
  `writeOSMXML` emits `<osm version="0.6" generator="osm-tag-filter-test">`, nodes with `lat="0" lon="0"`, ways with one `<nd ref=.../>` per `nodeRefs`, relations with one fixed `<member type="way" ref="1" role="outer"/>`, every object with `version="1"`, tags as `<tag k=.. v=../>` escaped with `xml.EscapeText`, in node, way, relation order.
  `runOsmium` runs `osmium tags-filter -R -f opl <file> <exprs...>`; on a non-zero exit it returns an error whose text includes stderr, otherwise the set of first space-delimited tokens of every stdout line.
  `matchLocally` applies the spec's combination formula: nodes `h&Nodes != 0`; ways `h&Ways != 0 || (h&Areas != 0 && IsAreaWay(len(nodeRefs), nodeRefs[0] == nodeRefs[len-1]))`; relations `h&Relations != 0 || (h&Areas != 0 && m.Multipolygon())`.
  `diffCorpus`: the fixed objects below plus 150 objects from `rand.New(rand.NewSource(1))` with 0–4 tags each (keys unique per object, drawn from the key vocabulary, values from the value vocabulary), ways with 1–6 node refs and a 50% chance of being closed (first ref repeated last), relations with a 50% chance of a `type` tag from `multipolygon`, `boundary`, `route`. Fixed objects use the IDs below; generated objects take IDs from 100 upward per kind. No tabs, newlines, or control characters in any key or value: XML attribute normalization would turn them into spaces and silently change the test.
  `diffExpressionSets`: the fixed sets below plus 80 sets from the same seeded generator, each 1–3 expressions built as `prefix + key + op + value` from the vocabularies below. No expression starts with `-` (osmium would read it as an option). Some generated sets do not compile (`x/y=z` has an unknown type letter `x`); the test keeps them and requires osmium to reject them too.

  Key vocabulary: `amenity`, `highway`, `building`, `name`, `name:de`, `addr:street`, `addr:housenumber`, `type`, `note`, `x`, `k`, `highway!`, `x/y`, `""` (empty).
  Value vocabulary: `cafe`, `primary`, `residential`, `yes`, `Kastanienallee`, `Kastanienstrasse`, `Rue de Paris Nord`, `Paris`, `multipolygon`, `boundary`, `route`, `restriction`, `a*`, `b`, `za,bz`, `a`, `a*b`, `Straße`, ` spaced `, `""` (empty).
  Expression prefixes: `""`, `/`, `n/`, `w/`, `r/`, `a/`, `nw/`, `wa/`, `ra/`, `nwr/`, `nwra/`, `rr/`.
  Expression keys: `amenity`, `highway`, `building`, `name`, `name,name:de`, `addr:*`, `*name`, `*`, `**`, `type`, `x`, `note`, `k`, `""`, `highway!`, `x/y`, ` highway `.
  Expression ops: `""` (key-only), `=`, `!=`.
  Expression values: `cafe`, `primary`, `primary,residential`, `primary , residential`, `yes`, `*Paris`, `Kastanien*`, `*strasse`, `*`, `**`, `***`, `a*,b`, `*a,b*`, `""`, ` primary `, `multipolygon,boundary`, `a`, `Straße`.

  Fixed objects (id: kind, tags, way node refs):
  ```
  n1  amenity=cafe                                   n2  highway=primary
  n3  highway=residential                            n4  (no tags)
  n5  name=Rue de Paris Nord                         n6  name:de=Kastanienstrasse
  n7  addr:street=Main                               n8  ""=empty
  n9  k=""                                           n10 x=a*
  n11 x=b          n12 x=za,bz          n13 x=a      n14 key "highway!" value "primary"
  n15 x/y=z        n16 x=a*b            n17 name=Straße
  w10 building=yes refs 1,2,3,1    w11 building=yes refs 1,2,1    w12 building=yes refs 1,2,3,4
  w13 highway=primary refs 1,2,3,1 w14 highway=primary,building=yes refs 1,2,3,4,1
  w15 (no tags) refs 1,2,3,1
  r20 type=multipolygon,building=yes   r21 type=boundary,building=yes
  r22 type=route,building=yes          r23 building=yes
  r24 type=restriction                 r25 type=multipolygon (only)
  ```
  Fixed expression sets: each man page example alone (`n/amenity`, `nw/highway`, `/note`, `note`, `w/highway=primary`, `w/highway!=primary`, `r/type=multipolygon,boundary`, `w/name,name:de=Kastanienallee,Kastanienstrasse`, `n/addr:*`, `n/name=*Paris`, `a/building`, `r/type=restriction`), then `*`, `**`, `=empty`, `k=`, `highway!`, `highway !=primary`, `highway! =primary`, `x=a*,b`, `x=*a,b*`, `x=***`, `x!=*`, `n/x/y=z`, `""`, `wa/building`, `ra/building`, and the pair `nw/highway` + `r/type=restriction`.

- [ ] **Step 1: Write `osmium_test.go` with the test**

```go
func TestOsmiumDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in -short mode")
	}
	osmium, err := exec.LookPath("osmium")
	if err != nil {
		t.Skip("osmium not on PATH")
	}
	objs := diffCorpus()
	file := filepath.Join(t.TempDir(), "corpus.osm")
	var buf bytes.Buffer
	if err := writeOSMXML(&buf, objs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sanity: "*" must return every tagged object, proving osmium read the file.
	tagged := 0
	for _, o := range objs {
		if len(o.tags) > 0 {
			tagged++
		}
	}
	if got, err := runOsmium(osmium, file, []string{"*"}); err != nil || len(got) != tagged {
		t.Fatalf(`osmium "*" returned %d objects (err %v), corpus has %d tagged`, len(got), err, tagged)
	}

	for _, exprs := range diffExpressionSets() {
		f, err := Compile(exprs...)
		want, osmErr := runOsmium(osmium, file, exprs)
		if err != nil {
			if osmErr == nil || !strings.Contains(osmErr.Error(), "Unknown object type") {
				t.Errorf("exprs %q: we rejected (%v) but osmium said %v", exprs, err, osmErr)
			}
			continue
		}
		if osmErr != nil {
			t.Fatalf("exprs %q: osmium failed: %v", exprs, osmErr)
		}
		got := matchLocally(f, objs)
		if !maps.Equal(got, want) {
			for id := range want {
				if !got[id] {
					t.Errorf("exprs %q: osmium matched %s, we did not", exprs, id)
				}
			}
			for id := range got {
				if !want[id] {
					t.Errorf("exprs %q: we matched %s, osmium did not", exprs, id)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestOsmiumDifferential ./...`
Expected: build failure, `undefined: diffCorpus`.

- [ ] **Step 3: Implement the helpers in `osmium_test.go`**

Per the Interfaces block.

- [ ] **Step 4: Run the differential test**

Run: `gofmt -l . && go vet ./... && go test -run TestOsmiumDifferential -v ./...`
Expected: `--- PASS: TestOsmiumDifferential` in under 10 seconds. Then `go test -short ./...` must print `ok` and skip it. A mismatch is a library bug unless the probe with the binary shows otherwise; fix the library.

- [ ] **Step 5: Commit**

```bash
git add osmium_test.go
git commit -m "Add differential test against osmium tags-filter"
```

---

### Task 9: Benchmarks

**Files:**
- Create: `bench_test.go`

**Interfaces:**
- Consumes: public API.

- [ ] **Step 1: Write `BenchmarkTag`**

Twelve rules: `n/amenity`, `nw/highway`, `w/highway!=primary`, `r/type=multipolygon,boundary`, `w/name,name:de=Kastanienallee,Kastanienstrasse`, `n/addr:*`, `n/name=*Paris`, `a/building`, `r/type=restriction`, `nwr/note`, `w/railway=rail,light_rail`, `wa/natural=water`.

Two sub-benchmarks, each a `Way` with ten tags fed in full every iteration (no early exit), `b.ReportAllocs()`, and `b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/10, "ns/tag")` after the loop:
- `hit`: tags `surface=asphalt`, `lanes=2`, `maxspeed=30`, `oneway=no`, `lit=yes`, `sidewalk=both`, `highway=residential`, `name=Main Street`, `width=6`, `smoothness=good` (the first core hit lands on tag 7).
- `miss`: tags `surface=asphalt`, `lanes=2`, `maxspeed=30`, `oneway=no`, `lit=yes`, `sidewalk=both`, `width=6`, `smoothness=good`, `natural=grass`, `source=survey` (every tag scans every way and area rule).

Byte slices are built before `b.ResetTimer()`; the loop ORs `Tag` results into the package-level `sink`.

- [ ] **Step 2: Run the benchmarks**

Run: `gofmt -l . && go vet ./... && go test -run '^$' -bench . -benchmem ./...`
Expected: both lines end in `0 allocs/op` and report `ns/tag`.

- [ ] **Step 3: Commit**

```bash
git add bench_test.go
git commit -m "Add Tag benchmarks"
```

---

### Task 10: README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write the README**

Sections, in order: one-paragraph description (mirrors `osmium tags-filter` expressions and matching, verified against osmium-tool 1.19.0; streaming, zero-allocation, safe to share); `go get github.com/invisiblefunnel/osm-tag-filter`; a usage block that is the `ExampleFilter_Matcher` body plus the three-line combination formula from the spec's "How a caller combines results"; a short "Semantics" list (type prefixes, key-only, `=`, `!=`, comma lists, `*`, `prefix*`, `*substring`, area rule for closed ways with 4+ nodes and `type=multipolygon`/`boundary` relations, case sensitive, only spaces trimmed); a "Not in scope" list (expression files, referenced-object completion, `--invert-match`); a link to `docs/superpowers/specs/2026-10-04-osm-tag-filter-design.md`.

- [ ] **Step 2: Verify the whole module one last time**

Run: `gofmt -l . && go vet ./... && go test -race ./... && go test -short ./...`
Expected: all `ok`, no gofmt output.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "Add README"
```
