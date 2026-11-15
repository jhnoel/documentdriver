package sheet

import "testing"

func TestBounds(t *testing.T) {
	var b Bounds
	if b.OK {
		t.Fatal("zero bounds is not empty")
	}
	b.Add(5, 7)
	b.Add(2, 9)
	b.Add(8, 3)
	if want := (Bounds{2, 3, 8, 9, true}); b != want {
		t.Errorf("got %+v, want %+v", b, want)
	}
}
