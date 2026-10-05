package osmtf

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestKindValues(t *testing.T) {
	if Node != 0 || Way != 1 || Relation != 2 {
		t.Fatalf("Kind values = %d %d %d", Node, Way, Relation)
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{Node: "Node", Way: "Way", Relation: "Relation", 3: "Kind(3)"} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestParseErrorError(t *testing.T) {
	err := &ParseError{Expr: "x/amenity", Pos: 0, Msg: "unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')"}
	want := `osmtf: expression "x/amenity": unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')`
	if got := err.Error(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestCanMatch(t *testing.T) {
	cases := []struct {
		exprs               []string
		node, way, relation bool
	}{
		{nil, false, false, false},
		{[]string{"n/a"}, true, false, false},
		{[]string{"w/a"}, false, true, false},
		{[]string{"r/a"}, false, false, true},
		{[]string{"a/a"}, false, true, true},
		{[]string{"wa/a"}, false, true, true},
		{[]string{"a"}, true, true, true},
		{[]string{"/a"}, true, true, true},
		{[]string{"n/a", "r/b"}, true, false, true},
	}
	for _, c := range cases {
		f := MustCompile(c.exprs...)
		got := [3]bool{f.CanMatch(Node), f.CanMatch(Way), f.CanMatch(Relation)}
		if want := [3]bool{c.node, c.way, c.relation}; got != want {
			t.Errorf("CanMatch for %q = %v, want %v", c.exprs, got, want)
		}
	}
	defer func() {
		if r := recover(); r != "osmtf: invalid Kind" {
			t.Fatalf("recovered %v", r)
		}
	}()
	MustCompile("a").CanMatch(Kind(3))
}

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
	if f := MustCompile("n/amenity"); f == nil || f.types != nodes {
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
		types: nodes | ways | relations | areas,
	}
	if !reflect.DeepEqual(f.core, want.core) || !reflect.DeepEqual(f.area, want.area) || f.types != want.types {
		t.Fatalf("core=%v area=%v types=%d", f.core, f.area, f.types)
	}
	if d := MustCompile("n/a", "n/a"); !reflect.DeepEqual(d.core[Node], []uint32{0, 1}) {
		t.Fatalf("duplicates: core[Node]=%v", d.core[Node])
	}
	e := MustCompile()
	if e.types != 0 || len(e.rules) != 0 || len(e.area) != 0 || len(e.core[Node])+len(e.core[Way])+len(e.core[Relation]) != 0 {
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
