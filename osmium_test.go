package osmtf

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestOsmiumDifferential writes diffCorpus as an OSM XML file, runs the
// osmium binary's tags-filter over it for every diffExpressionSets entry, and
// requires the surviving object IDs to equal what Filter and the combination
// formula in the package doc give. Sets that do not compile must make osmium
// fail too.
func TestOsmiumDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in -short mode")
	}
	osmium, err := exec.LookPath("osmium")
	if err != nil {
		t.Skip("osmium not on PATH")
	}
	out, err := exec.Command(osmium, "--version").Output()
	if err != nil {
		t.Fatalf("osmium --version: %v", err)
	}
	version, _, _ := strings.Cut(string(out), "\n")
	t.Logf("%s: %s", osmium, version)
	objs := diffCorpus()
	file := filepath.Join(t.TempDir(), "corpus.osm")
	var buf bytes.Buffer
	if err := writeOSMXML(&buf, objs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sanity: "*" must return every tagged object, proving osmium read the file.
	tagged := 0
	for _, o := range objs {
		if len(o.tags) > 0 {
			tagged++
		}
	}
	if got, err := runOsmium(osmium, file, []string{"*"}); err != nil || len(got) != tagged {
		t.Fatalf(`osmium "*" returned %d objects (err %v), corpus has %d tagged`, len(got), err, tagged)
	}

	for _, exprs := range diffExpressionSets() {
		f, err := Compile(exprs...)
		want, osmErr := runOsmium(osmium, file, exprs)
		if err != nil {
			if osmErr == nil || !strings.Contains(osmErr.Error(), "Unknown object type") {
				t.Errorf("exprs %q: we rejected (%v) but osmium said %v", exprs, err, osmErr)
			}
			continue
		}
		if osmErr != nil {
			t.Fatalf("exprs %q: osmium failed: %v", exprs, osmErr)
		}
		got := matchLocally(f, objs)
		if !maps.Equal(got, want) {
			for id := range want {
				if !got[id] {
					t.Errorf("exprs %q: osmium matched %s, we did not", exprs, id)
				}
			}
			for id := range got {
				if !want[id] {
					t.Errorf("exprs %q: we matched %s, osmium did not", exprs, id)
				}
			}
		}
	}
}

// osmObject is one object of the differential corpus. nodeRefs is set for
// ways only.
type osmObject struct {
	kind     Kind
	id       int
	tags     [][2]string
	nodeRefs []int
}

// inFileOrder returns objs in the order an OSM file lists them: nodes, then
// ways, then relations, each kind keeping its order in objs.
func inFileOrder(objs []osmObject) []osmObject {
	sorted := slices.Clone(objs)
	slices.SortStableFunc(sorted, func(a, b osmObject) int { return cmp.Compare(a.kind, b.kind) })
	return sorted
}

// writeOSMXML writes objs to w as an OSM XML file in node, way, relation
// order. Nodes sit at 0,0, ways list their nodeRefs, and every relation has
// the same single way member, which osmium tags-filter -R never resolves.
func writeOSMXML(w io.Writer, objs []osmObject) error {
	// bw keeps the first write error, and Flush returns it.
	bw := bufio.NewWriter(w)
	bw.WriteString(xml.Header)
	bw.WriteString(`<osm version="0.6" generator="osmtf-test">` + "\n")
	for _, o := range inFileOrder(objs) {
		elem := [...]string{Node: "node", Way: "way", Relation: "relation"}[o.kind]
		fmt.Fprintf(bw, `  <%s id="%d" version="1"`, elem, o.id)
		if o.kind == Node {
			bw.WriteString(` lat="0" lon="0"`)
		}
		bw.WriteString(">\n")
		switch o.kind {
		case Way:
			for _, ref := range o.nodeRefs {
				fmt.Fprintf(bw, `    <nd ref="%d"/>`+"\n", ref)
			}
		case Relation:
			bw.WriteString(`    <member type="way" ref="1" role="outer"/>` + "\n")
		}
		for _, tag := range o.tags {
			bw.WriteString(`    <tag k="`)
			xml.EscapeText(bw, []byte(tag[0]))
			bw.WriteString(`" v="`)
			xml.EscapeText(bw, []byte(tag[1]))
			bw.WriteString(`"/>` + "\n")
		}
		fmt.Fprintf(bw, "  </%s>\n", elem)
	}
	bw.WriteString("</osm>\n")
	return bw.Flush()
}

// runOsmium runs osmium tags-filter -R over file with exprs and returns the
// first token of every OPL line it prints, which names a kept object: "n1",
// "w10", "r20". A non-zero exit is an error whose text includes stderr.
func runOsmium(osmium, file string, exprs []string) (map[string]bool, error) {
	cmd := exec.Command(osmium, append([]string{"tags-filter", "-R", "-f", "opl", file}, exprs...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	kept := map[string]bool{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line != "" {
			token, _, _ := strings.Cut(line, " ")
			kept[token] = true
		}
	}
	return kept, nil
}

// matchLocally returns the objects f matches, named as runOsmium names them,
// combining Hits with geometry by the formula in the package doc. Like a
// decoder, it reads objs in file order and reuses one Matcher, calling Begin
// per object.
func matchLocally(f *Filter, objs []osmObject) map[string]bool {
	matched := map[string]bool{}
	m := f.Matcher()
	for _, o := range inFileOrder(objs) {
		m.Begin(o.kind)
		for _, tag := range o.tags {
			m.Tag([]byte(tag[0]), []byte(tag[1]))
		}
		h := m.Hits()
		var match bool
		switch o.kind {
		case Node:
			match = h&Nodes != 0
		case Way:
			n := len(o.nodeRefs)
			match = h&Ways != 0 || (h&Areas != 0 && IsAreaWay(n, o.nodeRefs[0] == o.nodeRefs[n-1]))
		case Relation:
			match = h&Relations != 0 || (h&Areas != 0 && m.Multipolygon())
		}
		if match {
			matched[fmt.Sprintf("%c%d", "nwr"[o.kind], o.id)] = true
		}
	}
	return matched
}

// diffCorpus returns hand-picked objects with IDs below 100, then 150 objects
// from a fixed seed with IDs from 100 upward per kind. Keys and values hold no
// control characters, which would silently change the test: XML cannot carry
// most of them (xml.EscapeText writes U+FFFD instead), and a tab or newline
// written unescaped reads back as a space.
func diffCorpus() []osmObject {
	objs := []osmObject{
		{kind: Node, id: 1, tags: kv("amenity", "cafe")},
		{kind: Node, id: 2, tags: kv("highway", "primary")},
		{kind: Node, id: 3, tags: kv("highway", "residential")},
		{kind: Node, id: 4},
		{kind: Node, id: 5, tags: kv("name", "Rue de Paris Nord")},
		{kind: Node, id: 6, tags: kv("name:de", "Kastanienstrasse")},
		{kind: Node, id: 7, tags: kv("addr:street", "Main")},
		{kind: Node, id: 8, tags: kv("", "empty")},
		{kind: Node, id: 9, tags: kv("k", "")},
		{kind: Node, id: 10, tags: kv("x", "a*")},
		{kind: Node, id: 11, tags: kv("x", "b")},
		{kind: Node, id: 12, tags: kv("x", "za,bz")},
		{kind: Node, id: 13, tags: kv("x", "a")},
		{kind: Node, id: 14, tags: kv("highway!", "primary")},
		{kind: Node, id: 15, tags: kv("x/y", "z")},
		{kind: Node, id: 16, tags: kv("x", "a*b")},
		{kind: Node, id: 17, tags: kv("name", "Straße")},
		{kind: Way, id: 10, tags: kv("building", "yes"), nodeRefs: []int{1, 2, 3, 1}},
		{kind: Way, id: 11, tags: kv("building", "yes"), nodeRefs: []int{1, 2, 1}},
		{kind: Way, id: 12, tags: kv("building", "yes"), nodeRefs: []int{1, 2, 3, 4}},
		{kind: Way, id: 13, tags: kv("highway", "primary"), nodeRefs: []int{1, 2, 3, 1}},
		{kind: Way, id: 14, tags: kv("highway", "primary", "building", "yes"), nodeRefs: []int{1, 2, 3, 4, 1}},
		{kind: Way, id: 15, nodeRefs: []int{1, 2, 3, 1}},
		{kind: Relation, id: 20, tags: kv("type", "multipolygon", "building", "yes")},
		{kind: Relation, id: 21, tags: kv("type", "boundary", "building", "yes")},
		{kind: Relation, id: 22, tags: kv("type", "route", "building", "yes")},
		{kind: Relation, id: 23, tags: kv("building", "yes")},
		{kind: Relation, id: 24, tags: kv("type", "restriction")},
		{kind: Relation, id: 25, tags: kv("type", "multipolygon")},
		// Duplicate type tags, which osmium's XML reader keeps. Osmium reads
		// only a relation's first type tag, so r26 is not a multipolygon and
		// r27 is.
		{kind: Relation, id: 26, tags: kv("type", "route", "type", "multipolygon", "building", "yes")},
		{kind: Relation, id: 27, tags: kv("type", "multipolygon", "type", "route", "building", "yes")},
	}

	keys := []string{"amenity", "highway", "building", "name", "name:de", "addr:street",
		"addr:housenumber", "type", "note", "x", "k", "highway!", "x/y", ""}
	values := []string{"cafe", "primary", "residential", "yes", "Kastanienallee",
		"Kastanienstrasse", "Rue de Paris Nord", "Paris", "multipolygon", "boundary", "route",
		"restriction", "a*", "b", "za,bz", "a", "a*b", "Straße", " spaced ", ""}
	typeValues := []string{"multipolygon", "boundary", "route"}

	rng := rand.New(rand.NewSource(1))
	nextID := [...]int{Node: 100, Way: 100, Relation: 100}
	for range 150 {
		o := osmObject{kind: Kind(rng.Intn(3))}
		o.id = nextID[o.kind]
		nextID[o.kind]++

		// Half the relations get a type tag after their random tags, which
		// then skip the key "type", so generated objects keep unique keys.
		withType := o.kind == Relation && rng.Intn(2) == 0
		ntags := rng.Intn(5)
		for _, i := range rng.Perm(len(keys)) {
			if len(o.tags) == ntags {
				break
			}
			if withType && keys[i] == "type" {
				continue
			}
			o.tags = append(o.tags, [2]string{keys[i], values[rng.Intn(len(values))]})
		}
		if withType {
			o.tags = append(o.tags, [2]string{"type", typeValues[rng.Intn(len(typeValues))]})
		}

		if o.kind == Way {
			// 1–6 distinct refs to the fixed nodes n1–n10, and for half the
			// ways the first ref again, closing them.
			nrefs := 1 + rng.Intn(6)
			for _, i := range rng.Perm(10)[:nrefs] {
				o.nodeRefs = append(o.nodeRefs, i+1)
			}
			if rng.Intn(2) == 0 {
				o.nodeRefs = append(o.nodeRefs, o.nodeRefs[0])
			}
		}
		objs = append(objs, o)
	}
	return objs
}

// diffExpressionSets returns hand-picked expression sets, each man page
// example alone first, then 80 sets of 1–3 expressions from a fixed seed. No
// expression starts with '-', which osmium would read as an option. The last
// fixed set, x/y=z, has the unknown type letter 'x' and must fail to compile;
// generated sets can too (prefix "" with key "x/y"), but with this seed none
// does.
func diffExpressionSets() [][]string {
	sets := [][]string{
		{"n/amenity"},
		{"nw/highway"},
		{"/note"},
		{"note"},
		{"w/highway=primary"},
		{"w/highway!=primary"},
		{"r/type=multipolygon,boundary"},
		{"w/name,name:de=Kastanienallee,Kastanienstrasse"},
		{"n/addr:*"},
		{"n/name=*Paris"},
		{"a/building"},
		{"r/type=restriction"},
		{"*"},
		{"**"},
		{"=empty"},
		{"k="},
		{"highway!"},
		{"highway !=primary"},
		{"highway! =primary"},
		{"x=a*,b"},
		{"x=*a,b*"},
		{"x=***"},
		{"x!=*"},
		{"n/x/y=z"},
		{""},
		{"wa/building"},
		{"ra/building"},
		{"nw/highway", "r/type=restriction"},
		{"x/y=z"},
	}

	prefixes := []string{"", "/", "n/", "w/", "r/", "a/", "nw/", "wa/", "ra/", "nwr/", "nwra/", "rr/"}
	keys := []string{"amenity", "highway", "building", "name", "name,name:de", "addr:*", "*name",
		"*", "**", "type", "x", "note", "k", "", "highway!", "x/y", " highway "}
	ops := []string{"", "=", "!="}
	values := []string{"cafe", "primary", "primary,residential", "primary , residential", "yes",
		"*Paris", "Kastanien*", "*strasse", "*", "**", "***", "a*,b", "*a,b*", "", " primary ",
		"multipolygon,boundary", "a", "Straße"}

	rng := rand.New(rand.NewSource(1))
	for range 80 {
		set := make([]string, 1+rng.Intn(3))
		for i := range set {
			prefix := prefixes[rng.Intn(len(prefixes))]
			key := keys[rng.Intn(len(keys))]
			op := ops[rng.Intn(len(ops))]
			set[i] = prefix + key + op
			if op != "" { // a key-only expression has no value
				set[i] += values[rng.Intn(len(values))]
			}
		}
		sets = append(sets, set)
	}
	return sets
}
