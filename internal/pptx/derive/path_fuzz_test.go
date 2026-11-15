package derive

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

// FuzzParsePath checks that the SVG path parser never panics, that every
// refusal wraps ErrPathSyntax, and that what it accepts is well formed
// (starts with M, right point count per command, finite coordinates) and
// survives FormatPath and a second parse unchanged.
func FuzzParsePath(f *testing.F) {
	for _, s := range []string{
		"M0 0 L10 10 Z",
		"m1,2 l3,4 h5 v-6 z",
		"M0 0 C1 2 3 4 5 6 Q7 8 9 10 Z M20 20 30 30 40 40",
		"M1e3 -2.5e-2 l.5.5 H0 V0",
		"M0 0 A1 1 0 0 1 2 2",
		"M0 0 S1 1 2 2",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		cmds, err := ParsePath(s)
		if err != nil {
			if !errors.Is(err, ErrPathSyntax) {
				t.Fatalf("ParsePath(%q): error %v does not wrap ErrPathSyntax", s, err)
			}
			return
		}
		if len(cmds) == 0 || cmds[0].Op != 'M' {
			t.Fatalf("ParsePath(%q) = %v: does not start with M", s, cmds)
		}
		want := map[byte]int{'M': 1, 'L': 1, 'C': 3, 'Q': 2, 'Z': 0}
		for _, c := range cmds {
			n, ok := want[c.Op]
			if !ok || len(c.Pts) != n {
				t.Fatalf("ParsePath(%q): command %c with %d points", s, c.Op, len(c.Pts))
			}
			for _, p := range c.Pts {
				if math.IsInf(p.X, 0) || math.IsNaN(p.X) || math.IsInf(p.Y, 0) || math.IsNaN(p.Y) {
					t.Fatalf("ParsePath(%q): non-finite point %v", s, p)
				}
			}
		}
		exact := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
		out := FormatPath(cmds, exact)
		again, err := ParsePath(out)
		if err != nil {
			t.Fatalf("ParsePath(%q) accepted, but its formatting %q is refused: %v", s, out, err)
		}
		if got := FormatPath(again, exact); got != out {
			t.Fatalf("round trip of %q: %q then %q", s, out, got)
		}
	})
}
