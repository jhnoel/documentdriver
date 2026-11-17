package htmlx

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/jhnoel/documentdriver/internal/ascii"
)

// StyleDecl is one CSS declaration.
type StyleDecl struct {
	Name, Value string
}

// StyleDecls parses an inline style into declarations sorted by property
// name (a later duplicate replaces an earlier one). Names are lowercased;
// values may not hold control characters, < > { }, url( or !; "unset"
// values are dropped.
func StyleDecls(css string) ([]StyleDecl, error) {
	m := map[string]string{}
	for part := range strings.SplitSeq(css, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("CSS declarations require property: value")
		}
		name = ascii.Lower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsFunc(value, func(r rune) bool {
			return unicode.IsControl(r) || strings.ContainsRune("<>{}", r)
		}) || strings.Contains(ascii.Lower(value), "url(") || strings.Contains(value, "!") {
			return nil, fmt.Errorf("unsupported CSS value")
		}
		if ascii.EqualFold(value, "unset") {
			continue
		}
		m[name] = value
	}
	out := make([]StyleDecl, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, StyleDecl{k, m[k]})
	}
	return out, nil
}
