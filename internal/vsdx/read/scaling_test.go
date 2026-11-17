package read

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// shapesPerPage is the shape count of each page of a multi-page
// drawing: 4 pages hold laws.Small shapes and 16 pages hold
// laws.Large, so find over pages scales with the page count.
const shapesPerPage = 250

// memoryShapes is the shape count of the memory budget's page.
const memoryShapes = 10000

// multiPageBytes builds a drawing of pages pages, each a copy of a
// Generated(shapesPerPage) page, and returns its bytes. It reads
// the one-page drawing's members with opc and writes a fresh
// package with opc.BuildZip: pages.xml, its relationships and the
// content types cover every page, and each page part is a copy of
// the drawing's own page part and page sheet.
func multiPageBytes(t *testing.T, pages int) []byte {
	t.Helper()
	if pages < 1 {
		t.Fatalf("multiPageBytes(%d): at least one page", pages)
	}
	pkg, err := opc.Open(vsdxtest.Generated(t, shapesPerPage))
	if err != nil {
		t.Fatalf("opc.Open(Generated(%d)): %v", shapesPerPage, err)
	}
	members := map[string]string{}
	for _, name := range pkg.Names() {
		pt, err := pkg.Part(name)
		if err != nil {
			t.Fatalf("Part(%s): %v", name, err)
		}
		b, err := pt.Original()
		if err != nil {
			t.Fatalf("Original(%s): %v", name, err)
		}
		members[strings.TrimPrefix(name, "/")] = string(b)
	}
	if err := pkg.Close(); err != nil {
		t.Fatalf("close Generated(%d): %v", shapesPerPage, err)
	}
	pageXML := members["visio/pages/page1.xml"]
	sheet, ok := pageSheetOf(members["visio/pages/pages.xml"])
	if !ok {
		t.Fatal("Generated pages.xml has no PageSheet element")
	}
	var pagesXML, rels, overrides strings.Builder
	fmt.Fprintf(&pagesXML, "<?xml version='1.0' encoding='utf-8' ?>\n<Pages xmlns='%s' xmlns:r='%s' xml:space='preserve'>", vsd.NSMain2012, vsd.NSRel)
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range pages {
		fmt.Fprintf(&pagesXML, "<Page ID='%d' NameU='Page-%d' Name='Page-%d' ViewScale='-1' ViewCenterX='4.25' ViewCenterY='5.5'>%s<Rel r:id='rId%d'/></Page>",
			i, i+1, i+1, sheet, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s" Target="page%d.xml"/>`, i+1, ctypes.RelPage, i+1)
		if i > 0 {
			fmt.Fprintf(&overrides, `<Override PartName="/visio/pages/page%d.xml" ContentType="%s"/>`, i+1, ctypes.CTMsVisioPage)
		}
		members[fmt.Sprintf("visio/pages/page%d.xml", i+1)] = pageXML
	}
	pagesXML.WriteString("</Pages>")
	rels.WriteString("</Relationships>")
	members["visio/pages/pages.xml"] = pagesXML.String()
	members["visio/pages/_rels/pages.xml.rels"] = rels.String()
	ct := members["[Content_Types].xml"]
	if !strings.Contains(ct, "</Types>") {
		t.Fatal("[Content_Types].xml has no Types element")
	}
	members["[Content_Types].xml"] = strings.Replace(ct, "</Types>", overrides.String()+"</Types>", 1)
	names := slices.Sorted(maps.Keys(members))
	zipMembers := make([]opc.ZipMember, 0, len(names))
	if body, ok := members["[Content_Types].xml"]; ok {
		zipMembers = append(zipMembers, opc.ZipMember{Name: "[Content_Types].xml", Body: body})
	}
	for _, name := range names {
		if name == "[Content_Types].xml" {
			continue
		}
		zipMembers = append(zipMembers, opc.ZipMember{Name: name, Body: members[name]})
	}
	data, err := opc.BuildZip(zipMembers...)
	if err != nil {
		t.Fatalf("opc.BuildZip: %v", err)
	}
	return data
}

// pageSheetOf returns the PageSheet element (start tag to end tag)
// of a pages.xml part, and false when it has none.
func pageSheetOf(pages string) (string, bool) {
	start := strings.Index(pages, "<PageSheet")
	end := strings.Index(pages, "</PageSheet>")
	if start < 0 || end < 0 {
		return "", false
	}
	return pages[start : end+len("</PageSheet>")], true
}

// TestScalingReadPage checks the scaling row: Page on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000 shapes)
// allocates at most 5.5 times as much (laws.Scales) and the
// laws.TimeScales time ratio is at most 8.
func TestScalingReadPage(t *testing.T) {
	setup := func(n int) (func() error, error) {
		data, err := os.ReadFile(vsdxtest.Generated(t, n))
		if err != nil {
			return nil, err
		}
		return func() error {
			got, err := Page(data, "B", "page0", Options{})
			if err != nil {
				return err
			}
			if !strings.Contains(string(got), `"page":"page0"`) {
				return fmt.Errorf("Generated(%d): output is not page0", n)
			}
			if last := fmt.Sprintf(`"page0/s%d"`, n+n/2); !strings.Contains(string(got), last) {
				return fmt.Errorf("Generated(%d): output has no %s", n, last)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Page shapes", setup)
	laws.TimeScales(t, "vsdx Page shapes", setup)
}

// TestScalingFindPages checks the scaling row: Find over 4 and 16
// pages (1000 and 4000 shapes at shapesPerPage a page) allocates at
// most 5.5 times as much (laws.Scales) and the laws.TimeScales time ratio
// is at most 8.
func TestScalingFindPages(t *testing.T) {
	setup := func(n int) (func() error, error) {
		if n%shapesPerPage != 0 {
			return nil, fmt.Errorf("setup at %d: not a multiple of %d shapes a page", n, shapesPerPage)
		}
		data := multiPageBytes(t, n/shapesPerPage)
		return func() error {
			res, err := Find(data, "B", FindRequest{Query: "Box"})
			if err != nil {
				return err
			}
			if len(res.Matches) != n {
				return fmt.Errorf("find over %d pages: len(Matches) = %d, want %d", n/shapesPerPage, len(res.Matches), n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx Find pages", setup)
	laws.TimeScales(t, "vsdx Find pages", setup)
}

// TestMemoryBudget checks the memory row: a 10,000-shape page reads
// with peak live heap at most 8 times its page XML plus 2 MB
// (laws.WithinMemory; the allocation budget is the measured number
// with about twenty percent of margin: Generated(10000), page XML
// 7,502,118 bytes, measured peak live 5.0 MB at "page read" and
// 334.9 MB allocated, so the allocation budget is 402 MB). Page
// has no checkpoint of its own, so the run marks after the read
// with the output kept alive, as the xlsx shift memory test does.
func TestMemoryBudget(t *testing.T) {
	path := vsdxtest.Generated(t, memoryShapes)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Generated(%d): %v", memoryShapes, err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatalf("opc.Open(Generated(%d)): %v", memoryShapes, err)
	}
	pt, err := pkg.Part("/visio/pages/page1.xml")
	if err != nil {
		t.Fatalf("Part(page1.xml): %v", err)
	}
	pageXML, ok, err := pt.OriginalString()
	if err != nil || !ok {
		t.Fatalf("OriginalString(page1.xml): %v, ok %v", err, ok)
	}
	if err := pkg.Close(); err != nil {
		t.Fatalf("close Generated(%d): %v", memoryShapes, err)
	}
	t.Logf("Generated(%d): page XML %d bytes, budget peak live %d bytes (8 times the page XML plus 2 MB)",
		memoryShapes, len(pageXML), 8*len(pageXML)+2*(1<<20))
	budget := laws.Budget{PeakLive: uint64(8*len(pageXML)) + laws.MB(2), Alloc: laws.MB(402)}
	laws.WithinMemory(t, "vsdx Page memory", budget, func(mark func(string)) error {
		out, err := Page(data, "B", "page0", Options{})
		if err != nil {
			return err
		}
		// The output is what the read holds when it returns; the
		// page tree and model are already dropped, so keep the
		// output alive past the mark to measure it.
		mark("page read")
		runtime.KeepAlive(out)
		runtime.KeepAlive(data)
		if !strings.Contains(string(out), `"page0/s15000"`) {
			return fmt.Errorf("Page(Generated(%d)) has no page0/s15000", memoryShapes)
		}
		return nil
	})
}

// TestScalingUsesTimeScales checks the TSF row: no listed vsdx test
// computes a time ratio except through laws.TimeScales, so a function
// whose name starts with TestScaling (plus TestLawsScalingSubset)
// over the listed files uses no time.Since and no laws.Measure
// Cost.Time ratio.
func TestScalingUsesTimeScales(t *testing.T) {
	files := []string{
		"cmd/vsdx/render_test.go",
		"internal/vsdx/doc/contents_test.go",
		"internal/vsdx/edit/edit_test.go",
		"internal/vsdx/edit/pages_test.go",
		"internal/vsdx/integrity/integrity_test.go",
		"internal/vsdx/model/build_test.go",
		"internal/vsdx/model/cells_test.go",
		"internal/vsdx/model/connects_test.go",
		"internal/vsdx/model/props_test.go",
		"internal/vsdx/model/textobj_test.go",
		"internal/vsdx/read/find_test.go",
		"internal/vsdx/read/page_test.go",
		"internal/vsdx/read/scaling_test.go",
		"internal/vsdx/read/summary_test.go",
		"internal/vsdx/read/xml_test.go",
		"internal/vsdx/read/laws_test.go",
	}
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	for _, rel := range files {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, rel), nil, 0)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", rel, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "TestScaling") && fn.Name.Name != "TestLawsScalingSubset" {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if id.Name == "time" && sel.Sel.Name == "Since" {
					t.Errorf("%s: %s uses time.Since, want laws.TimeScales", rel, fn.Name.Name)
				}
				if id.Name == "laws" && sel.Sel.Name == "Measure" {
					t.Errorf("%s: %s uses laws.Measure, want laws.TimeScales", rel, fn.Name.Name)
				}
				if sel.Sel.Name == "Time" {
					t.Errorf("%s: %s reads Cost.Time, want laws.TimeScales", rel, fn.Name.Name)
				}
				return true
			})
		}
	}
}
