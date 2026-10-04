package osmtf

import "testing"

func TestStrMatcherMatch(t *testing.T) {
	cases := []struct {
		pattern, input string
		want           bool
	}{
		{"*", "", true}, {"*", "x", true},
		{"", "", true}, {"", "x", false},
		{"abc", "abc", true}, {"abc", "abcd", false}, {"abc", "ab", false}, {"abc", "ABC", false},
		{"abc*", "abc", true}, {"abc*", "abcd", true}, {"abc*", "ab", false}, {"abc*", "xabc", false},
		{"*abc", "abc", true}, {"*abc", "xabcx", true}, {"*abc", "ab", false},
		{"*abc*", "xabcx", true}, {"*abc*", "ab", false},
		{"**", "", true}, {"**", "anything", true},
		{"***", "a*b", true}, {"***", "ab", false},
		{"a,b", "a", true}, {"a,b", "b", true}, {"a,b", "a,b", false}, {"a,b", "", false},
		{"a, ,b", "", true},
		{"a*,b", "a*", true}, {"a*,b", "a", false},
	}
	for _, c := range cases {
		cmp := compiler{blob: make([]byte, 0, len(c.pattern))}
		m := cmp.stringMatcher(c.pattern)
		if got := m.match([]byte(c.input)); got != c.want {
			t.Errorf("pattern %q input %q = %v, want %v", c.pattern, c.input, got, c.want)
		}
	}
}

func TestRuleMatch(t *testing.T) {
	cases := []struct {
		expr, key, value string
		want             bool
	}{
		{"highway!=primary", "highway", "secondary", true},
		{"highway!=primary", "highway", "primary", false},
		{"highway!=primary", "name", "primary", false},
		{"highway", "highway", "", true},
		{"highway", "highways", "", false},
		{"highway=primary,secondary", "highway", "secondary", true},
		{"addr:*=*", "addr:city", "x", true},
		{"x!=*", "x", "anything", false},
	}
	for _, c := range cases {
		cmp := compiler{blob: make([]byte, 0, len(c.expr))}
		r, err := cmp.parseExpr(c.expr)
		if err != nil {
			t.Fatalf("parseExpr(%q): %v", c.expr, err)
		}
		if got := r.match([]byte(c.key), []byte(c.value)); got != c.want {
			t.Errorf("rule %q on %q=%q = %v, want %v", c.expr, c.key, c.value, got, c.want)
		}
	}
}
