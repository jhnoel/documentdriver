package ops

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// ResolveAlias resolves raw to the address it names, replacing a
// leading $name with the address an earlier operation bound to name
// and keeping what follows it: the suffix the named address takes
// ($r/prop:Cost, $r/field:1, $p/s5, $p/layer:Name). A raw that is no
// alias is kept. A $name no earlier operation defined is not_found
// at field, naming the alias ($undefined), not the whole raw.
func ResolveAlias(raw string, aliases map[string]string, field string) (string, error) {
	if !strings.HasPrefix(raw, "$") {
		return raw, nil
	}
	name, suffix, ok := splitAlias(raw)
	if !ok {
		return "", cli.Errorf(cli.CodeNotFound, field, "alias %s is not defined", raw)
	}
	hit, ok := aliases[name]
	if ok {
		return hit + suffix, nil
	}
	return "", cli.Errorf(cli.CodeNotFound, field, "alias $%s is not defined", name)
}

// splitAlias splits an alias address into its name and the suffix
// that follows it ("" for a bare $name). It reports false when s is
// no alias: it does not start with $, or its name is not a valid
// alias name.
func splitAlias(s string) (name, suffix string, ok bool) {
	if !strings.HasPrefix(s, "$") {
		return "", "", false
	}
	rest := s[1:]
	i := 0
	for i < len(rest) && aliasChar(rest[i]) {
		i++
	}
	name = rest[:i]
	if !cli.ValidAlias(name) {
		return "", "", false
	}
	return name, rest[i:], true
}

// aliasChar reports whether c may appear in an alias name:
// [A-Za-z0-9_], as cli.ValidAlias allows after the first letter.
func aliasChar(c byte) bool {
	return ascii.IsAlnum(rune(c)) || c == '_'
}

// suffixValid reports whether suffix is a syntactically valid
// continuation of a page or shape address: what may follow page0 or
// page0/s1. An empty suffix (a bare $name) is valid.
func suffixValid(suffix string) bool {
	if suffix == "" {
		return true
	}
	if _, err := vsd.Parse("page0" + suffix); err == nil {
		return true
	}
	_, err := vsd.Parse("page0/s1" + suffix)
	return err == nil
}

// suffixShapeValid reports whether suffix could leave a shape
// address: page0 plus suffix or page0/s1 plus suffix parses as a
// shape (a page alias's /s5, or a shape alias's ~dup, among others).
func suffixShapeValid(suffix string) bool {
	if suffix == "" {
		return true
	}
	if a, err := vsd.Parse("page0" + suffix); err == nil && a.Kind == vsd.KindShape {
		return true
	}
	a, err := vsd.Parse("page0/s1" + suffix)
	return err == nil && a.Kind == vsd.KindShape
}
