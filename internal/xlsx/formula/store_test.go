package formula

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K4a: Store and Spills (designer rows; frozen). K4a also adds, in its
// own store_chunk_test.go, a corpus law over XLSX_CORPUS:
// Store(Display(s)) == s for every corpus formula s, differences
// counted and listed for the designer.

func TestStore(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct{ in, want string }{
			{"B2+C2", "B2+C2"},
			{"concat(A1,A2)", "_xlfn.concat(A1,A2)"},
			{"_xlfn.CONCAT(A1)", "_xlfn.CONCAT(A1)"},
			{`"CONCAT(" & A1`, `"CONCAT(" & A1`},
			{"@A1:A3", "_xlfn.SINGLE(A1:A3)"},
			{"Tbl[@Col]", "Tbl[[#This Row],[Col]]"},
			{"[@[Col 2]]", "[[#This Row],[Col 2]]"},
			{"Tbl[@]", "Tbl[#This Row]"},
			{"SUM(Sheet2!A1:B2)", "SUM(Sheet2!A1:B2)"},
		} {
			got, err := Store(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("Store(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
				continue
			}
			if Display(got) != Display(tc.in) {
				t.Errorf("Display(Store(%q)) = %q", tc.in, Display(got))
			}
		}
		for _, in := range []string{"A1#", "SUM(A1#)", "(1"} {
			_, err := Store(in)
			var r *Refusal
			if !errors.As(err, &r) {
				t.Errorf("Store(%q): %v, want a *Refusal", in, err)
			}
		}
	})
}

func TestSpills(t *testing.T) {
	xlsxtest.Skip(t, func() {
		spilling := ""
		for _, f := range functionTable {
			if f.Spill != "" {
				spilling = f.Prefix + f.Name
				break
			}
		}
		if spilling == "" {
			t.Skip("the corpus table shows no spilling function")
		}
		if name, ok := Spills("1+" + spilling + "(3)"); !ok || name == "" {
			t.Errorf("Spills(%s) = %q %v", spilling, name, ok)
		}
		if _, ok := Spills(`SUM(A1:A3)&"` + spilling + `("`); ok {
			t.Error("a string literal was taken for a call")
		}
	})
}

func FuzzStore(f *testing.F) {
	for _, s := range []string{"SUM(A1:B2)", "@A1", "Tbl[@Col]", "A1#", `"x"&LET(a,1,a)`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		xlsxtest.Skip(t, func() {
			out, err := Store(s)
			if err != nil {
				return
			}
			if Join(Lex(out)) != out {
				t.Fatalf("Store(%q) = %q does not lex back", s, out)
			}
		})
	})
}
