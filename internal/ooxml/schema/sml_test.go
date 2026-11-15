package schema

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSpreadsheetML(t *testing.T) {
	// The worksheet's head and tail elements are placed by this order.
	order, ok := ChildOrder("x:CT_Worksheet")
	if !ok {
		t.Fatal("x:CT_Worksheet not tabulated")
	}
	idx := func(name string) int {
		return slices.IndexFunc(order, func(s []string) bool { return slices.Contains(s, name) })
	}
	dim, cols, data, merge, ext := idx("x:dimension"), idx("x:cols"), idx("x:sheetData"), idx("x:mergeCells"), idx("x:extLst")
	if !(dim >= 0 && dim < cols && cols < data && data < merge && merge < ext && ext == len(order)-1) {
		t.Errorf("x:CT_Worksheet order: dimension %d, cols %d, sheetData %d, mergeCells %d, extLst %d of %d", dim, cols, data, merge, ext, len(order))
	}
	slots, _ := Slots("x:CT_Row")
	if k, c := Find(slots, "x", "c"); k < 0 || !slots[k].Multi || c.Type != "x:CT_Cell" {
		t.Errorf("x:CT_Row c: %d %+v", k, c)
	}
	// The workbook's calcPr, which the recalculation policy ensures.
	if typ, ok := ChildType("x:CT_Workbook", "x:calcPr"); !ok || typ != "x:CT_CalcPr" {
		t.Errorf("x:CT_Workbook calcPr: %q %v", typ, ok)
	}
	// Elements of schemas that are not tabulated keep their prefix.
	slots, _ = Slots("x:CT_ObjectAnchor")
	if k, c := Find(slots, "xdr", "from"); k < 0 || c.Type != "" {
		t.Errorf("x:CT_ObjectAnchor xdr:from: %d %+v", k, c)
	}

	m, ok := ContentModel("x:CT_Worksheet")
	if !ok || !slices.ContainsFunc(m.Children, func(p Particle) bool { return p.Local == "sheetData" && p.Required && p.Max == 1 }) {
		t.Errorf("x:CT_Worksheet model does not require one sheetData: %+v", m.Children)
	}
	cell, _ := ContentModel("x:CT_Cell")
	if !slices.Contains(cell.Attrs, ModelAttr{"", "r", "x:ST_CellRef", false}) {
		t.Errorf("x:CT_Cell attributes %+v", cell.Attrs)
	}
	for _, c := range []struct{ local, typ string }{
		{"worksheet", "x:CT_Worksheet"}, {"workbook", "x:CT_Workbook"},
		{"sst", "x:CT_Sst"}, {"styleSheet", "x:CT_Stylesheet"}, {"calcChain", "x:CT_CalcChain"},
	} {
		if typ, ok := RootType("x", c.local); !ok || typ != c.typ {
			t.Errorf("RootType(x, %s) = %q, %v; want %s", c.local, typ, ok, c.typ)
		}
	}
	// The prefixes dispatch: a wml root is not an sml one.
	if _, ok := RootType("x", "document"); ok {
		t.Error("RootType(x, document) found")
	}
	if _, ok := ContentModel("x:CT_P"); ok {
		t.Error("ContentModel(x:CT_P) found")
	}
	if _, ok := Untabulated("x:CT_Worksheet"); ok {
		t.Error("x:CT_Worksheet untabulated")
	}
}

func TestCheckAttrSpreadsheetML(t *testing.T) {
	for _, c := range []struct {
		ctype, attr, v string
		ok             bool
		why            string
	}{
		{"x:CT_Cell", "t", "inlineStr", true, ""},
		{"x:CT_Cell", "t", "string", false, "x:ST_CellType"},
		// A list type: each whitespace-separated item is checked.
		{"x:CT_Selection", "sqref", "A1 B2:C3", true, ""},
		{"x:CT_Selection", "sqref", "", true, ""},
		// An anonymous union member: 0 to 180, or 255.
		{"x:CT_CellAlignment", "textRotation", "90", true, ""},
		{"x:CT_CellAlignment", "textRotation", "255", true, ""},
		{"x:CT_CellAlignment", "textRotation", "200", false, "x:ST_TextRotation"},
		{"x:CT_Row", "r", "-1", false, "xsd:unsignedInt"},
	} {
		err := CheckAttr(c.ctype, c.attr, c.v)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s@%s=%q: %v", c.ctype, c.attr, c.v, err)
		case !c.ok && (!errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), c.why)):
			t.Errorf("%s@%s=%q: err = %v, want ErrSchema naming %q", c.ctype, c.attr, c.v, err, c.why)
		}
	}
	if st, ok := SimpleType("x:ST_Sqref"); !ok || st.List != "x:ST_Ref" {
		t.Errorf("x:ST_Sqref: %+v %v", st, ok)
	}
}

// TestGeneratedTables is the drift test of every generated table, sml's
// included: it runs gen_schema.go on the pinned zip named by
// OOXML_SCHEMA_ZIP into a temporary directory and requires each output to
// equal the committed file. It skips without the zip, which is not in git
// (schema.ZipURL; its SHA-256 is checked by the generator).
func TestGeneratedTables(t *testing.T) {
	zip := os.Getenv("OOXML_SCHEMA_ZIP")
	if zip == "" {
		t.Skip("OOXML_SCHEMA_ZIP is not set: the schema zip is not in git (" + ZipURL + ")")
	}
	out := t.TempDir()
	cmd := exec.Command("go", "run", "gen_schema.go", zip, out) //nolint:gosec // G702: the schema zip the developer named
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen_schema.go: %v\n%s", err, b)
	}
	files, err := filepath.Glob(filepath.Join(out, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"dml_table.go", "pml_table.go", "rel_table.go", "shared_table.go", "sml_model_table.go",
		"sml_table.go", "wml_model_table.go", "wml_table.go", "xml_table.go",
	}
	var got []string
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	if !slices.Equal(got, want) {
		t.Errorf("generated %v, want %v", got, want)
	}
	for _, name := range got {
		gen, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gen, committed) {
			t.Errorf("%s is stale: run go generate ./internal/ooxml/schema with the pinned zip", name)
		}
	}
}
