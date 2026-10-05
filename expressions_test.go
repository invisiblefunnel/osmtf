package osmtf

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadExpressions(t *testing.T) {
	cases := []struct {
		name, text string
		want       []string
	}{
		{"empty", "", nil},
		{"comment only", "# nothing\n", nil},
		{"hash only", "#\n", nil},
		{"blank lines", "\n\n", nil},
		{"one per line", "n/amenity\nw/highway=primary\n", []string{"n/amenity", "w/highway=primary"}},
		{"no final newline", "n/amenity\nw/highway", []string{"n/amenity", "w/highway"}},
		{"crlf", "n/amenity\r\nw/highway\r\n", []string{"n/amenity", "w/highway"}},
		{"trailing comment keeps spaces", "n/amenity   # cafes\n", []string{"n/amenity   "}},
		{"comment cuts mid-expression", "name=a#b\n", []string{"name=a"}},
		{"cr before comment", "n/amenity\r# c\n", []string{"n/amenity"}},
		{"only one cr dropped", "amenity\r\r\n", []string{"amenity\r"}},
		{"spaces only is a rule", "   \n", []string{"   "}},
		{"cr only is a rule", "\r\n", []string{""}},
		{"spaces before comment is a rule", "  # c\n", []string{"  "}},
		{"tab kept", "\thighway\n", []string{"\thighway"}},
	}
	for _, c := range cases {
		got, err := ReadExpressions(strings.NewReader(c.text))
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: ReadExpressions(%q) = %q, %v; want %q", c.name, c.text, got, err, c.want)
		}
	}
}

func TestReadExpressionsError(t *testing.T) {
	want := errors.New("read failed")
	exprs, err := ReadExpressions(iotest.ErrReader(want))
	if exprs != nil || err != want {
		t.Fatalf("ReadExpressions = %q, %v; want nil, %v", exprs, err, want)
	}
}
