package osmtf

import (
	"bytes"
	"unsafe"
)

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

// Matcher is reusable per-object state. It is a value type and allocates
// nothing. The zero Matcher is not usable; obtain one from Filter.Matcher.
// Start each object with BeginNode, BeginWay, or BeginRelation before feeding
// its tags. Do not copy a Matcher while matching an object or use it from
// more than one goroutine at a time.
type Matcher struct {
	f            *Filter
	kind         Kind
	hits         ruleTypes // groups matched since the object's start
	areaWay      bool      // this way is closed and has at least four references
	multipolygon bool      // the relation's first type tag is multipolygon or boundary
	typeSeen     bool      // later relation type tags are ignored
}

// Matcher returns a Matcher for f, in the state BeginNode would leave it.
// Any number of Matchers may share f. Call the appropriate Begin method
// before each object's tags.
func (f *Filter) Matcher() Matcher {
	return Matcher{f: f}
}

// BeginNode resets the state for a new node.
// It panics with "osmtf: zero Matcher; use Filter.Matcher" on a zero Matcher.
func (m *Matcher) BeginNode() {
	m.begin(Node, false)
}

// BeginWay resets the state for a new way. Supply real geometry for every
// way, regardless of the filter: nodeCount is the length of its node reference
// list (including the repeated final reference of a closed way), and closed
// reports whether the first and last references are equal. For an empty list,
// pass 0 and false. Area rules apply only when closed and nodeCount >= 4.
// It panics with "osmtf: zero Matcher; use Filter.Matcher" on a zero Matcher.
func (m *Matcher) BeginWay(nodeCount int, closed bool) {
	m.begin(Way, closed && nodeCount >= 4)
}

// BeginRelation resets the state for a new relation. The first type tag
// determines whether area rules apply; subsequent type tags are ignored.
// It panics with "osmtf: zero Matcher; use Filter.Matcher" on a zero Matcher.
func (m *Matcher) BeginRelation() {
	m.begin(Relation, false)
}

func (m *Matcher) begin(kind Kind, areaWay bool) {
	if m.f == nil {
		panic("osmtf: zero Matcher; use Filter.Matcher")
	}
	*m = Matcher{f: m.f, kind: kind, areaWay: areaWay}
}

// The key and values that Tag checks a relation's first type tag against, as
// byte slices so it can compare them with bytes.Equal.
var (
	keyType         = []byte("type")
	valMultipolygon = []byte("multipolygon")
	valBoundary     = []byte("boundary")
)

// Tag tests one key/value pair. Neither slice is retained or modified.
// The caller may stop feeding an object's tags once Matches is true, but
// must still advance its decoder and check decoding errors. A false Matches
// result is final only after every tag.
func (m *Matcher) Tag(key, value []byte) {
	f := m.f
	// The core and area groups are each evaluated only while their bit is
	// unset; once set, a bit stays set until the next object's start.
	kindBit := ruleTypes(1) << m.kind
	if m.hits&kindBit == 0 {
		for _, i := range f.core[m.kind] {
			if f.rules[i].match(key, value) {
				m.hits |= kindBit
				break
			}
		}
	}
	if (m.areaWay || m.kind == Relation) && m.hits&areas == 0 {
		for _, i := range f.area {
			if f.rules[i].match(key, value) {
				m.hits |= areas
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
}

// TagString is Tag for a key and value held as strings. It hands the strings'
// bytes to Tag without copying, which is safe because Tag neither writes to
// nor retains its slices.
func (m *Matcher) TagString(key, value string) {
	m.Tag(unsafe.Slice(unsafe.StringData(key), len(key)),
		unsafe.Slice(unsafe.StringData(value), len(value)))
}

// Matches reports osmium's result for the current object, including area
// rules when its way geometry or first relation type tag qualifies. A true
// result stays true until the next BeginNode, BeginWay, or BeginRelation.
// A false result is final only after the last tag. Before any tags, Matches
// returns false.
func (m *Matcher) Matches() bool {
	return m.hits&^areas != 0 || (m.hits&areas != 0 && (m.areaWay || m.multipolygon))
}
