package formula

import (
	"errors"
	"strings"
	"testing"
)

var testOrder = []string{"Jan", "Feb", "Mar", "Other", "My Sheet"}

func TestShift(t *testing.T) {
	own := Sheets{Own: "Feb", Order: testOrder}
	ins := func(at, n int) Edit { return Edit{Sheet: "Feb", Kind: InsertRows, At: at, Count: n} }
	del := func(at, n int) Edit { return Edit{Sheet: "Feb", Kind: DeleteRows, At: at, Count: n} }
	insC := func(at, n int) Edit { return Edit{Sheet: "Feb", Kind: InsertCols, At: at, Count: n} }
	delC := func(at, n int) Edit { return Edit{Sheet: "Feb", Kind: DeleteCols, At: at, Count: n} }
	for _, tc := range []struct {
		src  string
		e    Edit
		want string
	}{
		{"A1+A5", ins(3, 2), "A1+A7"},
		{"$A$5*A$5", ins(5, 1), "$A$6*A$6"},
		{"SUM(A2:A10)", ins(5, 3), "SUM(A2:A13)"},
		{"SUM(A5:A10)", ins(5, 3), "SUM(A8:A13)"},
		{"SUM(A2:A4)", ins(5, 3), "SUM(A2:A4)"},
		{"A:A+3:4", ins(1, 1), "A:A+4:5"},
		{"A1:A1048576", ins(2, 1), "A1:A1048576"},
		{"feb!A5+Jan!A5", ins(1, 1), "feb!A6+Jan!A5"},
		{"'My Sheet'!A5", Edit{Sheet: "my sheet", Kind: InsertRows, At: 1, Count: 1}, "'My Sheet'!A6"},
		{"[1]Feb!A5", ins(1, 1), "[1]Feb!A5"},
		{"\"A5\"&Rate&T[Col]", ins(1, 1), "\"A5\"&Rate&T[Col]"},
		{"Jan:Mar!A1", ins(5, 1), "Jan:Mar!A1"},
		{"Jan:Feb!A9", Edit{Sheet: "Mar", Kind: InsertRows, At: 1, Count: 1}, "Jan:Feb!A9"},
		{"a5 + B5", ins(1, 1), "A6 + B6"},
		{"A5", del(5, 1), "#REF!"},
		{"Feb!A5:B6", del(5, 2), "Feb!#REF!"},
		{"A6", del(2, 3), "A3"},
		{"SUM(A2:A10)", del(5, 2), "SUM(A2:A8)"},
		{"SUM(A5:A10)", del(3, 4), "SUM(A3:A6)"},
		{"SUM(A2:A6)", del(5, 4), "SUM(A2:A4)"},
		{"SUM(A10:A2)", del(5, 2), "SUM(A8:A2)"},
		{"3:5", del(4, 1), "3:4"},
		{"A5:A1048576", del(1, 2), "A3:A1048576"},
		{"C1+A1", insC(2, 1), "D1+A1"},
		{"B:C", delC(2, 1), "B:B"},
		{"B1", delC(2, 1), "#REF!"},
		{"3:3", insC(1, 1), "3:3"},
		{"Sheet2!#REF!+A1", ins(1, 1), "Sheet2!#REF!+A2"},
	} {
		got, err := Shift(tc.src, own, tc.e)
		if err != nil || got != tc.want {
			t.Errorf("Shift(%q, %+v) = %q, %v; want %q", tc.src, tc.e, got, err, tc.want)
		}
	}
}

func TestShiftRefuses(t *testing.T) {
	for _, tc := range []struct {
		src    string
		sh     Sheets
		e      Edit
		reason string
	}{
		{"A1048576", Sheets{Own: "S"}, Edit{Sheet: "S", Kind: InsertRows, At: 1, Count: 1}, "past row"},
		{"XFD1", Sheets{Own: "S"}, Edit{Sheet: "S", Kind: InsertCols, At: 1, Count: 1}, "past column"},
		{"Jan:Mar!A5", Sheets{Own: "S", Order: testOrder}, Edit{Sheet: "Feb", Kind: InsertRows, At: 1, Count: 1}, "spans"},
		{"Jan:Nope!A5", Sheets{Own: "S", Order: testOrder}, Edit{Sheet: "Feb", Kind: InsertRows, At: 1, Count: 1}, "not in the workbook"},
		{"A5", Sheets{}, Edit{Sheet: "Feb", Kind: InsertRows, At: 1, Count: 1}, "no sheet"},
		{"A5", Sheets{Own: "S"}, Edit{Sheet: "S", Kind: InsertRows, At: 0, Count: 1}, "outside"},
		{"A5", Sheets{Own: "S"}, Edit{Sheet: "S", Kind: DeleteRows, At: MaxRows, Count: 2}, "outside"},
	} {
		_, err := Shift(tc.src, tc.sh, tc.e)
		var r *Refusal
		if !errors.As(err, &r) || !strings.Contains(r.Reason, tc.reason) {
			t.Errorf("Shift(%q, %+v) = %v, want a refusal with %q", tc.src, tc.e, err, tc.reason)
		}
	}
	// An unqualified reference the edit leaves alone needs no sheet.
	if got, err := Shift("A1+Feb!A1", Sheets{}, Edit{Sheet: "Feb", Kind: InsertRows, At: 5, Count: 1}); err != nil || got != "A1+Feb!A1" {
		t.Errorf("no-own-sheet unaffected: %q, %v", got, err)
	}
}

// TestShiftInverseLaw: an insert followed by its inverse is the identity
// on every reference, and a delete followed by its inverse is too when no
// reference touches the deleted lines.
func TestShiftInverseLaw(t *testing.T) {
	sh := Sheets{Own: "Feb", Order: testOrder}
	srcs := []string{
		"SUM(A1:C9)+B5*$D$7-Feb!E3:E8/'My Sheet'!A1", "2:4+B:B", "A1048576:B1048576", "Jan!A3+Mar!C9",
		"INDEX(A1:A1048576,5)", "XFD5+A5:XFD5", "{1,2}+T[#All]+\"A1\"",
	}
	for _, src := range srcs {
		for _, k := range []EditKind{InsertRows, InsertCols} {
			for _, at := range []int{1, 2, 3, 5, 9, 1000} {
				for _, n := range []int{1, 3} {
					e := Edit{Sheet: "Feb", Kind: k, At: at, Count: n}
					got, err := Shift(src, sh, e)
					if err != nil {
						continue // off the grid: refused, no inverse to check
					}
					back, err := Shift(got, sh, e.Inverse())
					if err != nil || back != src {
						t.Errorf("%q %+v: %q then %q, %v", src, e, got, back, err)
					}
				}
			}
		}
	}
	// Delete then insert where no reference meets the deleted lines.
	e := Edit{Sheet: "Feb", Kind: DeleteRows, At: 20, Count: 4}
	src := "SUM(A1:A10)+B30+C40:C50"
	got, err := Shift(src, sh, e)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := Shift(got, sh, e.Inverse()); err != nil || back != src {
		t.Errorf("delete inverse: %q then %q, %v", got, back, err)
	}
}

func TestRename(t *testing.T) {
	for _, tc := range []struct{ src, old, new, want string }{
		{"Feb!A1+feb!B2+A3", "Feb", "Qtr", "Qtr!A1+Qtr!B2+A3"},
		{"Feb!A1", "Feb", "My Sheet", "'My Sheet'!A1"},
		{"'My Sheet'!A1", "my sheet", "Plain", "Plain!A1"},
		{"Feb!A1", "Feb", "A1", "'A1'!A1"},
		{"Feb!A1", "Feb", "R1C1", "'R1C1'!A1"},
		{"Feb!A1", "Feb", "Bob's", "'Bob''s'!A1"},
		{"Jan:Feb!A1", "Feb", "Q 1", "'Jan:Q 1'!A1"},
		{"Feb!Rate+Feb!#REF!", "Feb", "Qtr", "Qtr!Rate+Qtr!#REF!"},
		{"[1]Feb!A1", "Feb", "Q1", "[1]Feb!A1"},
		{"INDIRECT(\"Feb!A1\")", "Feb", "Q1", "INDIRECT(\"Feb!A1\")"},
		{"Febr!A1", "Feb", "Q1", "Febr!A1"},
	} {
		if got := Rename(tc.src, tc.old, tc.new); got != tc.want {
			t.Errorf("Rename(%q, %q, %q) = %q, want %q", tc.src, tc.old, tc.new, got, tc.want)
		}
	}
	// Renaming there and back is the identity on canonical text.
	src := "Feb!A1+'My Sheet'!B2"
	if got := Rename(Rename(src, "Feb", "Q 1"), "Q 1", "Feb"); got != src {
		t.Errorf("rename there and back: %q", got)
	}
}

func TestRenameQuotesRefLikeNames(t *testing.T) {
	if got := Rename("Feb!A1", "Feb", "Q1"); got != "'Q1'!A1" {
		t.Errorf("Rename to a cell-like name = %q", got)
	}
}
