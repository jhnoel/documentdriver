package ctypes

import (
	"slices"
	"strings"
	"testing"
)

func TestTablesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tbl := range [][]Entry{ContentTypes(), RelTypes()} {
		if len(tbl) == 0 {
			t.Fatal("empty table")
		}
		for _, e := range tbl {
			if e.Name == "" || e.Value == "" || e.Source == "" || len(e.SHA256) != 64 {
				t.Errorf("incomplete entry %+v", e)
			}
			if e.Word+e.PowerPoint+e.Excel != e.Files {
				t.Errorf("%s: Word+PowerPoint+Excel != Files", e.Name)
			}
			if seen[e.Name] {
				t.Errorf("duplicate name %s", e.Name)
			}
			seen[e.Name] = true
		}
	}
	known := map[string]bool{}
	for _, e := range append(ContentTypes(), RelTypes()...) {
		known[e.Value] = true
	}
	for _, tg := range Targets() {
		if !known[tg.Rel] || !known[tg.ContentType] {
			t.Errorf("target %s -> %s names a value missing from the tables", tg.Rel, tg.ContentType)
		}
		if tg.Word+tg.PowerPoint+tg.Excel != tg.Files {
			t.Errorf("target %s -> %s: Word+PowerPoint+Excel != Files", tg.Rel, tg.ContentType)
		}

	}
}

func TestExpectedContentTypes(t *testing.T) {
	got := ExpectedContentTypes(RelSlideLayout)
	if len(got) != 1 || got[0] != CTPresentationmlSlideLayout {
		t.Errorf("slideLayout targets = %v", got)
	}
	if ExpectedContentTypes("urn:nonsense") != nil {
		t.Error("unknown relationship type has expectations")
	}
}

func TestIsAndRebase(t *testing.T) {
	strict := "http://purl.oclc.org/ooxml/officeDocument/relationships/slideLayout"
	if !Is(RelSlideLayout, RelSlideLayout) || !Is(strict, RelSlideLayout) {
		t.Error("Is must accept transitional and strict")
	}
	if Is(RelSlide, RelSlideLayout) || Is(RelSlideLayout, RelSlide) {
		t.Error("slide and slideLayout are different types")
	}
	got, ok := Rebase(strict, RelImage)
	if !ok || got != "http://purl.oclc.org/ooxml/officeDocument/relationships/image" {
		t.Errorf("Rebase = %q, %v", got, ok)
	}
	if !strings.HasPrefix(RelImage, "http://") {
		t.Error("RelImage is not a URI")
	}
}

// TestExpectedContentTypesWordPowerPoint: evidence from Excel files alone
// never widens what docx and pptx accept. The styles relationship targets
// a spreadsheetml styles part in workbooks, which a Word package must not
// be allowed.
func TestExpectedContentTypesWordPowerPoint(t *testing.T) {
	got := ExpectedContentTypes(RelStyles)
	if !slices.Contains(got, CTWordprocessingmlStyles) || slices.Contains(got, CTSpreadsheetmlStyles) {
		t.Errorf("ExpectedContentTypes(styles) = %v", got)
	}
	if got := ExpectedContentTypes(RelWorksheet); got != nil {
		t.Errorf("ExpectedContentTypes(worksheet) = %v, want nil (Excel-only evidence)", got)
	}
}
