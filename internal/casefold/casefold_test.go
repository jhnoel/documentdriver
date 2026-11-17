package casefold

import (
	"strings"
	"testing"
)

// TestSimple checks simple folding: equal under strings.EqualFold for
// one-rune folds, distinct for multi-rune ones, and no allocation for
// text already folded.
func TestSimple(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"Item", "iTEM"}, {"ΣΑΣ", "σας"}, {"Straße", "STRAßE"}, {"KELVIN", "Kelvin"}, {"ſ", "S"}, {"Ǆ", "ǆ"},
	} {
		if Simple(tc.a) != Simple(tc.b) {
			t.Errorf("Simple(%q) %q != Simple(%q) %q", tc.a, Simple(tc.a), tc.b, Simple(tc.b))
		}
		if !strings.EqualFold(tc.a, tc.b) {
			t.Errorf("strings.EqualFold(%q, %q) is false: the test is wrong", tc.a, tc.b)
		}
	}
	for _, tc := range []struct{ a, b string }{{"ß", "ss"}, {"ı", "i"}, {"a", "b"}} {
		if Simple(tc.a) == Simple(tc.b) {
			t.Errorf("Simple(%q) == Simple(%q): simple folding only", tc.a, tc.b)
		}
	}
	if s := "already folded"; Simple(s) != s {
		t.Error("lower ASCII changed")
	}
	if got := Simple("A\xffB"); got != "a\xffb" {
		t.Errorf("invalid UTF-8: %q", got)
	}
	if n := testing.AllocsPerRun(10, func() { _ = Simple("lower case text") }); n != 0 {
		t.Errorf("Simple of folded text allocates %v", n)
	}
}
