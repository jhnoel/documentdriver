package formula

import "testing"

func TestParseCell(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Cell
		ok   bool
	}{
		{"A1", Cell{Row: 1, Col: 1}, true},
		{"$B$2", Cell{Row: 2, Col: 2, RowAbs: true, ColAbs: true}, true},
		{"xfd1048576", Cell{Row: MaxRows, Col: MaxCols}, true},
		{"XFE1", Cell{}, false},
		{"A1048577", Cell{}, false},
		{"A0", Cell{}, false},
		{"ABCD1", Cell{}, false},
		{"A", Cell{}, false},
		{"1", Cell{}, false},
		{"A1 ", Cell{}, false},
		{"", Cell{}, false},
	} {
		got, ok := ParseCell(tc.in)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("ParseCell(%q) = %+v, %v; want %+v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseRangeRoundTrip(t *testing.T) {
	for _, s := range []string{"A1", "A1:B2", "$A$1:$B$2", "A:C", "$A:$C", "1:3", "$1:$3", "B2:A1", "XFD1048576"} {
		r, ok := ParseRange(s)
		if !ok {
			t.Errorf("ParseRange(%q) failed", s)
			continue
		}
		if got := r.String(); got != s {
			t.Errorf("ParseRange(%q).String() = %q", s, got)
		}
	}
	for _, s := range []string{"A1:", ":A1", "A1:B", "A:1", "0:1", "A1:B2:C3", "Sheet1!A1"} {
		if _, ok := ParseRange(s); ok {
			t.Errorf("ParseRange(%q) accepted", s)
		}
	}
}

func TestParseSqref(t *testing.T) {
	rs, ok := ParseSqref("A1:B2  D4 E:E")
	if !ok || len(rs) != 3 || rs[2].Kind != Cols {
		t.Fatalf("ParseSqref = %+v, %v", rs, ok)
	}
	for _, s := range []string{"", " ", "A1 B"} {
		if _, ok := ParseSqref(s); ok {
			t.Errorf("ParseSqref(%q) accepted", s)
		}
	}
}

func TestColumnName(t *testing.T) {
	for n, s := range map[int]string{1: "A", 26: "Z", 27: "AA", 702: "ZZ", 703: "AAA", MaxCols: "XFD"} {
		if got := ColumnName(n); got != s {
			t.Errorf("ColumnName(%d) = %q, want %q", n, got, s)
		}
		if got, ok := ColumnIndex(s); !ok || got != n {
			t.Errorf("ColumnIndex(%q) = %d, %v", s, got, ok)
		}
	}
	if ColumnName(0) != "" {
		t.Error("ColumnName(0) not empty")
	}
}

func TestLooksLikeCellRef(t *testing.T) {
	for s, want := range map[string]bool{
		"A1": true, "$A$1": true, "xfd1": true, "R": true, "c": true, "RC": true, "R1C1": true,
		"R[-1]C[2]": true, "r12": true, "C3": true, "XFE1": false, "Sheet1": false, "Rate": false,
		"RC1x": false, "R[1": false, "": false, "Total": false,
	} {
		if got := LooksLikeCellRef(s); got != want {
			t.Errorf("LooksLikeCellRef(%q) = %v", s, got)
		}
	}
}
