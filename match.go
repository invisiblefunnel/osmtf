package osmtf

import "bytes"

// match reports whether b matches s's pattern.
func (s *strMatcher) match(b []byte) bool {
	switch s.kind {
	case matchAny:
		return true
	case matchEqual:
		return bytes.Equal(b, s.pat)
	case matchPrefix:
		return bytes.HasPrefix(b, s.pat)
	case matchSubstring:
		// An empty pat matches everything, as C strstr does, so "**"
		// behaves like "*".
		return bytes.Contains(b, s.pat)
	case matchList:
		for _, item := range s.list {
			if bytes.Equal(b, item) {
				return true
			}
		}
	}
	return false
}

// match reports whether the tag key=value matches r: the key matcher accepts
// key and the value matcher's result equals r.want. So highway!=primary
// matches highway=secondary but not highway=primary or name=secondary.
func (r *rule) match(key, value []byte) bool {
	return r.key.match(key) && (r.value.match(value) == r.want)
}

// Matcher is per-object state. It is a value type and allocates nothing.
// The zero Matcher is not usable; obtain one from Filter.Matcher.
type Matcher struct {
	f            *Filter
	kind         Kind
	hits         Types // groups matched since Begin
	multipolygon bool  // a relation tag type=multipolygon or type=boundary was seen
}

// Matcher returns a Matcher for f, in the state Begin(Node) would leave it.
// Call Begin before each object's tags. A Matcher must not be used by more
// than one goroutine at a time, but any number of Matchers may share f.
func (f *Filter) Matcher() Matcher {
	return Matcher{f: f}
}

// Begin resets the state for a new object of the given kind.
// It panics with "osmtf: invalid Kind" if kind is not Node, Way, or Relation.
func (m *Matcher) Begin(kind Kind) {
	if kind > Relation {
		panic("osmtf: invalid Kind")
	}
	m.kind = kind
	m.hits = 0
	m.multipolygon = false
}

// The relation tags that set the multipolygon flag, type=multipolygon and
// type=boundary, as byte slices so Tag can compare them with bytes.Equal.
var (
	keyType         = []byte("type")
	valMultipolygon = []byte("multipolygon")
	valBoundary     = []byte("boundary")
)

// Tag tests one key/value pair and returns the hits so far, identical to
// what Hits would return. Neither slice is retained.
func (m *Matcher) Tag(key, value []byte) Types {
	f := m.f
	// Each group is evaluated only while its result is unset; once set, it
	// stays set until the next Begin.
	kindBit := Types(1) << m.kind
	if m.hits&kindBit == 0 {
		for _, i := range f.core[m.kind] {
			if f.rules[i].match(key, value) {
				m.hits |= kindBit
				break
			}
		}
	}
	if m.kind != Node && m.hits&Areas == 0 {
		for _, i := range f.area {
			if f.rules[i].match(key, value) {
				m.hits |= Areas
				break
			}
		}
	}
	if m.kind == Relation && !m.multipolygon && bytes.Equal(key, keyType) &&
		(bytes.Equal(value, valMultipolygon) || bytes.Equal(value, valBoundary)) {
		m.multipolygon = true
	}
	return m.hits
}

// Hits reports which rule groups have matched since Begin: the kind's own
// bit (Nodes, Ways, or Relations) for a core hit, and Areas for an area
// rule hit.
func (m *Matcher) Hits() Types {
	return m.hits
}

// Multipolygon reports whether, since Begin, a relation tag
// type=multipolygon or type=boundary was seen. Always false for other kinds.
func (m *Matcher) Multipolygon() bool {
	return m.multipolygon
}
