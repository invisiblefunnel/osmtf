package osmtf_test

import (
	"fmt"

	"github.com/invisiblefunnel/osmtf"
)

func ExampleFilter_Matcher() {
	f := osmtf.MustCompile("w/highway", "a/building")
	m := f.Matcher()

	// Geometry is supplied before tags, for every way. Guard empty ref lists.
	refs := []int64{1, 2, 3, 1}
	n := len(refs)
	m.BeginWay(n, n > 0 && refs[0] == refs[n-1])
	m.Tag([]byte("building"), []byte("yes"))
	fmt.Println(m.Matches())

	// The same tag on an open way does not match the area rule.
	refs = []int64{1, 2, 3, 4}
	n = len(refs)
	m.BeginWay(n, n > 0 && refs[0] == refs[n-1])
	m.TagString("building", "yes")
	fmt.Println(m.Matches())

	// A relation counts as an area when its first type tag is multipolygon
	// or boundary, which the Matcher tracks itself. TagString is Tag for
	// decoders that hold tags as strings.
	m.BeginRelation()
	m.TagString("type", "multipolygon")
	m.TagString("building", "yes")
	fmt.Println(m.Matches())

	m.BeginNode()
	m.TagString("building", "yes")
	fmt.Println(m.Matches()) // Area rules do not apply to nodes.
	// Output:
	// true
	// false
	// true
	// false
}

func ExampleFilter_CanMatch() {
	// Area rules apply to ways and relations, so a decoder can skip nodes.
	f := osmtf.MustCompile("a/building")
	fmt.Println(f.CanMatch(osmtf.Node), f.CanMatch(osmtf.Way), f.CanMatch(osmtf.Relation))
	// Output: false true true
}
