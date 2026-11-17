package schema

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// TestVisioChildOrder checks the designer rows for the generated
// Visio table: Shape's full order (row 3), PageSheet's leading
// interleaved Cell/Trigger slot then Section (row 4), and the
// PageContents, Section and Text orders (row 5).
func TestVisioChildOrder(t *testing.T) {
	if order, ok := ChildOrder("v:Shape"); !ok || !equalSlots(order, [][]string{
		{"v:Cell"}, {"v:Section"}, {"v:Text"}, {"v:ForeignData"},
		{"v:Data1", "v:Data2", "v:Data3"}, {"v:Shapes"},
	}) {
		t.Errorf("ChildOrder(v:Shape) = %v, %v; want [[Cell] [Section] [Text] [ForeignData] [Data1 Data2 Data3] [Shapes]]", order, ok)
	}
	order, ok := ChildOrder("v:PageSheet")
	if !ok || len(order) < 2 || !equalSlots(order[:2], [][]string{
		{"v:Cell", "v:Trigger"}, {"v:Section"},
	}) {
		t.Errorf("ChildOrder(v:PageSheet) = %v, %v; want first slots [[Cell Trigger] [Section]]", order, ok)
	}
	for ctype, want := range map[string][][]string{
		"v:PageContents": {{"v:Shapes"}, {"v:Connects"}},
		"v:Section":      {{"v:Cell"}, {"v:Row"}},
		"v:Text":         {{"v:cp", "v:pp", "v:tp", "v:fld"}},
	} {
		if order, ok := ChildOrder(ctype); !ok || !equalSlots(order, want) {
			t.Errorf("ChildOrder(%s) = %v, %v; want %v", ctype, order, ok, want)
		}
	}
}

// TestVisioRootsAndTypes checks row 6: the 12 roots, ChildType as
// v:<child>, and that an unknown element has no order.
func TestVisioRootsAndTypes(t *testing.T) {
	if typ, ok := RootType("v", "PageContents"); !ok || typ != "v:PageContents" {
		t.Errorf("RootType(v, PageContents) = %q, %v; want v:PageContents", typ, ok)
	}
	if typ, ok := ChildType("v:Shape", "Shapes"); !ok || typ != "v:Shapes" {
		t.Errorf("ChildType(v:Shape, Shapes) = %q, %v; want v:Shapes", typ, ok)
	}
	if _, ok := ChildOrder("v:Nope"); ok {
		t.Error("ChildOrder(v:Nope) found")
	}
	for _, root := range []string{"VisioDocument", "SolutionXML", "Masters", "MasterContents", "Pages", "PageContents", "Windows", "Solutions", "DataConnections", "DataRecordSets", "Validation", "Extensions"} {
		if typ, ok := RootType("v", root); !ok || typ != "v:"+root {
			t.Errorf("RootType(v, %s) = %q, %v; want v:%s", root, typ, ok, root)
		}
	}
}

// TestVisioProvenance checks the committed provenance record (rows 1
// and 2): exactly two disagreements (PageSheet>Trigger through the
// wildcard, in 32 files holding 33 PageSheet elements and 36
// Trigger elements; and the undeclared Trigger element, whose
// RefBy children come from the corpus with 36 occurrences) and
// exactly 52 refinements including the named pairs. The designer's
// row calls the 33 parent elements "files"; the record keeps both
// the distinct-file count (32) and the parent-element count (33).
func TestVisioProvenance(t *testing.T) {
	b, err := os.ReadFile("v_provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		XSD  string   `json:"xsd_sha256"`
		Scan []string `json:"scanned_files"`
		Acc  []string `json:"accepted_files"`
		Dis  []struct {
			Parent   string   `json:"parent"`
			Child    string   `json:"child"`
			Kind     string   `json:"kind"`
			Files    int      `json:"files"`
			Parents  int      `json:"parent_occurrences"`
			Occurs   int      `json:"occurrences"`
			Children []string `json:"children"`
		} `json:"disagreements"`
		Refs []struct {
			Parent string `json:"parent"`
			First  string `json:"first"`
			Second string `json:"second"`
			Before int    `json:"before"`
			After  int    `json:"after"`
		} `json:"refinements"`
		Wildcards []string `json:"wildcards"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.XSD != "29caea2b747dcc560cca0b112de30e3cbefe751ccd0e874618ff7dfb3ba0db76" {
		t.Errorf("provenance xsd sha256 = %q", p.XSD)
	}
	if len(p.Scan) != 65 || len(p.Acc) != 53 {
		t.Errorf("scanned %d and accepted %d files; want 65 and 53", len(p.Scan), len(p.Acc))
	}
	if len(p.Dis) != 2 {
		t.Fatalf("disagreements = %d, want exactly 2", len(p.Dis))
	}
	for _, d := range p.Dis {
		switch d.Kind {
		case "wildcard_only":
			if d.Parent != "v:PageSheet" || d.Child != "Trigger" || d.Occurs != 36 || d.Parents != 33 || d.Files != 32 {
				t.Errorf("wildcard disagreement = %+v", d)
			}
		case "undeclared":
			if d.Child != "Trigger" || d.Occurs != 36 || !slices.Equal(d.Children, []string{"RefBy"}) {
				t.Errorf("undeclared disagreement = %+v", d)
			}
		default:
			t.Errorf("disagreement kind = %q", d.Kind)
		}
	}
	if len(p.Refs) != 52 {
		t.Fatalf("refinements = %d, want exactly 52", len(p.Refs))
	}
	docSettings := 0
	for _, r := range p.Refs {
		if r.Parent == "v:DocumentSettings" {
			docSettings++
		}
	}
	if docSettings != 36 {
		t.Errorf("DocumentSettings pairs = %d, want 36", docSettings)
	}
	for name := range map[string]bool{"v:VisioDocument": true, "v:SolutionXML": true, "v:Shape": true, "v:StyleSheet": true, "v:PageSheet": true, "v:DocumentSheet": true} {
		if !slices.Contains(p.Wildcards, name) {
			t.Errorf("wildcards lack %s: %v", name, p.Wildcards)
		}
	}
}

// equalSlots reports whether a and b hold the same slots.
func equalSlots(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
