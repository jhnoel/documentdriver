package read

import (
	"fmt"
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// fixtureBytes returns the bytes of the vsdx fixture name.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(vsdxtest.Fixture(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestDocumentV1Bytes checks the V1 row: the summary marshalled by
// cli.Marshal is byte for byte
// {"base":"B","pages":[{"id":"page0","name":"Page-1","size":[612,792],"orientation":"portrait","print_orientation":"portrait","objects":1}],"masters":[],"notes":[]}.
func TestDocumentV1Bytes(t *testing.T) {
	sum, err := Document(fixtureBytes(t, "v1-minimal"), "B")
	if err != nil {
		t.Fatalf("Document(v1-minimal): %v", err)
	}
	b, err := cli.Marshal(sum)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"base":"B","pages":[{"id":"page0","name":"Page-1","size":[612,792],"orientation":"portrait","print_orientation":"portrait","objects":1}],"masters":[],"notes":[]}`
	if string(b) != want {
		t.Errorf("Marshal = %s, want %s", b, want)
	}
}

// TestDocumentV2 checks the V2 row: pages[0].objects is 5 and the
// masters marshal to
// [{"id":"master2","name":"Process","objects":1},{"id":"master3","name":"Pair","objects":3}].
func TestDocumentV2(t *testing.T) {
	sum, err := Document(fixtureBytes(t, "v2-master"), "B")
	if err != nil {
		t.Fatalf("Document(v2-master): %v", err)
	}
	if len(sum.Pages) != 1 {
		t.Fatalf("Pages = %v, want one entry", sum.Pages)
	}
	if sum.Pages[0].Objects != 5 {
		t.Errorf("Pages[0].Objects = %d, want 5", sum.Pages[0].Objects)
	}
	b, err := cli.Marshal(sum.Masters)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `[{"id":"master2","name":"Process","objects":1},{"id":"master3","name":"Pair","objects":3}]`
	if string(b) != want {
		t.Errorf("Marshal(masters) = %s, want %s", b, want)
	}
}

// TestDocumentV8 checks the V8 row: pages in order page0, page1,
// page3, with page0's background_page "page3" and page3's background
// flag set.
func TestDocumentV8(t *testing.T) {
	sum, err := Document(fixtureBytes(t, "v8-pages"), "B")
	if err != nil {
		t.Fatalf("Document(v8-pages): %v", err)
	}
	wantIDs := []string{"page0", "page1", "page3"}
	if len(sum.Pages) != len(wantIDs) {
		t.Fatalf("Pages = %v, want %v", sum.Pages, wantIDs)
	}
	for i, want := range wantIDs {
		if got := sum.Pages[i].ID; got != want {
			t.Errorf("Pages[%d].ID = %q, want %q", i, got, want)
		}
	}
	if p := sum.Pages[0]; p.Background {
		t.Error("page0 Background = true, want false")
	}
	if p := sum.Pages[0]; p.BackPage != "page3" {
		t.Errorf("page0 background_page = %q, want page3", p.BackPage)
	}
	if p := sum.Pages[2]; !p.Background {
		t.Error("page3 Background = false, want true")
	}
	if p := sum.Pages[2]; p.BackPage != "" {
		t.Errorf("page3 background_page = %q, want empty", p.BackPage)
	}
	wantObjects := []int{1, 0, 1}
	for i, want := range wantObjects {
		if got := sum.Pages[i].Objects; got != want {
			t.Errorf("Pages[%d].Objects = %d, want %d", i, got, want)
		}
	}
}

// TestDocumentV10 checks the V10 row: the pages end with page5 and
// page5~2, and the notes cover page2 and page4.
func TestDocumentV10(t *testing.T) {
	sum, err := Document(fixtureBytes(t, "v10-defects"), "B")
	if err != nil {
		t.Fatalf("Document(v10-defects): %v", err)
	}
	wantIDs := []string{"page0", "page2", "page4", "page5", "page5~2"}
	if len(sum.Pages) != len(wantIDs) {
		t.Fatalf("Pages = %v, want %v", sum.Pages, wantIDs)
	}
	for i, want := range wantIDs {
		if got := sum.Pages[i].ID; got != want {
			t.Errorf("Pages[%d].ID = %q, want %q", i, got, want)
		}
	}
	for _, at := range []string{"page2", "page4"} {
		found := false
		for _, n := range sum.Notes {
			if n.At == at {
				found = true
			}
		}
		if !found {
			t.Errorf("Notes = %v, want a note at %s", sum.Notes, at)
		}
	}
	if got := sum.Pages[1].Objects; got != 0 {
		t.Errorf("page2 Objects = %d, want 0 (unreadable)", got)
	}
	if got := sum.Pages[2].Objects; got != 0 {
		t.Errorf("page4 Objects = %d, want 0 (no part)", got)
	}
}

// TestDocumentV17 checks the V17 row: the orientations and sizes are
// as M6 names them (page3 square: no orientation, portrait print).
func TestDocumentV17(t *testing.T) {
	sum, err := Document(fixtureBytes(t, "v17-orientation"), "B")
	if err != nil {
		t.Fatalf("Document(v17-orientation): %v", err)
	}
	want := []struct {
		id          string
		orientation string
		print       string
		size        [2]string
	}{
		{id: "page0", orientation: "portrait", print: "portrait", size: [2]string{"612", "792"}},
		{id: "page1", orientation: "landscape", print: "landscape", size: [2]string{"792", "612"}},
		{id: "page2", orientation: "landscape", print: "portrait", size: [2]string{"792", "612"}},
		{id: "page3", orientation: "", print: "portrait", size: [2]string{"576", "576"}},
	}
	if len(sum.Pages) != len(want) {
		t.Fatalf("Pages = %v, want %d entries", sum.Pages, len(want))
	}
	for i, tc := range want {
		p := sum.Pages[i]
		if p.ID != tc.id {
			t.Errorf("Pages[%d].ID = %q, want %q", i, p.ID, tc.id)
		}
		if p.Orientation != tc.orientation {
			t.Errorf("%s Orientation = %q, want %q", tc.id, p.Orientation, tc.orientation)
		}
		if p.PrintOrientation != tc.print {
			t.Errorf("%s PrintOrientation = %q, want %q", tc.id, p.PrintOrientation, tc.print)
		}
		if len(p.Size) != 2 || string(p.Size[0]) != tc.size[0] || string(p.Size[1]) != tc.size[1] {
			t.Errorf("%s Size = %v, want [%s %s]", tc.id, p.Size, tc.size[0], tc.size[1])
		}
	}
}

// TestDocumentNonZip checks the non-zip row: input that is not a zip
// is an error.
func TestDocumentNonZip(t *testing.T) {
	for _, in := range [][]byte{[]byte("not a zip"), {}} {
		if sum, err := Document(in, "B"); err == nil || sum != nil {
			t.Errorf("Document(%q) = %v, %v, want nil, an error", in, sum, err)
		}
	}
}

// TestDocumentAllocationGenerated4000 checks the allocation row:
// allocating Generated(4000) costs less than 3 times its page XML.
func TestDocumentAllocationGenerated4000(t *testing.T) {
	if laws.RaceEnabled {
		t.Skip("memory is not measured under the race detector")
	}
	path := vsdxtest.Generated(t, 4000)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Generated(4000): %v", err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatalf("opc.Open(Generated(4000)): %v", err)
	}
	pt, err := pkg.Part("/visio/pages/page1.xml")
	if err != nil {
		t.Fatalf("Part(page1.xml): %v", err)
	}
	pageXML, ok, err := pt.OriginalString()
	if err != nil {
		t.Fatalf("OriginalString(page1.xml): %v", err)
	}
	if !ok {
		t.Fatal("page1.xml has no original bytes")
	}
	if err := pkg.Close(); err != nil {
		t.Fatalf("close Generated(4000): %v", err)
	}
	cost, err := laws.Measure(1, func() error {
		sum, err := Document(data, "B")
		if err != nil {
			return err
		}
		if len(sum.Pages) != 1 || sum.Pages[0].Objects != 6000 {
			return fmt.Errorf("Pages = %v, want one page with 6000 objects", sum.Pages)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Document(Generated(4000)): %v", err)
	}
	t.Logf("page XML %d bytes, allocated %d bytes (budget %d)", len(pageXML), cost.Bytes, 3*len(pageXML))
	if cost.Bytes >= uint64(3*len(pageXML)) {
		t.Errorf("allocated %d bytes for Generated(4000), want less than 3 times its page XML %d", cost.Bytes, len(pageXML))
	}
}

// TestScalingDocument checks the scaling row: Document on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000) allocates
// at most 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio
// is at most 8.
func TestScalingDocument(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			sum, err := Document(data, "B")
			if err != nil {
				return err
			}
			if len(sum.Pages) != 1 || sum.Pages[0].Objects <= 0 {
				return fmt.Errorf("Generated(%d): Pages = %v", n, sum.Pages)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Document", setup)
	laws.TimeScales(t, "vsdx Document", setup)
}
