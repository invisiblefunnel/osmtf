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
