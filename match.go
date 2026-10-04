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
	multipolygon bool  // the relation's first type tag is type=multipolygon or type=boundary
	typeSeen     bool  // a relation tag with key type was seen; later ones are ignored
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
	m.typeSeen = false
}

// The key and values that Tag checks a relation's first type tag against, as
// byte slices so it can compare them with bytes.Equal.
var (
	keyType         = []byte("type")
	valMultipolygon = []byte("multipolygon")
	valBoundary     = []byte("boundary")
)

// Tag tests one key/value pair and returns the hits so far, identical to
// what Hits would return. Neither slice is retained.
//
// The caller may stop feeding tags early, but then only the bits already set
// are meaningful: an unset bit, or a false Multipolygon, is final only after
// every tag. Stop early only once the combination formula in the package doc
// is already true for the object. Stopping on any hit,
//
//	if m.Tag(k, v) != 0 {
//		break
//	}
//
// loses relations whose type tag comes after the area hit, and open ways
// whose Areas hit precedes the tag that would hit a way rule.
func (m *Matcher) Tag(key, value []byte) Types {
	f := m.f
	// The core and area groups are each evaluated only while their bit is
	// unset; once set, a bit stays set until the next Begin.
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
	// Osmium reads only a relation's first type tag, so that tag alone decides
	// the multipolygon flag and any later type tag is ignored.
	if m.kind == Relation && !m.typeSeen && bytes.Equal(key, keyType) {
		m.typeSeen = true
		m.multipolygon = bytes.Equal(value, valMultipolygon) || bytes.Equal(value, valBoundary)
	}
	return m.hits
}

// Hits reports which rule groups have matched since Begin: the kind's own
// bit (Nodes, Ways, or Relations) for a core hit, and Areas for an area
// rule hit. The package doc shows how to combine them into osmium's result.
func (m *Matcher) Hits() Types {
	return m.hits
}

// Multipolygon reports whether the relation's first type tag seen since
// Begin has the value multipolygon or boundary, osmium's rule for when a
// relation counts as an area. Always false for other kinds.
func (m *Matcher) Multipolygon() bool {
	return m.multipolygon
}
