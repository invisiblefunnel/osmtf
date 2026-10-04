package osmtf

import (
	"reflect"
	"testing"
)

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

type ruleShape struct {
	types      Types
	key, value smShape
	want       bool
}

func shapeOfRule(r rule) ruleShape {
	return ruleShape{r.types, shapeOfSM(r.key), shapeOfSM(r.value), r.want}
}

var (
	eq   = func(p string) smShape { return smShape{kind: matchEqual, pat: p} }
	pre  = func(p string) smShape { return smShape{kind: matchPrefix, pat: p} }
	sub  = func(p string) smShape { return smShape{kind: matchSubstring, pat: p} }
	lst  = func(items ...string) smShape { return smShape{kind: matchList, list: items} }
	anyM = smShape{kind: matchAny}
	nwr  = Nodes | Ways | Relations
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
