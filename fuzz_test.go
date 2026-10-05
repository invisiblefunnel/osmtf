package osmtf

import (
	"reflect"
	"strings"
	"testing"
)

// The reference oracle is written from osmium's semantics, as implemented in
// osmium-tool's src/util.cpp and src/command_tags_filter.cpp and libosmium's
// tags/matcher.hpp and util/string_matcher.hpp, with the strings package on
// string values. It shares no parsing or matching code with parse.go or
// match.go, only the matchKind labels.

// oracleMatcher is the oracle's key or value matcher. pat is set only for
// matchEqual, matchPrefix, and matchSubstring, and list only for matchList.
type oracleMatcher struct {
	kind matchKind
	pat  string
	list []string
}

// oracleStringMatcher applies osmium's string matcher construction steps,
// in order, to a raw key or value.
func oracleStringMatcher(s string) oracleMatcher {
	s = strings.Trim(s, " ") // 1: ASCII spaces only
	starFirst, starLast := strings.HasPrefix(s, "*"), strings.HasSuffix(s, "*")
	switch {
	case s == "*": // 2
		return oracleMatcher{kind: matchAny}
	case !starFirst && !starLast: // 3, which covers the empty string
		if strings.IndexByte(s, ',') < 0 {
			return oracleMatcher{kind: matchEqual, pat: s}
		}
		items := strings.Split(s, ",")
		for i := range items {
			items[i] = strings.Trim(items[i], " ")
		}
		return oracleMatcher{kind: matchList, list: items}
	case !starFirst: // 4: only the last byte is '*'
		return oracleMatcher{kind: matchPrefix, pat: s[:len(s)-1]}
	default: // 5: drop the leading '*', then a trailing '*' if one is left
		pat := s[1:]
		if strings.HasSuffix(pat, "*") {
			pat = pat[:len(pat)-1]
		}
		return oracleMatcher{kind: matchSubstring, pat: pat}
	}
}

// match reports whether s matches o. An empty substring pattern matches
// everything, as C strstr does.
func (o oracleMatcher) match(s string) bool {
	switch o.kind {
	case matchAny:
		return true
	case matchEqual:
		return s == o.pat
	case matchPrefix:
		return strings.HasPrefix(s, o.pat)
	case matchSubstring:
		return strings.Contains(s, o.pat)
	case matchList:
		for _, item := range o.list {
			if s == item {
				return true
			}
		}
	}
	return false
}

// oracleRule is the oracle's form of one expression.
type oracleRule struct {
	types      ruleTypes
	key, value oracleMatcher
	want       bool // false when inverted
}

// oracleTypeLetters maps the type letters allowed before the first '/' to
// their groups.
var oracleTypeLetters = map[byte]ruleTypes{'n': nodes, 'w': ways, 'r': relations, 'a': areas}

// oracleParse parses expr with osmium's grammar, [TYPES/]REST. It returns
// the offset of the first byte before the first '/' that is not a type
// letter, or -1 when expr is valid.
func oracleParse(expr string) (oracleRule, int) {
	r := oracleRule{types: nodes | ways | relations, want: true}
	rest := expr
	if slash := strings.IndexByte(expr, '/'); slash >= 0 {
		if slash > 0 {
			r.types = 0
			for i := 0; i < slash; i++ {
				bit, ok := oracleTypeLetters[expr[i]]
				if !ok {
					return oracleRule{}, i
				}
				r.types |= bit
			}
		}
		rest = expr[slash+1:]
	}
	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		// Key-only: the value matcher is "any".
		r.key, r.value = oracleStringMatcher(rest), oracleMatcher{kind: matchAny}
		return r, -1
	}
	// The '!' is looked for on the raw key, before trimming.
	key := rest[:eq]
	if key != "" && key[len(key)-1] == '!' {
		key = key[:len(key)-1]
		r.want = false
	}
	r.key, r.value = oracleStringMatcher(key), oracleStringMatcher(rest[eq+1:])
	return r, -1
}

// oracleMatch is osmium's nested loops over a whole tag list. For each tag it
// tests every rule for kind's own group and, for ways and relations, every
// area rule. For a relation, mp comes from its first type tag alone, as
// osmium's get_value_by_key("type") does: true when that tag's value is
// multipolygon or boundary.
func oracleMatch(rules []oracleRule, kind Kind, tags [][2]string) (hits ruleTypes, mp bool) {
	own := [...]ruleTypes{Node: nodes, Way: ways, Relation: relations}[kind]
	typeSeen := false
	for _, tag := range tags {
		k, v := tag[0], tag[1]
		for _, r := range rules {
			if r.types&own != 0 && r.key.match(k) && r.value.match(v) == r.want {
				hits |= own
			}
		}
		if kind != Node {
			for _, r := range rules {
				if r.types&areas != 0 && r.key.match(k) && r.value.match(v) == r.want {
					hits |= areas
				}
			}
		}
		if kind == Relation && k == "type" && !typeSeen {
			typeSeen = true
			mp = v == "multipolygon" || v == "boundary"
		}
	}
	return hits, mp
}

func oracleShape(r oracleRule) ruleShape {
	conv := func(o oracleMatcher) smShape { return smShape{kind: o.kind, pat: o.pat, list: o.list} }
	return ruleShape{r.types, conv(r.key), conv(r.value), r.want}
}

func TestOracleParse(t *testing.T) {
	for _, c := range compileShapeCases {
		r, bad := oracleParse(c.expr)
		if bad != -1 {
			t.Errorf("oracleParse(%q) reported error at %d", c.expr, bad)
		} else if got := oracleShape(r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("oracleParse(%q) = %+v, want %+v", c.expr, got, c.want)
		}
	}
	for _, c := range compileErrorCases {
		if _, bad := oracleParse(c.expr); bad != c.pos {
			t.Errorf("oracleParse(%q) error at %d, want %d", c.expr, bad, c.pos)
		}
	}
}

func TestOracleConformance(t *testing.T) {
	for _, c := range conformance {
		var rules []oracleRule
		for _, e := range c.exprs {
			r, _ := oracleParse(e)
			rules = append(rules, r)
		}
		if hits, mp := oracleMatch(rules, c.kind, c.tags); hits != c.hits || mp != c.mp {
			t.Errorf("%s: oracle hits=%d mp=%v, want hits=%d mp=%v", c.name, hits, mp, c.hits, c.mp)
		}
	}
}

func FuzzMatchAgainstOracle(f *testing.F) {
	for _, c := range conformance {
		var e [2]string
		copy(e[:], c.exprs)
		var tv [6]string
		for i, t := range c.tags {
			if i == 3 {
				break
			}
			tv[2*i], tv[2*i+1] = t[0], t[1]
		}
		f.Add(e[0], e[1], uint8(c.kind), tv[0], tv[1], tv[2], tv[3], tv[4], tv[5])
	}
	// Duplicate type tags, in both orders: osmium reads only the first.
	f.Add("a/building", "a/building", uint8(Relation), "type", "route", "type", "multipolygon", "building", "yes")
	f.Add("a/building", "a/building", uint8(Relation), "type", "multipolygon", "type", "route", "building", "yes")
	f.Fuzz(func(t *testing.T, e1, e2 string, kind uint8, k1, v1, k2, v2, k3, v3 string) {
		k := Kind(kind % 3)
		tags := [][2]string{{k1, v1}, {k2, v2}, {k3, v3}}
		exprs := []string{e1, e2}
		var rules []oracleRule
		oracleBad := -1
		for _, e := range exprs {
			r, bad := oracleParse(e)
			if bad != -1 {
				oracleBad = bad
				break
			}
			rules = append(rules, r)
		}
		fl, err := Compile(exprs...)
		if (err != nil) != (oracleBad != -1) {
			t.Fatalf("Compile(%q) err=%v, oracle bad=%d", exprs, err, oracleBad)
		}
		if err != nil {
			if err.(*ParseError).Pos != oracleBad {
				t.Fatalf("Compile(%q) Pos=%d, oracle %d", exprs, err.(*ParseError).Pos, oracleBad)
			}
			return
		}
		wantHits, wantMP := oracleMatch(rules, k, tags)
		geometries := wayGeometries[:1]
		if k == Way {
			geometries = wayGeometries
		}
		for _, w := range geometries {
			m, ms, early := fl.Matcher(), fl.Matcher(), fl.Matcher()
			beginObject(&m, k, w.n, w.closed)
			beginObject(&ms, k, w.n, w.closed)
			beginObject(&early, k, w.n, w.closed)
			for _, tg := range tags {
				m.Tag([]byte(tg[0]), []byte(tg[1]))
				ms.TagString(tg[0], tg[1])
				if !early.Matches() {
					early.TagString(tg[0], tg[1])
				}
			}
			applicableHits := wantHits
			if k == Way && !(w.closed && w.n >= 4) {
				applicableHits &^= areas
			}
			if m.hits != applicableHits || m.multipolygon != wantMP {
				t.Fatalf("exprs %q kind %d geometry=%+v tags %q: hits=%d mp=%v, oracle hits=%d mp=%v",
					exprs, k, w, tags, m.hits, m.multipolygon, applicableHits, wantMP)
			}
			want := wantHits&(nodes|ways|relations) != 0 ||
				(wantHits&areas != 0 && ((k == Way && w.closed && w.n >= 4) || wantMP))
			if m.Matches() != want || ms.Matches() != want || early.Matches() != want {
				t.Fatalf("exprs %q kind %d geometry=%+v tags %q: byte=%v string=%v early=%v, oracle=%v",
					exprs, k, w, tags, m.Matches(), ms.Matches(), early.Matches(), want)
			}
		}
	})
}

func FuzzCompile(f *testing.F) {
	for _, c := range compileShapeCases {
		f.Add(c.expr)
	}
	for _, c := range compileErrorCases {
		f.Add(c.expr)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		wantPos := -1
		if p := strings.IndexByte(expr, '/'); p > 0 {
			for i := 0; i < p; i++ {
				if !strings.ContainsRune("nwra", rune(expr[i])) {
					wantPos = i
					break
				}
			}
		}
		fl, err := Compile(expr)
		if wantPos == -1 {
			if err != nil || fl == nil || len(fl.rules) != 1 {
				t.Fatalf("Compile(%q) = %v, %v", expr, fl, err)
			}
			return
		}
		pe, ok := err.(*ParseError)
		if !ok || fl != nil || pe.Expr != expr || pe.Pos != wantPos {
			t.Fatalf("Compile(%q) = %v, %v; want ParseError at %d", expr, fl, err, wantPos)
		}
	})
}
