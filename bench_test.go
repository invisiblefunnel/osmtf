package osmtf

import (
	"fmt"
	"testing"
)

// The benchmarks feed a ten-tag way to a filter of a dozen rules. In "hit"
// the first core hit lands on the seventh tag; in "miss" nothing matches, so
// every tag scans every way and area rule.
var (
	benchFilter = MustCompile("n/amenity", "nw/highway", "w/highway!=primary", "r/type=multipolygon,boundary",
		"w/name,name:de=Kastanienallee,Kastanienstrasse", "n/addr:*", "n/name=*Paris", "a/building",
		"r/type=restriction", "nwr/note", "w/railway=rail,light_rail", "wa/natural=water")
	benchCases = []struct {
		name string
		tags [][2]string
	}{
		{"hit", kv("surface", "asphalt", "lanes", "2", "maxspeed", "30", "oneway", "no", "lit", "yes",
			"sidewalk", "both", "highway", "residential", "name", "Main Street", "width", "6", "smoothness", "good")},
		{"miss", kv("surface", "asphalt", "lanes", "2", "maxspeed", "30", "oneway", "no", "lit", "yes",
			"sidewalk", "both", "width", "6", "smoothness", "good", "natural", "grass", "source", "survey")},
	}
)

// BenchmarkTag measures Tag with byte slices.
func BenchmarkTag(b *testing.B) {
	for _, c := range benchCases {
		b.Run(c.name, func(b *testing.B) {
			tags := make([][2][]byte, len(c.tags))
			for i, tg := range c.tags {
				tags[i] = [2][]byte{[]byte(tg[0]), []byte(tg[1])}
			}
			m := benchFilter.Matcher()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m.BeginWay(4, true)
				for _, tg := range tags {
					m.Tag(tg[0], tg[1])
				}
				sink = m.Matches()
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/10, "ns/tag")
		})
	}
}

// BenchmarkTagString is BenchmarkTag through TagString, which copies nothing
// and so should cost the same.
func BenchmarkTagString(b *testing.B) {
	for _, c := range benchCases {
		b.Run(c.name, func(b *testing.B) {
			m := benchFilter.Matcher()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				m.BeginWay(4, true)
				for _, tg := range c.tags {
					m.TagString(tg[0], tg[1])
				}
				sink = m.Matches()
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/10, "ns/tag")
		})
	}
}

// BenchmarkWayGeometry isolates skipping area rules for ways that cannot be
// areas. Nothing matches, so every applicable rule is scanned on every tag.
func BenchmarkWayGeometry(b *testing.B) {
	exprs := []string{"w/highway"}
	for i := 0; i < 64; i++ {
		exprs = append(exprs, fmt.Sprintf("a/key%d=value", i))
	}
	for _, filter := range []struct {
		name string
		f    *Filter
	}{{"mixed", benchFilter}, {"area-heavy", MustCompile(exprs...)}} {
		for _, geometry := range []struct {
			name   string
			n      int
			closed bool
		}{{"open", 4, false}, {"short", 3, true}, {"area", 4, true}} {
			b.Run(filter.name+"/"+geometry.name, func(b *testing.B) {
				m := filter.f.Matcher()
				tags := benchCases[1].tags
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					m.BeginWay(geometry.n, geometry.closed)
					for _, tg := range tags {
						m.TagString(tg[0], tg[1])
					}
					sink = m.Matches()
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(tags)), "ns/tag")
			})
		}
	}
}
