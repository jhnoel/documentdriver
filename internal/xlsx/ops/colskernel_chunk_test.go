package ops

import "testing"

func TestSetColumnsChunk(t *testing.T) {
	h := false
	s := 0
	src := `<x:cols><x:col min='1' max='4' hidden="1" style="3" extra="a&gt;b"/></x:cols>`
	got, err := SetColumns(src, "x:", 2, 3, nil, &h, &s)
	// Q2 byte rule (EditTag): a changed attribute keeps its quote and
	// spacing; only its value bytes change.
	want := `<x:cols><x:col min='1' max='1' hidden="1" style="3" extra="a&gt;b"/><x:col min='2' max='3' extra="a&gt;b"/><x:col min='4' max='4' hidden="1" style="3" extra="a&gt;b"/></x:cols>`
	if err != nil || got != want {
		t.Fatalf("got %s, %v; want %s", got, err, want)
	}
	for _, bad := range []string{`<cols><col min="x" max="2"/></cols>`, `<cols><col min="2" max="3"/><col min="3" max="4"/></cols>`} {
		if _, err := SetColumns(bad, "", 1, 2, nil, nil, nil); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// TestSetColumnsChunkBytes pins the EditTag byte rules the cols kernel
// keeps (Q2): a changed attribute keeps its spacing and quote, and a
// new attribute is inserted right after the last attribute's closing
// quote, with whitespace before "/>" staying after it.
func TestSetColumnsChunkBytes(t *testing.T) {
	w20 := 20.0
	got, err := SetColumns(`<cols><col  min='1'   max = "3"  width='9'/></cols>`, "", 2, 2, &w20, nil, nil)
	want := `<cols><col  min='1'   max = "1"  width='9'/><col  min='2'   max = "2"  width='20' customWidth="1"/><col  min='3'   max = "3"  width='9'/></cols>`
	if err != nil || got != want {
		t.Fatalf("spacing kept: got %s, %v; want %s", got, err, want)
	}
	got, err = SetColumns(`<cols><col min="1" max="4"  /></cols>`, "", 2, 2, &w20, nil, nil)
	want = `<cols><col min="1" max="1"  /><col min="2" max="2" width="20" customWidth="1"  /><col min="3" max="4"  /></cols>`
	if err != nil || got != want {
		t.Fatalf("insertion point: got %s, %v; want %s", got, err, want)
	}
}
