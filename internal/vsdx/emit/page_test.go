package emit

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/repo"
)

// readGolden reads the designer-authored golden or member at
// testdata/vsdx/<name> from the repository root.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "testdata", "vsdx", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return got
}

// TestPageXML checks the K6 rows: the wide page equals page-wide.xml,
// the blank page equals the Page element of the blank pages.xml, and
// the name is escaped for a single-quoted attribute.
func TestPageXML(t *testing.T) {
	for _, tc := range []struct {
		name string
		page Page
	}{
		{name: "wide", page: Page{ID: 1, Name: "Wide", Width: 17, Height: 11, Orientation: 2, RelID: "rId2"}},
		{name: "blank", page: Page{ID: 0, Name: "Page-1", Width: 8.5, Height: 11, Orientation: 0, RelID: "rId1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PageXML(tc.page)
			if err != nil {
				t.Fatalf("PageXML(%+v): %v", tc.page, err)
			}
			var want []byte
			if tc.name == "wide" {
				want = readGolden(t, "create/emit/page-wide.xml")
			} else {
				pages := string(readGolden(t, "create/blank/visio/pages/pages.xml"))
				start := strings.Index(pages, "<Page ")
				end := strings.Index(pages, "</Page>")
				if start < 0 || end < 0 {
					t.Fatalf("blank pages.xml holds no Page element: %q", pages)
				}
				want = []byte(pages[start : end+len("</Page>")])
			}
			if !bytes.Equal(got, want) {
				t.Errorf("PageXML(%+v) =\n%s\nwant\n%s", tc.page, got, want)
			}
		})
	}

	t.Run("name-escaped", func(t *testing.T) {
		got, err := PageXML(Page{ID: 1, Name: "A&B's", Width: 8.5, Height: 11, RelID: "rId1"})
		if err != nil {
			t.Fatalf("PageXML: %v", err)
		}
		for _, want := range []string{"NameU='A&amp;B&apos;s'", "Name='A&amp;B&apos;s'"} {
			if !strings.Contains(string(got), want) {
				t.Errorf("PageXML with name %q = %s, want attribute %s", "A&B's", got, want)
			}
		}
	})
}

// TestPageXMLBad checks the refusals the bounds guard: an orientation
// outside {0, 1, 2}, a non-finite size and a non-positive size.
func TestPageXMLBad(t *testing.T) {
	for _, tc := range []struct {
		name string
		page Page
	}{
		{name: "orientation-3", page: Page{ID: 1, Name: "X", Width: 8.5, Height: 11, Orientation: 3, RelID: "rId1"}},
		{name: "orientation-negative", page: Page{ID: 1, Name: "X", Width: 8.5, Height: 11, Orientation: -1, RelID: "rId1"}},
		{name: "width-nan", page: Page{ID: 1, Name: "X", Width: math.NaN(), Height: 11, RelID: "rId1"}},
		{name: "height-inf", page: Page{ID: 1, Name: "X", Width: 8.5, Height: math.Inf(1), RelID: "rId1"}},
		{name: "width-zero", page: Page{ID: 1, Name: "X", Width: 0, Height: 11, RelID: "rId1"}},
		{name: "height-zero", page: Page{ID: 1, Name: "X", Width: 8.5, Height: 0, RelID: "rId1"}},
		{name: "width-negative", page: Page{ID: 1, Name: "X", Width: -1, Height: 11, RelID: "rId1"}},
		{name: "height-negative", page: Page{ID: 1, Name: "X", Width: 8.5, Height: -8.5, RelID: "rId1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := PageXML(tc.page); err == nil {
				t.Errorf("PageXML(%+v) = %s, nil error; want refusal", tc.page, got)
			}
		})
	}
}

// TestPageXMLBadSizeNamesSize checks the K6F rows: a non-positive size
// is refused with an error naming that size.
func TestPageXMLBadSizeNamesSize(t *testing.T) {
	for _, tc := range []struct {
		name string
		page Page
		want string
	}{
		{name: "width-zero", page: Page{ID: 1, Name: "X", Width: 0, Height: 11, RelID: "rId1"}, want: "page size (0, 11) inches"},
		{name: "height-zero", page: Page{ID: 1, Name: "X", Width: 8.5, Height: 0, RelID: "rId1"}, want: "page size (8.5, 0) inches"},
		{name: "width-negative", page: Page{ID: 1, Name: "X", Width: -1, Height: 11, RelID: "rId1"}, want: "page size (-1, 11) inches"},
		{name: "height-negative", page: Page{ID: 1, Name: "X", Width: 8.5, Height: -8.5, RelID: "rId1"}, want: "page size (8.5, -8.5) inches"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := PageXML(tc.page); err == nil {
				t.Errorf("PageXML(%+v) = %s, nil error; want refusal naming %s", tc.page, got, tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("PageXML(%+v) error = %q, want it to name %s", tc.page, err, tc.want)
			}
		})
	}
}

// TestPageContentsXML checks the K6 row: the empty page part is the
// blank template's page1.xml byte for byte.
func TestPageContentsXML(t *testing.T) {
	want := readGolden(t, "create/blank/visio/pages/page1.xml")
	if got := PageContentsXML(); !bytes.Equal(got, want) {
		t.Errorf("PageContentsXML() =\n%s\nwant\n%s", got, want)
	}
}
