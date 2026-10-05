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

func ExampleAll() {
	highway := osmtf.MustCompile("w/highway").Matcher()
	excluded := osmtf.MustCompile("w/foot=*no*").Matcher()
	m := osmtf.All(&highway, osmtf.Not(&excluded))

	refs := []int64{101, 102, 103}
	n := len(refs)
	closed := n > 0 && refs[0] == refs[n-1]
	m.BeginWay(n, closed)
	m.TagString("highway", "path")
	fmt.Println(m.Matches()) // This way has no restriction tags.

	m.BeginWay(n, closed)
	m.TagString("highway", "path")
	m.TagString("foot", "no")
	// Read the final result only after all tags: the foot tag rejects the way.
	fmt.Println(m.Matches())
	// Output:
	// true
	// false
}

func ExampleAny() {
	cafe := osmtf.MustCompile("n/amenity=cafe").Matcher()
	bakery := osmtf.MustCompile("n/shop=bakery").Matcher()
	m := osmtf.Any(&cafe, &bakery)

	m.BeginNode()
	m.TagString("name", "Corner Bakery")
	m.TagString("shop", "bakery")
	fmt.Println(m.Matches())
	// Output: true
}

func ExampleNot() {
	prohibited := osmtf.MustCompile("w/foot=*no*").Matcher()
	m := osmtf.Not(&prohibited)

	// Negation includes kinds that the child cannot match.
	m.BeginNode()
	m.TagString("foot", "no")
	fmt.Println(m.Matches())

	refs := []int64{101, 102}
	n := len(refs)
	m.BeginWay(n, n > 0 && refs[0] == refs[n-1])
	m.TagString("foot", "no")
	fmt.Println(m.Matches())
	// Output:
	// true
	// false
}
