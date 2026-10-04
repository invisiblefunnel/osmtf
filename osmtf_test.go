package osmtf

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"unicode/utf8"
)

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
	want := `osmtf: expression "x/amenity": unknown object type 'x' (allowed are 'n', 'w', 'r', and 'a')`
	if got := err.Error(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestIsAreaWay(t *testing.T) {
	cases := []struct {
		n            int
		closed, want bool
	}{
		{4, true, true}, {5, true, true}, {100, true, true},
		{3, true, false}, {4, false, false}, {0, false, false}, {0, true, false}, {1, true, false},
	}
	for _, c := range cases {
		if got := IsAreaWay(c.n, c.closed); got != c.want {
			t.Errorf("IsAreaWay(%d, %v) = %v, want %v", c.n, c.closed, got, c.want)
		}
	}
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
