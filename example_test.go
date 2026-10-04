package osmtf_test

import (
	"fmt"

	"github.com/invisiblefunnel/osmtf"
)

func ExampleFilter_Matcher() {
	f := osmtf.MustCompile("w/highway", "a/building")
	m := f.Matcher()

	// A way tagged building=yes, fed one tag at a time by a decoder.
	m.Begin(osmtf.Way)
	m.Tag([]byte("building"), []byte("yes"))
	h := m.Hits()

	// The caller combines hits with geometry it already knows.
	closedWith5Nodes := osmtf.IsAreaWay(5, true)
	openWith5Nodes := osmtf.IsAreaWay(5, false)
	fmt.Println(h&osmtf.Ways != 0 || (h&osmtf.Areas != 0 && closedWith5Nodes))
	fmt.Println(h&osmtf.Ways != 0 || (h&osmtf.Areas != 0 && openWith5Nodes))
	// Output:
	// true
	// false
}
