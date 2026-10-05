package osmtf

import (
	"sync"
	"testing"
)

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

func kv(pairs ...string) [][2]string {
	out := make([][2]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, [2]string{pairs[i], pairs[i+1]})
	}
	return out
}

// tagCase is one matching scenario. Expected groups assume a way qualifies
// as an area; runCase also exercises ways whose geometry rules areas out.
type tagCase struct {
	name  string
	exprs []string
	kind  Kind
	tags  [][2]string
	hits  ruleTypes
	mp    bool
}

var conformance = []tagCase{
	// man page examples
	{"n/amenity node", []string{"n/amenity"}, Node, kv("amenity", "cafe"), nodes, false},
	{"n/amenity way", []string{"n/amenity"}, Way, kv("amenity", "cafe"), 0, false},
	{"nw/highway node", []string{"nw/highway"}, Node, kv("highway", "primary"), nodes, false},
	{"nw/highway way", []string{"nw/highway"}, Way, kv("highway", "primary"), ways, false},
	{"nw/highway relation", []string{"nw/highway"}, Relation, kv("highway", "primary"), 0, false},
	{"/note relation", []string{"/note"}, Relation, kv("note", "x"), relations, false},
	{"note way", []string{"note"}, Way, kv("note", "x"), ways, false},
	{"w/highway=primary hit", []string{"w/highway=primary"}, Way, kv("highway", "primary"), ways, false},
	{"w/highway=primary other value", []string{"w/highway=primary"}, Way, kv("highway", "secondary"), 0, false},
	{"w/highway=primary case", []string{"w/highway=primary"}, Way, kv("highway", "Primary"), 0, false},
	{"w/highway!=primary other value", []string{"w/highway!=primary"}, Way, kv("highway", "secondary"), ways, false},
	{"w/highway!=primary same value", []string{"w/highway!=primary"}, Way, kv("highway", "primary"), 0, false},
	{"w/highway!=primary missing key", []string{"w/highway!=primary"}, Way, kv("name", "x"), 0, false},
	{"w/highway!=primary no tags", []string{"w/highway!=primary"}, Way, nil, 0, false},
	{"r/type list boundary", []string{"r/type=multipolygon,boundary"}, Relation, kv("type", "boundary"), relations, true},
	{"r/type list route", []string{"r/type=multipolygon,boundary"}, Relation, kv("type", "route"), 0, false},
	{"key list name:de", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name:de", "Kastanienstrasse"), ways, false},
	{"key list name", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name", "Kastanienallee"), ways, false},
	{"key list wrong key", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name:en", "Kastanienallee"), 0, false},
	{"key list partial value", []string{"w/name,name:de=Kastanienallee,Kastanienstrasse"}, Way, kv("name", "Kastanien"), 0, false},
	{"n/addr:* prefix", []string{"n/addr:*"}, Node, kv("addr:street", "Main"), nodes, false},
	{"n/addr:* no colon", []string{"n/addr:*"}, Node, kv("addr", "Main"), 0, false},
	{"n/addr:* not at start", []string{"n/addr:*"}, Node, kv("xaddr:street", "Main"), 0, false},
	{"n/name=*Paris substring", []string{"n/name=*Paris"}, Node, kv("name", "Rue de Paris Nord"), nodes, false},
	{"n/name=*Paris case", []string{"n/name=*Paris"}, Node, kv("name", "paris"), 0, false},
	{"a/building way", []string{"a/building"}, Way, kv("building", "yes"), areas, false},
	{"a/building node", []string{"a/building"}, Node, kv("building", "yes"), 0, false},
	{"a/building multipolygon", []string{"a/building"}, Relation, kv("type", "multipolygon", "building", "yes"), areas, true},
	{"a/building boundary", []string{"a/building"}, Relation, kv("type", "boundary", "building", "yes"), areas, true},
	{"a/building route", []string{"a/building"}, Relation, kv("type", "route", "building", "yes"), areas, false},
	{"a/building no type", []string{"a/building"}, Relation, kv("building", "yes"), areas, false},
	{"a/building type after hit", []string{"a/building"}, Relation, kv("building", "yes", "type", "multipolygon"), areas, true},
	{"r/type=restriction", []string{"r/type=restriction"}, Relation, kv("type", "restriction"), relations, false},
	// groups
	{"core and area from two rules", []string{"w/highway", "a/building"}, Way, kv("highway", "x", "building", "y"), ways | areas, false},
	{"core and area from one rule way", []string{"wa/building"}, Way, kv("building", "yes"), ways | areas, false},
	{"core and area from one rule relation", []string{"wa/building"}, Relation, kv("building", "yes"), areas, false},
	{"multipolygon flag without rules", []string{"n/x"}, Relation, kv("type", "boundary"), 0, true},
	{"multipolygon flag ignored on ways", []string{"w/x"}, Way, kv("type", "multipolygon"), 0, false},
	{"multipolygon flag ignored on nodes", []string{"n/x"}, Node, kv("type", "multipolygon"), 0, false},
	{"multipolygon flag needs type key", []string{"a/building"}, Relation, kv("building", "yes", "name", "multipolygon"), areas, false},
	{"multipolygon flag case sensitive", []string{"a/building"}, Relation, kv("type", "Multipolygon", "building", "yes"), areas, false},
	{"no expressions", nil, Node, kv("a", "b"), 0, false},
	{"second expression matches", []string{"n/a", "n/b"}, Node, kv("b", ""), nodes, false},
	{"later tag matches", []string{"n/amenity=cafe"}, Node, kv("name", "x", "cuisine", "y", "amenity", "cafe"), nodes, false},
	// wildcards and empties
	{"star any tag", []string{"*"}, Node, kv("foo", "bar"), nodes, false},
	{"star no tags", []string{"*"}, Node, nil, 0, false},
	{"double star", []string{"**"}, Node, kv("foo", "bar"), nodes, false},
	{"empty key tag", []string{"=empty"}, Node, kv("", "empty"), nodes, false},
	{"empty expression matches empty key", []string{""}, Node, kv("", "x"), nodes, false},
	{"empty expression normal key", []string{""}, Node, kv("k", "x"), 0, false},
	{"n/ empty key", []string{"n/"}, Node, kv("", "x"), nodes, false},
	{"k= empty value", []string{"k="}, Node, kv("k", ""), nodes, false},
	{"k= non-empty value", []string{"k="}, Node, kv("k", "x"), 0, false},
	{"k key-only empty value", []string{"k"}, Node, kv("k", ""), nodes, false},
	{"highway=* any value", []string{"highway=*"}, Node, kv("highway", ""), nodes, false},
	{"x!=* never", []string{"x!=*"}, Node, kv("x", "c"), 0, false},
	{"highway!= non-empty", []string{"highway!="}, Node, kv("highway", "primary"), nodes, false},
	{"highway!= empty", []string{"highway!="}, Node, kv("highway", ""), 0, false},
	{"highway! key-only", []string{"highway!"}, Node, kv("highway!", "x"), nodes, false},
	{"highway! key-only not highway", []string{"highway!"}, Node, kv("highway", "x"), 0, false},
	// lists, spaces, tabs, bang placement
	{"list with spaces", []string{"highway=primary , residential"}, Node, kv("highway", "residential"), nodes, false},
	{"list literal star", []string{"x=a*,b"}, Node, kv("x", "a*"), nodes, false},
	{"list literal star b", []string{"x=a*,b"}, Node, kv("x", "b"), nodes, false},
	{"list literal star no prefix", []string{"x=a*,b"}, Node, kv("x", "a"), 0, false},
	{"substring with comma", []string{"x=*a,b*"}, Node, kv("x", "za,bz"), nodes, false},
	{"substring with comma not list", []string{"x=*a,b*"}, Node, kv("x", "a"), 0, false},
	{"spaces trimmed", []string{" highway = primary "}, Node, kv("highway", "primary"), nodes, false},
	{"tab not trimmed", []string{"highway=\tprimary"}, Node, kv("highway", "primary"), 0, false},
	{"tab kept literally", []string{"highway=\tprimary"}, Node, kv("highway", "\tprimary"), nodes, false},
	{"inverted list other", []string{"x!=a,b"}, Node, kv("x", "c"), nodes, false},
	{"inverted list member", []string{"x!=a,b"}, Node, kv("x", "a"), 0, false},
	{"inverted list missing key", []string{"x!=a,b"}, Node, kv("y", "c"), 0, false},
	{"space before bang inverted", []string{"highway !=primary"}, Node, kv("highway", "secondary"), nodes, false},
	{"space after bang not inverted", []string{"highway! =primary"}, Node, kv("highway", "secondary"), 0, false},
	{"later slash literal", []string{"n/x/y=z"}, Node, kv("x/y", "z"), nodes, false},
	{"triple star substring star", []string{"x=***"}, Node, kv("x", "a*b"), nodes, false},
	{"triple star no star", []string{"x=***"}, Node, kv("x", "ab"), 0, false},
}

var wayGeometries = []struct {
	n      int
	closed bool
}{
	{0, false}, {1, true}, {3, true}, {4, false}, {4, true}, {5, true},
}

// beginObject dispatches test objects through the same explicit starts a
// decoder uses. The geometry describes the test way, regardless of its rules.
func beginObject(m *Matcher, kind Kind, n int, closed bool) {
	switch kind {
	case Node:
		m.BeginNode()
	case Way:
		m.BeginWay(n, closed)
	case Relation:
		m.BeginRelation()
	default:
		panic("invalid test kind")
	}
}

// runCase checks byte and string tags, early stopping, and final results
// against the conformance table. Ways run with every boundary geometry.
func runCase(t *testing.T, f *Filter, c tagCase) {
	t.Helper()
	geometries := wayGeometries[:1]
	if c.kind == Way {
		geometries = wayGeometries
	}
	for _, w := range geometries {
		m, ms, early := f.Matcher(), f.Matcher(), f.Matcher()
		beginObject(&m, c.kind, w.n, w.closed)
		beginObject(&ms, c.kind, w.n, w.closed)
		beginObject(&early, c.kind, w.n, w.closed)
		if m.Matches() {
			t.Fatalf("%s: matched before tags", c.name)
		}
		for _, tg := range c.tags {
			m.Tag([]byte(tg[0]), []byte(tg[1]))
			ms.TagString(tg[0], tg[1])
			if !early.Matches() {
				early.TagString(tg[0], tg[1])
			}
			if early.Matches() != m.Matches() || ms.Matches() != m.Matches() {
				t.Errorf("%s geometry=%+v: early, string, and byte results differ after %q", c.name, w, tg)
			}
		}
		if ms != m {
			t.Errorf("%s: TagString left %+v, Tag left %+v", c.name, ms, m)
		}
		wantHits := c.hits
		if c.kind == Way && !(w.closed && w.n >= 4) {
			wantHits &^= areas
		}
		if m.hits != wantHits || m.multipolygon != c.mp {
			t.Errorf("%s geometry=%+v: hits=%v mp=%v, want hits=%v mp=%v", c.name, w, m.hits, m.multipolygon, wantHits, c.mp)
		}
		want := c.hits&(nodes|ways|relations) != 0 ||
			(c.hits&areas != 0 && ((c.kind == Way && w.closed && w.n >= 4) || c.mp))
		if got := m.Matches(); got != want {
			t.Errorf("%s geometry=%+v: Matches=%v, want %v", c.name, w, got, want)
		}
	}
}

func TestConformance(t *testing.T) {
	for _, c := range conformance {
		runCase(t, MustCompile(c.exprs...), c)
	}
}

func TestWayAreaRule(t *testing.T) {
	for _, expr := range []string{"a/building", "w/building", "wa/building"} {
		m := MustCompile(expr).Matcher()
		for _, w := range wayGeometries {
			m.BeginWay(w.n, w.closed)
			m.TagString("building", "yes")
			want := expr != "a/building" || (w.closed && w.n >= 4)
			if got := m.Matches(); got != want {
				t.Errorf("%s geometry=%+v: Matches=%v, want %v", expr, w, got, want)
			}
		}
	}
}

func TestBeginResets(t *testing.T) {
	f := MustCompile("n/amenity", "w/highway", "a/building")
	for _, previous := range []Kind{Node, Way, Relation} {
		for _, next := range []Kind{Node, Way, Relation} {
			for _, w := range wayGeometries {
				m := f.Matcher()
				beginObject(&m, previous, 4, true)
				for _, tg := range kv("amenity", "cafe", "highway", "primary", "building", "yes", "type", "boundary") {
					m.TagString(tg[0], tg[1])
				}
				if !m.Matches() {
					t.Fatalf("%v: previous object did not match", previous)
				}
				beginObject(&m, next, w.n, w.closed)
				if m.Matches() {
					t.Fatalf("%v -> %v: match survived reset", previous, next)
				}
				m.TagString("building", "yes")
				want := next == Way && w.closed && w.n >= 4
				if m.Matches() != want {
					t.Errorf("%v -> %v geometry=%+v: area state survived reset", previous, next, w)
				}
				m.TagString("type", "route")
				m.TagString("type", "multipolygon")
				if m.Matches() != want {
					t.Errorf("%v -> %v: duplicate type changed result", previous, next)
				}
				m.BeginRelation()
				m.TagString("building", "yes")
				m.TagString("type", "boundary")
				if !m.Matches() {
					t.Error("first type tag ignored after BeginRelation")
				}
			}
		}
	}
}

func TestZeroMatcherStartsPanic(t *testing.T) {
	for _, kind := range []Kind{Node, Way, Relation} {
		t.Run(kind.String(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != "osmtf: zero Matcher; use Filter.Matcher" {
					t.Fatalf("recovered %v", r)
				}
			}()
			var m Matcher
			beginObject(&m, kind, 0, false)
		})
	}
}

func TestTagBeforeBegin(t *testing.T) {
	m := MustCompile("n/a", "w/a").Matcher()
	if m.Matches() {
		t.Fatal("new Matcher matched before tags")
	}
	m.Tag([]byte("a"), nil)
	if !m.Matches() {
		t.Fatal("new Matcher did not start in the node state")
	}
}

func TestNilKeyValue(t *testing.T) {
	m := MustCompile("=").Matcher()
	m.BeginNode()
	m.Tag(nil, nil)
	if !m.Matches() {
		t.Fatal("nil key and value did not match the empty rule")
	}
	m = MustCompile("k").Matcher()
	m.BeginNode()
	m.Tag([]byte("k"), nil)
	if !m.Matches() {
		t.Fatal("nil value did not match the key-only rule")
	}
	m.BeginNode()
	m.Tag([]byte{}, []byte{})
	if m.Matches() {
		t.Fatal("empty key matched a rule for key k")
	}
}

func TestDuplicateKeys(t *testing.T) {
	m := MustCompile("w/highway=primary").Matcher()
	m.BeginWay(4, true)
	m.Tag([]byte("highway"), []byte("secondary"))
	m.Tag([]byte("highway"), []byte("primary"))
	if m.hits != ways {
		t.Fatalf("hits = %v, want ways", m.hits)
	}
	// Osmium reads only a relation's first type tag, so with duplicate type
	// tags the multipolygon flag depends on their order. These cases stay out
	// of conformance, whose rows TestHitsIndependentOfTagOrder reorders.
	for _, c := range []tagCase{
		{"type=multipolygon then type=route", []string{"n/x"}, Relation, kv("type", "multipolygon", "type", "route"), 0, true},
		{"type=route then type=multipolygon", []string{"a/building"}, Relation, kv("type", "route", "type", "multipolygon", "building", "yes"), areas, false},
		{"type=route then type=boundary", []string{"a/building"}, Relation, kv("building", "yes", "type", "route", "type", "boundary"), areas, false},
	} {
		runCase(t, MustCompile(c.exprs...), c)
	}
}

var sink bool

func TestZeroAllocs(t *testing.T) {
	f := MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "r/type=multipolygon,boundary",
		"w/name,name:de=Kastanienallee,Kastanienstrasse", "n/addr:*", "n/name=*Paris", "a/building")
	// highway=residential must stay last so every core group keeps scanning and all five match kinds run inside AllocsPerRun.
	strs := kv("name", "Main Street", "surface", "asphalt", "type", "multipolygon", "building", "yes",
		"addr:street", "x", "highway", "residential")
	tags := make([][2][]byte, len(strs))
	for i, tg := range strs {
		tags[i] = [2][]byte{[]byte(tg[0]), []byte(tg[1])}
	}
	for _, kind := range []Kind{Node, Way, Relation} {
		for _, viaString := range []bool{false, true} {
			allocs := testing.AllocsPerRun(1000, func() {
				m := f.Matcher()
				beginObject(&m, kind, 4, true)
				if viaString {
					for _, tg := range strs {
						m.TagString(tg[0], tg[1])
					}
				} else {
					for _, tg := range tags {
						m.Tag(tg[0], tg[1])
					}
				}
				sink = m.Matches()
			})
			if allocs != 0 {
				t.Errorf("kind %v, via TagString %v: %v allocs per run, want 0", kind, viaString, allocs)
			}
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

// A raw area-rule hit must not hide a later applicable core rule.
func TestOpenWayMatchesLaterCoreTag(t *testing.T) {
	m := MustCompile("a/building", "w/highway").Matcher()
	m.BeginWay(4, false)
	m.TagString("building", "yes")
	if m.Matches() {
		t.Fatal("open way matched an area rule")
	}
	m.TagString("highway", "residential")
	if !m.Matches() {
		t.Fatal("open way lost its later core match")
	}
}

func TestConcurrentMatchers(t *testing.T) {
	f := MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "a/building", "r/type=multipolygon,boundary")
	cases := []tagCase{
		{"node", nil, Node, kv("amenity", "cafe"), nodes, false},
		{"way", nil, Way, kv("highway", "secondary", "building", "yes"), ways | areas, false},
		{"relation", nil, Relation, kv("building", "yes", "type", "boundary"), relations | areas, true},
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
