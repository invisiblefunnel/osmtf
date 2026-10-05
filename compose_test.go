package osmtf

import "testing"

func compositeLeaf(exprs ...string) *Matcher {
	m := MustCompile(exprs...).Matcher()
	return &m
}

func beginComposition(m ObjectMatcher, kind Kind, nodeCount int, closed bool) {
	switch kind {
	case Node:
		m.BeginNode()
	case Way:
		m.BeginWay(nodeCount, closed)
	case Relation:
		m.BeginRelation()
	default:
		panic("invalid test kind")
	}
}

func TestCombinators(t *testing.T) {
	cases := []struct {
		name    string
		compose func(ObjectMatcher, ObjectMatcher) ObjectMatcher
		// Results for no tags, x only, y only, and both tags.
		want [4]bool
	}{
		{"all", func(x, y ObjectMatcher) ObjectMatcher { return All(x, y) }, [4]bool{false, false, false, true}},
		{"any", func(x, y ObjectMatcher) ObjectMatcher { return Any(x, y) }, [4]bool{false, true, true, true}},
		{"not", func(x, y ObjectMatcher) ObjectMatcher { return Not(x) }, [4]bool{true, false, true, false}},
		{"all not any", func(x, y ObjectMatcher) ObjectMatcher { return All(x, Not(Any(y))) }, [4]bool{false, true, false, false}},
		{"any not all", func(x, y ObjectMatcher) ObjectMatcher { return Any(Not(All(x)), y) }, [4]bool{true, false, true, true}},
		{"double not", func(x, y ObjectMatcher) ObjectMatcher { return Not(Not(x)) }, [4]bool{false, true, false, true}},
		{"single all", func(x, y ObjectMatcher) ObjectMatcher { return All(x) }, [4]bool{false, true, false, true}},
		{"single any", func(x, y ObjectMatcher) ObjectMatcher { return Any(x) }, [4]bool{false, true, false, true}},
		{"empty all", func(x, y ObjectMatcher) ObjectMatcher { return All() }, [4]bool{true, true, true, true}},
		{"empty any", func(x, y ObjectMatcher) ObjectMatcher { return Any() }, [4]bool{false, false, false, false}},
		{"not empty all", func(x, y ObjectMatcher) ObjectMatcher { return Not(All()) }, [4]bool{false, false, false, false}},
		{"not empty any", func(x, y ObjectMatcher) ObjectMatcher { return Not(Any()) }, [4]bool{true, true, true, true}},
	}
	tags := kv("x", "yes", "y", "yes")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.compose(compositeLeaf("x"), compositeLeaf("y"))
			for _, viaString := range []bool{false, true} {
				for _, kind := range []Kind{Node, Way, Relation} {
					for mask := 0; mask < 4; mask++ {
						for _, order := range [][2]int{{0, 1}, {1, 0}} {
							beginComposition(m, kind, 4, true)
							if got := m.Matches(); got != c.want[0] {
								t.Fatalf("%v: result survived reset: %v, want %v", kind, got, c.want[0])
							}
							seen := 0
							for _, i := range order {
								if mask&(1<<i) == 0 {
									continue
								}
								tag := tags[i]
								if viaString {
									m.TagString(tag[0], tag[1])
								} else {
									m.Tag([]byte(tag[0]), []byte(tag[1]))
								}
								seen |= 1 << i
								// Query each intermediate state too: a composition can
								// become false after true, then become true again.
								if got := m.Matches(); got != c.want[seen] {
									t.Fatalf("kind=%v string=%v mask=%d order=%v seen=%d: got %v, want %v",
										kind, viaString, mask, order, seen, got, c.want[seen])
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestCombinatorGeometryAndTypes(t *testing.T) {
	cases := []struct {
		name      string
		kind      Kind
		nodeCount int
		closed    bool
		tags      [][2]string
		want      bool
	}{
		{"node building", Node, 0, false, kv("building", "yes"), false},
		{"node highway", Node, 0, false, kv("highway", "path"), false},
		{"open building", Way, 4, false, kv("building", "yes"), false},
		{"short closed building", Way, 3, true, kv("building", "yes"), false},
		{"closed building", Way, 4, true, kv("building", "yes"), true},
		{"private building", Way, 4, true, kv("building", "yes", "access", "private"), false},
		{"open highway", Way, 3, false, kv("highway", "path"), true},
		{"empty highway", Way, 0, false, kv("highway", "path"), true},
		{"empty building", Way, 0, false, kv("building", "yes"), false},
		{"multipolygon", Relation, 0, false, kv("type", "multipolygon", "building", "yes"), true},
		{"late boundary", Relation, 0, false, kv("building", "yes", "type", "boundary"), true},
		{"route first", Relation, 0, false, kv("type", "route", "type", "multipolygon", "building", "yes"), false},
		{"multipolygon first", Relation, 0, false, kv("type", "multipolygon", "type", "route", "building", "yes"), true},
		{"missing type", Relation, 0, false, kv("building", "yes"), false},
		{"private relation", Relation, 0, false, kv("access", "private", "type", "boundary", "building", "yes"), false},
	}
	m := All(
		Any(compositeLeaf("a/building"), compositeLeaf("w/highway")),
		Not(compositeLeaf("access=*private*")),
	)
	for _, viaString := range []bool{false, true} {
		for _, c := range cases {
			beginComposition(m, c.kind, c.nodeCount, c.closed)
			for _, tag := range c.tags {
				if viaString {
					m.TagString(tag[0], tag[1])
				} else {
					m.Tag([]byte(tag[0]), []byte(tag[1]))
				}
			}
			if got := m.Matches(); got != c.want {
				t.Errorf("%s string=%v: got %v, want %v", c.name, viaString, got, c.want)
			}
		}
	}
}

func TestNotComplementsKindsAndMissingTags(t *testing.T) {
	m := Not(compositeLeaf("w/foot=*no*"))
	for _, kind := range []Kind{Node, Way, Relation} {
		beginComposition(m, kind, 2, false)
		if !m.Matches() {
			t.Fatalf("%v: negated leaf did not match a tagless object", kind)
		}
		m.TagString("foot", "no")
		if got, want := m.Matches(), kind != Way; got != want {
			t.Errorf("%v: got %v, want %v", kind, got, want)
		}
	}
}

func TestCombinatorsCopyChildSlice(t *testing.T) {
	for _, compose := range []struct {
		name string
		fn   func(...ObjectMatcher) ObjectMatcher
	}{{"All", All}, {"Any", Any}} {
		t.Run(compose.name, func(t *testing.T) {
			children := []ObjectMatcher{compositeLeaf("kept")}
			m := compose.fn(children...)
			children[0] = compositeLeaf("replacement")
			m.BeginNode()
			m.TagString("replacement", "yes")
			if m.Matches() {
				t.Fatal("changing the caller's slice replaced a child")
			}
			m.TagString("kept", "yes")
			if !m.Matches() {
				t.Fatal("original child no longer receives tags")
			}
		})
	}
}

func TestCombinatorsZeroAllocs(t *testing.T) {
	m := All(
		Any(compositeLeaf("nw/highway"), compositeLeaf("a/building")),
		Not(compositeLeaf("access=*private*", "foot=*no*")),
	)
	strs := kv("name", "Main Street", "foot", "yes", "type", "boundary", "building", "yes", "highway", "path")
	tags := make([][2][]byte, len(strs))
	for i, tag := range strs {
		tags[i] = [2][]byte{[]byte(tag[0]), []byte(tag[1])}
	}
	for _, kind := range []Kind{Node, Way, Relation} {
		for _, viaString := range []bool{false, true} {
			allocs := testing.AllocsPerRun(1000, func() {
				beginComposition(m, kind, 4, true)
				if viaString {
					for _, tag := range strs {
						m.TagString(tag[0], tag[1])
					}
				} else {
					for _, tag := range tags {
						m.Tag(tag[0], tag[1])
					}
				}
				sink = m.Matches()
			})
			if allocs != 0 {
				t.Errorf("kind=%v string=%v: %v allocations per object, want 0", kind, viaString, allocs)
			}
		}
	}
}
