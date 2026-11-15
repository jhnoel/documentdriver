package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The styles shell over kernel K6 (designer rows; frozen).

func TestStylesIntern(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := oneSheet("")
		b.XFs = []int{0, 14}
		w := openBook(t, b)
		st, err := w.Styles()
		if err != nil {
			t.Fatal(err)
		}
		if st.Count() != 2 {
			t.Fatalf("count %d", st.Count())
		}
		if id, _, ok := st.NumFmt(1); !ok || id != 14 {
			t.Errorf("NumFmt(1) = %d", id)
		}
		// An identical entry is found, not appended.
		if xf, err := st.WithNumFmt(0, 14); err != nil || xf != 1 {
			t.Errorf("WithNumFmt(0, 14) = %d %v, want the existing 1", xf, err)
		}
		bold := true
		code := "0.000"
		a, err := st.WithFormat(0, CellFormat{Bold: &bold, NumberFormat: &code})
		if err != nil || a != 2 {
			t.Fatalf("WithFormat = %d %v", a, err)
		}
		if again, _ := st.WithFormat(0, CellFormat{Bold: &bold, NumberFormat: &code}); again != a {
			t.Errorf("not interned: %d then %d", a, again)
		}
		if id, c, _ := st.NumFmt(a); id < 164 || c != code {
			t.Errorf("custom format %d %q", id, c)
		}
		bad := "nope"
		if _, err := st.WithFormat(0, CellFormat{Align: &bad}); err == nil {
			t.Error("a bad alignment was written")
		}
		doc, _ := w.SmallTree("/xl/styles.xml")
		if s := string(doc.Bytes()); !strings.Contains(s, `<cellXfs count="3">`) {
			t.Errorf("styles: %s", s)
		}
	})
}
