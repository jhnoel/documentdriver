package read

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

// TestLocateV1 checks the locate row for V1: page0 is the only page,
// at PDF page index 0, 612 x 792 pt.
func TestLocateV1(t *testing.T) {
	info, err := Locate(fixtureBytes(t, "v1-minimal"), "page0")
	if err != nil {
		t.Fatalf("Locate(page0): %v", err)
	}
	if info.Page != "page0" || info.Index != 0 || info.Count != 1 || info.Background {
		t.Errorf("Locate = %+v, want page0 index 0 of 1, not background", info)
	}
	if info.W != 612 || info.H != 792 {
		t.Errorf("W, H = %v, %v, want 612, 792", info.W, info.H)
	}
}

// TestLocateV8 checks the RN2 V8 row without LibreOffice: page0,
// page1 and the background page3 are at PDF page indexes 0, 1 and 2 of
// 3 in pages.xml order, and page3 is the background page. The render
// test measures that LibreOffice exports them in this order.
func TestLocateV8(t *testing.T) {
	input := fixtureBytes(t, "v8-pages")
	for _, tc := range []struct {
		page       string
		index      int
		background bool
	}{
		{"page0", 0, false},
		{"page1", 1, false},
		{"page3", 2, true},
	} {
		info, err := Locate(input, tc.page)
		if err != nil {
			t.Fatalf("Locate(%s): %v", tc.page, err)
		}
		if info.Index != tc.index || info.Count != 3 || info.Background != tc.background {
			t.Errorf("Locate(%s) = index %d of %d background %v, want %d of 3 %v",
				tc.page, info.Index, info.Count, info.Background, tc.index, tc.background)
		}
	}
}

// TestLocateDuplicate checks a second page sharing an ID: page5~2 is
// at stored index 4 of V10's 5 pages.
func TestLocateDuplicate(t *testing.T) {
	info, err := Locate(fixtureBytes(t, "v10-defects"), "page5~2")
	if err != nil {
		t.Fatalf("Locate(page5~2): %v", err)
	}
	if info.Index != 4 || info.Count != 5 {
		t.Errorf("Locate(page5~2) = index %d of %d, want 4 of 5", info.Index, info.Count)
	}
}

// TestLocateNotFound checks the RN2 not-found row: page7, a master
// address and a non-address are not_found at --page.
func TestLocateNotFound(t *testing.T) {
	input := fixtureBytes(t, "v1-minimal")
	for _, address := range []string{"page7", "master2", "bogus", "page0/s1"} {
		_, err := Locate(input, address)
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeNotFound || e.Path != "--page" {
			t.Errorf("Locate(%s) error = %v, want not_found at --page", address, err)
		}
	}
}
