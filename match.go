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
