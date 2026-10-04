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

// tagCase is one matching scenario: compile exprs, Begin(kind), feed tags in
// order, then expect Hits() == hits and Multipolygon() == mp.
type tagCase struct {
	name  string
	exprs []string
	kind  Kind
	tags  [][2]string
	hits  Types
	mp    bool
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
	{"multipolygon flag needs type key", []string{"a/building"}, Relation, kv("building", "yes", "name", "multipolygon"), Areas, false},
	{"multipolygon flag case sensitive", []string{"a/building"}, Relation, kv("type", "Multipolygon", "building", "yes"), Areas, false},
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

func TestBeginResets(t *testing.T) {
	m := MustCompile("w/highway", "a/building").Matcher()
	m.Begin(Way)
	m.Tag([]byte("highway"), []byte("x"))
	m.Tag([]byte("building"), []byte("y"))
	if m.Hits() != Ways|Areas {
		t.Fatalf("hits before Begin = %d, want Ways|Areas", m.Hits())
	}
	m.Begin(Way)
	if m.Hits() != 0 {
		t.Fatalf("hits after Begin = %d", m.Hits())
	}
	m.Begin(Relation)
	m.Tag([]byte("type"), []byte("boundary"))
	if !m.Multipolygon() {
		t.Fatal("multipolygon not set before Begin")
	}
	m.Begin(Relation)
	if m.Multipolygon() {
		t.Fatal("multipolygon survived Begin")
	}
	// The previous relation's type tag must not hide this one's.
	m.Tag([]byte("type"), []byte("multipolygon"))
	if !m.Multipolygon() {
		t.Fatal("type tag ignored after Begin")
	}
}

func TestBeginInvalidKindPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != "osmtf: invalid Kind" {
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
	// Osmium reads only a relation's first type tag, so with duplicate type
	// tags the multipolygon flag depends on their order. These cases stay out
	// of conformance, whose rows TestHitsIndependentOfTagOrder reorders.
	for _, c := range []tagCase{
		{"type=multipolygon then type=route", []string{"n/x"}, Relation, kv("type", "multipolygon", "type", "route"), 0, true},
		{"type=route then type=multipolygon", []string{"a/building"}, Relation, kv("type", "route", "type", "multipolygon", "building", "yes"), Areas, false},
		{"type=route then type=boundary", []string{"a/building"}, Relation, kv("building", "yes", "type", "route", "type", "boundary"), Areas, false},
	} {
		runCase(t, MustCompile(c.exprs...), c)
	}
}

var sink Types

func TestZeroAllocs(t *testing.T) {
	f := MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "r/type=multipolygon,boundary",
		"w/name,name:de=Kastanienallee,Kastanienstrasse", "n/addr:*", "n/name=*Paris", "a/building")
	// highway=residential must stay last so every core group keeps scanning and all five match kinds run inside AllocsPerRun.
	tags := [][2][]byte{
		{[]byte("name"), []byte("Main Street")}, {[]byte("surface"), []byte("asphalt")},
		{[]byte("type"), []byte("multipolygon")}, {[]byte("building"), []byte("yes")},
		{[]byte("addr:street"), []byte("x")}, {[]byte("highway"), []byte("residential")},
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
