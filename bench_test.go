package osmtf

import "testing"

// BenchmarkTag feeds a ten-tag way to a filter of a dozen rules. In "hit" the
// first core hit lands on the seventh tag; in "miss" nothing matches, so every
// tag scans every way and area rule.
func BenchmarkTag(b *testing.B) {
	f := MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "r/type=multipolygon,boundary",
		"w/name,name:de=Kastanienallee,Kastanienstrasse", "n/addr:*", "n/name=*Paris", "a/building",
		"r/type=restriction", "nwr/note", "w/railway=rail,light_rail", "wa/natural=water")
	cases := []struct {
		name string
		tags [][2]string
	}{
		{"hit", kv("surface", "asphalt", "lanes", "2", "maxspeed", "30", "oneway", "no", "lit", "yes",
			"sidewalk", "both", "highway", "residential", "name", "Main Street", "width", "6", "smoothness", "good")},
		{"miss", kv("surface", "asphalt", "lanes", "2", "maxspeed", "30", "oneway", "no", "lit", "yes",
			"sidewalk", "both", "width", "6", "smoothness", "good", "natural", "grass", "source", "survey")},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			tags := make([][2][]byte, len(c.tags))
			for i, tg := range c.tags {
				tags[i] = [2][]byte{[]byte(tg[0]), []byte(tg[1])}
			}
			m := f.Matcher()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m.Begin(Way)
				for _, tg := range tags {
					sink |= m.Tag(tg[0], tg[1])
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/10, "ns/tag")
		})
	}
}
