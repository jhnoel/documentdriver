package formula

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K4b: StoreAt (designer rows; frozen).
func TestStoreAt(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct{ in, want string }{
			{"@A1:A3", "_xlfn.SINGLE(A1:A3)"},
			{"1+@Rates", "1+_xlfn.SINGLE(Rates)"},
			{"@INDEX(A:A,2)", "_xlfn.SINGLE(INDEX(A:A,2))"},
			{"Tbl[@Col]", "Tbl[[#This Row],[Col]]"},
			{"[@[Col 2]]", "[[#This Row],[Col 2]]"},
			{"Tbl[@]", "Tbl[#This Row]"},
			{`"@x"&A1`, `"@x"&A1`},
		} {
			got, err := StoreAt(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("StoreAt(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		}
		if _, err := StoreAt("1+@"); err == nil {
			t.Error("a bare @ was accepted")
		}
	})
}
