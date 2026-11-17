// Package vsdxtest builds the hand-written .vsdx fixtures of the tests
// (V1 to V17, docs/vsdx-design.md, Verification) from XML committed
// under testdata/vsdx/<name>/, with opc.BuildZip, and generates large
// drawings for the scaling laws. It is imported only by tests.
package vsdxtest

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// commonMembers maps a file in testdata/vsdx/common/ to its zip member:
// the shared document and application parts live in visio/ and docProps/.
var commonMembers = map[string]string{
	"document.xml": "visio/document.xml",
	"app.xml":      "docProps/app.xml",
}

// Fixture builds the fixture testdata/vsdx/<name> into a fresh file in
// t.TempDir and returns its path. Every member of the directory is a
// zip member at the same relative path; PROVENANCE.md is skipped.
// Chunk F1.
func Fixture(t testing.TB, name string) string {
	t.Helper()
	if name == "" || name != filepath.Base(name) {
		t.Fatalf("vsdxtest.Fixture(%q): a plain fixture directory name", name)
	}
	return writePackage(t, name+".vsdx", fixtureMembers(t, name))
}

// Blank builds the blank drawing from its golden members,
// testdata/vsdx/create/blank (create.Template is held byte-equal to
// them), alone (no common members), and returns its path. Tests of
// packages create imports (edit, ops) use it instead of create, which
// would be an import cycle.
func Blank(t testing.TB) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("vsdxtest: repo root: %v", err)
	}
	members := map[string]string{}
	if err := collectMembers(filepath.Join(root, "testdata", "vsdx", "create", "blank"), false, members); err != nil {
		t.Fatalf("vsdxtest: blank: %v", err)
	}
	return writePackage(t, "blank.vsdx", members)
}

// OpenPackage opens the built fixture testdata/vsdx/<name> with
// opc.Open and returns its package, closing it in t.Cleanup.
// Chunk MF6.
func OpenPackage(t testing.TB, name string) *opc.Package {
	t.Helper()
	p, err := opc.Open(Fixture(t, name))
	if err != nil {
		t.Fatalf("vsdxtest.OpenPackage(%s): %v", name, err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close %s: %v", name, err)
		}
	})
	return p
}

// Generated builds a drawing of one page with n rectangles, each an
// instance of one master with text and two shape data rows, every
// second pair joined by a glued dynamic connector, and returns its
// path. Chunk F1.
func Generated(t testing.TB, n int) string {
	t.Helper()
	if n < 0 {
		t.Fatalf("vsdxtest.Generated(%d): a non-negative shape count", n)
	}
	members := fixtureMembers(t, "v2-master")
	members["visio/pages/page1.xml"] = generatedPage(n)
	return writePackage(t, fmt.Sprintf("generated-%d.vsdx", n), members)
}

// fixtureMembers returns the zip members of fixture name: testdata/vsdx/
// common/ first, mapped through commonMembers, then the fixture
// directory at its relative paths, replacing a common member of the
// same name. PROVENANCE.md and README.md are skipped; every other file
// is copied byte for byte.
func fixtureMembers(t testing.TB, name string) map[string]string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("vsdxtest: repo root: %v", err)
	}
	base := filepath.Join(root, "testdata", "vsdx")
	members := map[string]string{}
	if err := collectMembers(filepath.Join(base, "common"), true, members); err != nil {
		t.Fatalf("vsdxtest: common members: %v", err)
	}
	if err := collectMembers(filepath.Join(base, name), false, members); err != nil {
		t.Fatalf("vsdxtest: fixture %s: %v", name, err)
	}
	return members
}

// collectMembers walks dir in file order and adds each regular file to
// members at its relative path (through commonMembers when common is
// true), skipping PROVENANCE.md and README.md. A fixture directory that
// does not exist is an error.
func collectMembers(dir string, common bool, members map[string]string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if b := d.Name(); b == "PROVENANCE.md" || b == "README.md" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if common {
			if mapped, ok := commonMembers[name]; ok {
				name = mapped
			}
		}
		body, err := os.ReadFile(path) //nolint:gosec // G304: testdata path from the repo root, never from a document
		if err != nil {
			return err
		}
		members[name] = string(body)
		return nil
	})
}

// writePackage builds members as a deflated zip with
// opc.BuildZip and writes it to file in t.TempDir, returning its path.
func writePackage(t testing.TB, file string, members map[string]string) string {
	t.Helper()
	data, err := opc.BuildZip(zipMembers(members)...)
	if err != nil {
		t.Fatalf("vsdxtest: build %s: %v", file, err)
	}
	path := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("vsdxtest: write %s: %v", file, err)
	}
	return path
}

// zipMembers returns members as zip members in builder order:
// [Content_Types].xml first, the rest in name order.
func zipMembers(members map[string]string) []opc.ZipMember {
	names := slices.Sorted(maps.Keys(members))
	out := make([]opc.ZipMember, 0, len(names))
	if body, ok := members["[Content_Types].xml"]; ok {
		out = append(out, opc.ZipMember{Name: "[Content_Types].xml", Body: body})
	}
	for _, name := range names {
		if name == "[Content_Types].xml" {
			continue
		}
		out = append(out, opc.ZipMember{Name: name, Body: members[name]})
	}
	return out
}

// generatedPage returns the page1.xml of a Generated drawing: n
// instances of master 2 of V2 on a 50-column grid, one connector for
// every even shape that has a next shape. PinX is 1 + 1.5*(i%50) and
// PinY is 1 + i/50, in inches, for shape i (0-based). It streams the
// page in one pass, recomputing each pin where a connector or a
// Connect row needs it, so it holds no per-shape structure.
func generatedPage(n int) string {
	var b strings.Builder
	if n > 0 {
		b.Grow(n*750 + 512)
	}
	b.WriteString("<?xml version='1.0' encoding='utf-8' ?>\n<PageContents xmlns='" + vsd.NSMain2012 +
		"' xmlns:r='" + vsd.NSRel + "' xml:space='preserve'><Shapes>")
	for i := range n {
		name := "Process"
		if i > 0 {
			name += "." + strconv.Itoa(i+1)
		}
		fmt.Fprintf(&b, "<Shape ID='%d' NameU='%s' Name='%s' Type='Shape' Master='2'>"+
			"<Cell N='PinX' V='%s'/><Cell N='PinY' V='%s'/>"+
			"<Section N='Property'><Row N='Cost'><Cell N='Value' V='%d' U='CY'/></Row></Section>"+
			"<Text>Box %d\n</Text></Shape>",
			i+1, name, name, geom.FormatNum(1+1.5*float64(i%50)), geom.FormatNum(1+float64(i/50)), i, i)
	}
	connector := n
	for i := 0; i+1 < n; i += 2 {
		connector++
		beginX, beginY := 1+1.5*float64(i%50), 1+float64(i/50)
		endX, endY := 1+1.5*float64((i+1)%50), 1+float64((i+1)/50)
		width := endX - beginX
		fmt.Fprintf(&b, "<Shape ID='%d' NameU='Dynamic connector.%d' Name='Dynamic connector.%d' Type='Shape'"+
			" LineStyle='3' FillStyle='3' TextStyle='3'>"+
			"<Cell N='PinX' V='%s' F='(BeginX+EndX)/2'/><Cell N='PinY' V='%s' F='(BeginY+EndY)/2'/>"+
			"<Cell N='Width' V='%s' F='SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)'/><Cell N='Height' V='0'/>"+
			"<Cell N='LocPinX' V='%s' F='Width*0.5'/><Cell N='LocPinY' V='0'/>"+
			"<Cell N='Angle' V='0' F='ATAN2(EndY-BeginY,EndX-BeginX)'/><Cell N='FlipX' V='0'/><Cell N='FlipY' V='0'/>"+
			"<Cell N='BeginX' V='%s'/><Cell N='BeginY' V='%s'/><Cell N='EndX' V='%s'/><Cell N='EndY' V='%s'/>"+
			"<Cell N='ObjType' V='2'/>"+
			"<Section N='Geometry' IX='0'><Row T='MoveTo' IX='1'><Cell N='X' V='0'/><Cell N='Y' V='0'/></Row>"+
			"<Row T='LineTo' IX='2'><Cell N='X' V='%s' F='Width'/><Cell N='Y' V='0'/></Row></Section></Shape>",
			connector, connector, connector,
			geom.FormatNum((beginX+endX)/2), geom.FormatNum((beginY+endY)/2),
			geom.FormatNum(width), geom.FormatNum(width/2),
			geom.FormatNum(beginX), geom.FormatNum(beginY), geom.FormatNum(endX), geom.FormatNum(endY),
			geom.FormatNum(width))
	}
	b.WriteString("</Shapes>")
	if connector > n {
		b.WriteString("<Connects>")
		connector = n
		for i := 0; i+1 < n; i += 2 {
			connector++
			fmt.Fprintf(&b, "<Connect FromSheet='%d' FromCell='BeginX' FromPart='9'"+
				" ToSheet='%d' ToCell='PinX' ToPart='3'/>"+
				"<Connect FromSheet='%d' FromCell='EndX' FromPart='12'"+
				" ToSheet='%d' ToCell='Connections.X1' ToPart='100'/>",
				connector, i+1, connector, i+2)
		}
		b.WriteString("</Connects>")
	}
	b.WriteString("</PageContents>")
	return b.String()
}
