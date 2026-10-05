package osmtf

import (
	"io"
	"strings"
)

// ReadExpressions reads an expressions file as osmium tags-filter -e does
// and returns its expressions, ready for Compile. Each line is cut at its
// first '#', even mid-expression, so "name=a#b" becomes "name=a". A line
// with nothing left is skipped; otherwise one trailing '\r' is dropped and
// the rest is an expression, unchanged. So a line of only spaces, or only
// "\r", becomes an empty-key rule, as in osmium. The only error is one from
// r.
func ReadExpressions(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var exprs []string
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if line == "" {
			continue
		}
		exprs = append(exprs, strings.TrimSuffix(line, "\r"))
	}
	return exprs, nil
}
