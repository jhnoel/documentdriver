package cli

import "strings"

// Snake converts an OOXML name to the contract's key spelling,
// mechanically: an underscore goes before each upper-case letter that
// follows a lower-case letter or a digit, and before the last letter of
// an upper-case run that a lower-case letter follows; then everything is
// lower-cased. flipH is flip_h, txBox tx_box, lnSpcReduction
// ln_spc_reduction, forceAA force_aa, noChangeAspect no_change_aspect.
// Characters other than ASCII letters and digits are kept.
func Snake(name string) string {
	upper := func(c byte) bool { return c >= 'A' && c <= 'Z' }
	lower := func(c byte) bool { return c >= 'a' && c <= 'z' }
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	var b strings.Builder
	for i := range len(name) {
		c := name[i]
		if upper(c) && i > 0 {
			prev := name[i-1]
			if lower(prev) || digit(prev) || upper(prev) && i+1 < len(name) && lower(name[i+1]) {
				b.WriteByte('_')
			}
		}
		if upper(c) {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Unsnake finds the name in names whose Snake form is key; false when
// none or the key is not in snake form of any.
func Unsnake(key string, names []string) (string, bool) {
	for _, n := range names {
		if Snake(n) == key {
			return n, true
		}
	}
	return "", false
}

// Camel is the simple inverse of Snake: each underscore is dropped and
// the letter after it upper-cased (no_change_aspect is noChangeAspect).
// It cannot restore an upper-case run (force_aa gives forceAa): check the
// result against the names the schema knows, or use Unsnake.
func Camel(key string) string {
	var b strings.Builder
	up := false
	for i := range len(key) {
		c := key[i]
		switch {
		case c == '_':
			up = true
			continue
		case up && c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		}
		up = false
		b.WriteByte(c)
	}
	return b.String()
}
