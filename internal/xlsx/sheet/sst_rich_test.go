package sheet

import "testing"

// TestSSTRich: an item with <r> runs is rich; a plain item with the same
// text is not (text.replace refuses rich cells, G4).
func TestSSTRich(t *testing.T) {
	sst, err := OpenSST(`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">` +
		`<si><t>ab</t></si><si><r><rPr><b/></rPr><t>a</t></r><r><t>b</t></r></si><si><t>x</t><rPh sb="0" eb="1"><t>y</t></rPh></si></sst>`)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{false, true, false} {
		got, err := sst.Rich(i)
		if err != nil || got != want {
			t.Errorf("Rich(%d) = %v %v, want %v", i, got, err, want)
		}
	}
	if s, _ := sst.Text(1); s != "ab" {
		t.Errorf("Text(1) = %q", s)
	}
	if _, err := sst.Rich(3); err == nil {
		t.Error("Rich(3) out of range: no error")
	}
}
