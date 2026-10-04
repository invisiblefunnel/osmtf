package osmtf

import "testing"

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
