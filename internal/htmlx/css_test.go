package htmlx

import (
	"errors"
	"testing"
)

// TestQuotedName checks the quoted-string forms font-family takes.
func TestQuotedName(t *testing.T) {
	for in, want := range map[string]string{`'Calibri'`: "Calibri", `"Arial Black"`: "Arial Black", `'O\'Neil'`: "O'Neil", `'a\\b'`: `a\b`, `'a;b'`: "a;b", `''`: ""} {
		if got, err := QuotedName(in); err != nil || got != want {
			t.Errorf("QuotedName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"Calibri", `'Calibri`, `'a' 'b'`, `'a'x`, `"a'`, "", `'`, ` 'a'`} {
		if got, err := QuotedName(in); !errors.Is(err, ErrQuotedName) {
			t.Errorf("QuotedName(%q) = %q, %v; want ErrQuotedName", in, got, err)
		}
	}
}

// TestMeasure checks the number-with-unit grammar.
func TestMeasure(t *testing.T) {
	ok := []struct {
		v, unit string
		neg     bool
		want    float64
	}{{"12pt", "pt", false, 12}, {"10.5pt", "pt", false, 10.5}, {"-18pt", "pt", true, -18}, {".5pt", "pt", false, 0.5}, {"120%", "%", false, 120}, {"0", "", false, 0}}
	for _, c := range ok {
		if got, good := Measure(c.v, c.unit, c.neg); !good || got != c.want {
			t.Errorf("Measure(%q, %q, %v) = %v, %v; want %v", c.v, c.unit, c.neg, got, good, c.want)
		}
	}
	for _, c := range []struct {
		v, unit string
		neg     bool
	}{{"-18pt", "pt", false}, {"12", "pt", false}, {"pt", "pt", false}, {"1e3pt", "pt", false}, {"+1pt", "pt", false}, {"1.2.3pt", "pt", false}, {" 1pt", "pt", false}, {"-pt", "pt", true}, {".pt", "pt", false}, {"12px", "pt", false}} {
		if got, good := Measure(c.v, c.unit, c.neg); good {
			t.Errorf("Measure(%q, %q, %v) = %v, accepted", c.v, c.unit, c.neg, got)
		}
	}
}
