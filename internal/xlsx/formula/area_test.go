package formula

import "testing"

func TestParseArea(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{{" b2:$A$1 ", "A1:B2", true}, {"A1:A1", "A1", true}, {"C4", "C4", true}, {"A:C", "", false}, {"1:3", "", false}, {"bad", "", false}} {
		r, ok := ParseArea(tc.in)
		if ok != tc.ok || ok && r.String() != tc.want {
			t.Errorf("ParseArea(%q) = %v, %v", tc.in, r, ok)
		}
	}
	for _, tc := range []struct{ in, want string }{{"$3:$1", "1:3"}, {"$C:$A", "A:C"}, {"$B$2:$A$1", "A1:B2"}, {"$A$1:$A$1", "A1:A1"}} {
		r, ok := ParseRange(tc.in)
		if !ok || r.Normalized().String() != tc.want {
			t.Errorf("Normalized(%q) = %v", tc.in, r.Normalized())
		}
	}
}

func TestAppendColumnName(t *testing.T) {
	for n, want := range map[int]string{0: "", 1: "A", 26: "Z", 27: "AA", 52: "AZ", 53: "BA", 702: "ZZ", 703: "AAA", 16384: "XFD"} {
		if got := string(AppendColumnName(nil, n)); got != want {
			t.Errorf("AppendColumnName(%d) = %q, want %q", n, got, want)
		}
	}
	for n := 0; n <= MaxCols+1; n++ {
		got := string(AppendColumnName([]byte("x"), n))
		if got != "x"+ColumnName(n) {
			t.Fatalf("column %d: %q", n, got)
		}
	}
}

func TestQuoteSheet(t *testing.T) {
	for _, name := range []string{"Budget", "Q1 Budget", "Bob's", "A1", ""} {
		if got, want := QuoteSheet(name)+"!", SheetPrefix(name, ""); got != want {
			t.Errorf("QuoteSheet(%q) = %q, want %q", name, got, want)
		}
	}
}

func FuzzParseArea(f *testing.F) {
	for _, s := range []string{"A1", "$B$2:A1", "1:3", "", "XFD1048576"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, ok := ParseArea(s)
		if ok {
			again, valid := ParseArea(r.String())
			if !valid || again != r {
				t.Fatalf("round trip %q: %v", s, r)
			}
		}
	})
}
