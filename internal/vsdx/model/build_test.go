package model

import (
	"archive/zip"
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// openFixture opens the vsdx fixture name and returns its Document
// and a resolver over it.
func openFixture(t *testing.T, name string) (*doc.Document, *inherit.Resolver) {
	t.Helper()
	d, err := doc.Open(vsdxtest.OpenPackage(t, name))
	if err != nil {
		t.Fatalf("doc.Open(%s): %v", name, err)
	}
	return d, inherit.New(d)
}

// openGenerated opens a Generated drawing of n shapes and returns its
// Document and a resolver over it.
func openGenerated(t *testing.T, n int) (*doc.Document, *inherit.Resolver) {
	t.Helper()
	p, err := opc.Open(vsdxtest.Generated(t, n))
	if err != nil {
		t.Fatalf("opc.Open(Generated(%d)): %v", n, err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close Generated(%d): %v", n, err)
		}
	})
	d, err := doc.Open(p)
	if err != nil {
		t.Fatalf("doc.Open(Generated(%d)): %v", n, err)
	}
	return d, inherit.New(d)
}

// buildPageOf builds page id of d through r and fails the test on an
// error.
func buildPageOf(t *testing.T, d *doc.Document, r *inherit.Resolver, id uint32) *Page {
	t.Helper()
	e, ok := d.Page(id)
	if !ok {
		t.Fatalf("Page(%d) not found, Pages = %v", id, d.Pages)
	}
	p, err := BuildPage(d, r, e, Options{})
	if err != nil {
		t.Fatalf("BuildPage(page%d): %v", id, err)
	}
	return p
}

// buildPageOpt builds page id of d through r with options o and fails
// the test on an error.
func buildPageOpt(t *testing.T, d *doc.Document, r *inherit.Resolver, id uint32, o Options) *Page {
	t.Helper()
	e, ok := d.Page(id)
	if !ok {
		t.Fatalf("Page(%d) not found, Pages = %v", id, d.Pages)
	}
	p, err := BuildPage(d, r, e, o)
	if err != nil {
		t.Fatalf("BuildPage(page%d): %v", id, err)
	}
	return p
}

// objectIDs returns the addresses of p's objects in order.
func objectIDs(p *Page) []string {
	ids := make([]string, len(p.Objects))
	for i, o := range p.Objects {
		ids[i] = o.ID
	}
	return ids
}

// byID returns the object of p with address id, failing the test when
// there is none.
func byID(t *testing.T, p *Page, id string) *Object {
	t.Helper()
	for _, o := range p.Objects {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("no object %s in %v", id, objectIDs(p))
	return nil
}

// TestBuildPageV1 checks the V1 row: one object page0/s1 of kind
// shape with Z 0.
func TestBuildPageV1(t *testing.T) {
	d, r := openFixture(t, "v1-minimal")
	p := buildPageOf(t, d, r, 0)
	if len(p.Objects) != 1 {
		t.Fatalf("objects = %v, want one", objectIDs(p))
	}
	o := p.Objects[0]
	if o.ID != "page0/s1" {
		t.Errorf("object ID = %q, want page0/s1", o.ID)
	}
	if o.Kind != Shape {
		t.Errorf("object kind = %q, want shape", o.Kind)
	}
	if o.Z != 0 {
		t.Errorf("object Z = %d, want 0", o.Z)
	}
	if o.ShapeID != 1 || o.Parent != "" || o.Node == nil {
		t.Errorf("object = %+v, want ShapeID 1, no parent and a node", o)
	}
	if p.ID != "page0" {
		t.Errorf("page ID = %q, want page0", p.ID)
	}
}

// TestOriginsFullDetail checks the M1F rows 1 to 5: the origins of
// the shown properties, exactly, with Full and (for the properties
// derived there) in the default view. Row 1 (V1): s1 box local,
// fill style (FillForegnd local, FillPattern from StyleSheet 0),
// html local, line style. Row 2 (V2): s1 box master, fill style,
// html master, line style; s2 box master (Width inherited, Height
// from the master, and master ranks above inherited), fill style,
// html local, line style. Row 3 (V3): s4 (styles 2, fill and line
// none) box local, fill style, line style. Row 4 (V5): s3 with
// Full, box local, fill style, from local, line style, to local;
// in the default view its box is not derived, so its default
// origins are the same without box. Row 6 (the v1-page0-full
// golden) is checked by R2.
func TestOriginsFullDetail(t *testing.T) {
	rows := []struct {
		fixture, id string
		full        map[string]inherit.Origin
		def         map[string]inherit.Origin
	}{
		{"v1-minimal", "page0/s1",
			map[string]inherit.Origin{"box": inherit.Local, "fill": inherit.Style, "html": inherit.Local, "line": inherit.Style},
			map[string]inherit.Origin{"box": inherit.Local, "fill": inherit.Style, "html": inherit.Local, "line": inherit.Style}},
		{"v2-master", "page0/s1",
			map[string]inherit.Origin{"box": inherit.Master, "fill": inherit.Style, "html": inherit.Master, "line": inherit.Style},
			map[string]inherit.Origin{"box": inherit.Master, "fill": inherit.Style, "html": inherit.Master, "line": inherit.Style}},
		{"v2-master", "page0/s2",
			map[string]inherit.Origin{"box": inherit.Master, "fill": inherit.Style, "html": inherit.Local, "line": inherit.Style},
			map[string]inherit.Origin{"box": inherit.Master, "fill": inherit.Style, "html": inherit.Local, "line": inherit.Style}},
		{"v3-styles", "page0/s4",
			map[string]inherit.Origin{"box": inherit.Local, "fill": inherit.Style, "line": inherit.Style},
			map[string]inherit.Origin{"box": inherit.Local, "fill": inherit.Style, "line": inherit.Style}},
		{"v5-glue", "page0/s3",
			map[string]inherit.Origin{"box": inherit.Local, "fill": inherit.Style, "from": inherit.Local, "line": inherit.Style, "to": inherit.Local},
			map[string]inherit.Origin{"fill": inherit.Style, "from": inherit.Local, "line": inherit.Style, "to": inherit.Local}},
	}
	for _, row := range rows {
		d, r := openFixture(t, row.fixture)
		if got := byID(t, buildPageOpt(t, d, r, 0, Options{Full: true}), row.id).Origins; !maps.Equal(got, row.full) {
			t.Errorf("%s %s origins with Full = %v, want %v", row.fixture, row.id, got, row.full)
		}
		if got := byID(t, buildPageOpt(t, d, r, 0, Options{}), row.id).Origins; !maps.Equal(got, row.def) {
			t.Errorf("%s %s origins in the default view = %v, want %v", row.fixture, row.id, got, row.def)
		}
	}
}

// TestBuildPageV2 checks the V2 row: objects in order s1, s2, s3, s4,
// s5; the Parent of s4 and s5 is page0/s3; s3 is a group; the
// sub-shapes' inherit.Shape Master is 3 (shown as master3).
func TestBuildPageV2(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	p := buildPageOf(t, d, r, 0)
	want := []string{"page0/s1", "page0/s2", "page0/s3", "page0/s4", "page0/s5"}
	if got := objectIDs(p); !slices.Equal(got, want) {
		t.Fatalf("objects = %v, want %v", got, want)
	}
	for i, o := range p.Objects {
		if o.Z != i {
			t.Errorf("objects[%d] Z = %d, want %d", i, o.Z, i)
		}
	}
	if o := byID(t, p, "page0/s3"); o.Kind != Group {
		t.Errorf("s3 kind = %q, want group", o.Kind)
	}
	for _, id := range []string{"page0/s4", "page0/s5"} {
		o := byID(t, p, id)
		if o.Parent != "page0/s3" {
			t.Errorf("%s Parent = %q, want page0/s3", id, o.Parent)
		}
		if o.Master != "master3" {
			t.Errorf("%s Master = %q, want master3 (the inherit.Shape Master 3 of the group instance)", id, o.Master)
		}
		if o.MasterName != "Pair" {
			t.Errorf("%s MasterName = %q, want Pair", id, o.MasterName)
		}
	}
	if o := byID(t, p, "page0/s1"); o.Master != "master2" {
		t.Errorf("s1 Master = %q, want master2", o.Master)
	}
	if o := byID(t, p, "page0/s3"); o.Master != "master3" {
		t.Errorf("s3 Master = %q, want master3", o.Master)
	}
}

// TestBuildPageV5 checks the V5 row: s3, s4 and s5 are connectors
// (BeginX stored), s6 is a shape.
func TestBuildPageV5(t *testing.T) {
	d, r := openFixture(t, "v5-glue")
	p := buildPageOf(t, d, r, 0)
	for _, id := range []string{"page0/s3", "page0/s4", "page0/s5"} {
		if o := byID(t, p, id); o.Kind != Connector {
			t.Errorf("%s kind = %q, want connector", id, o.Kind)
		}
	}
	if o := byID(t, p, "page0/s6"); o.Kind != Shape {
		t.Errorf("s6 kind = %q, want shape", o.Kind)
	}
	for _, id := range []string{"page0/s1", "page0/s2"} {
		if o := byID(t, p, id); o.Kind != Shape {
			t.Errorf("%s kind = %q, want shape", id, o.Kind)
		}
	}
}

// TestBuildPageV4 checks the V4 row: the paint order is s10, s11,
// s12, s13, s20, s21, with s11 in s10, s13 in s12 and s21 in s20.
func TestBuildPageV4(t *testing.T) {
	d, r := openFixture(t, "v4-groups")
	p := buildPageOf(t, d, r, 0)
	want := []string{"page0/s10", "page0/s11", "page0/s12", "page0/s13", "page0/s20", "page0/s21"}
	if got := objectIDs(p); !slices.Equal(got, want) {
		t.Fatalf("objects = %v, want %v", got, want)
	}
	for id, parent := range map[string]string{
		"page0/s11": "page0/s10", "page0/s13": "page0/s12", "page0/s21": "page0/s20",
	} {
		if o := byID(t, p, id); o.Parent != parent {
			t.Errorf("%s Parent = %q, want %q", id, o.Parent, parent)
		}
	}
	for _, id := range []string{"page0/s10", "page0/s12", "page0/s20"} {
		if o := byID(t, p, id); o.Kind != Group {
			t.Errorf("%s kind = %q, want group", id, o.Kind)
		}
	}
}

// TestBuildPageV10 checks the V10 row: the two shapes with stored ID
// 7 are addressed page0/s7 and page0/s7~2.
func TestBuildPageV10(t *testing.T) {
	d, r := openFixture(t, "v10-defects")
	p := buildPageOf(t, d, r, 0)
	if len(p.Objects) < 2 || p.Objects[0].ID != "page0/s7" || p.Objects[1].ID != "page0/s7~2" {
		t.Errorf("objects = %v, want page0/s7 then page0/s7~2", objectIDs(p))
	}
	if o := byID(t, p, "page0/s7~2"); o.ShapeID != 7 || o.Z != 1 {
		t.Errorf("page0/s7~2 = ShapeID %d Z %d, want 7 and 1", o.ShapeID, o.Z)
	}
}

// TestBuildPageV15 checks the V15 row: kinds picture and other (s1 a
// picture, s2 and the guide s3 other).
func TestBuildPageV15(t *testing.T) {
	d, r := openFixture(t, "v15-foreign")
	p := buildPageOf(t, d, r, 0)
	if o := byID(t, p, "page0/s1"); o.Kind != Picture {
		t.Errorf("s1 kind = %q, want picture", o.Kind)
	}
	if o := byID(t, p, "page0/s2"); o.Kind != Other {
		t.Errorf("s2 kind = %q, want other", o.Kind)
	}
	if o := byID(t, p, "page0/s3"); o.Kind != Other || o.Type != "Guide" {
		t.Errorf("s3 kind, Type = %q, %q, want other, Guide", o.Kind, o.Type)
	}
}

// TestBuildMasterV2 checks BuildMaster on V2's master3: objects
// master3/s5, master3/s6, master3/s7 in that order, s5 a group and
// the Parent of s6 and s7 master3/s5.
func TestBuildMasterV2(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	m, ok := d.Master(3)
	if !ok {
		t.Fatalf("Master(3) not found, Masters = %v", d.Masters)
	}
	p, err := BuildMaster(d, r, m, Options{})
	if err != nil {
		t.Fatalf("BuildMaster(master3): %v", err)
	}
	want := []string{"master3/s5", "master3/s6", "master3/s7"}
	if got := objectIDs(p); !slices.Equal(got, want) {
		t.Fatalf("objects = %v, want %v", got, want)
	}
	if o := byID(t, p, "master3/s5"); o.Kind != Group {
		t.Errorf("master3/s5 kind = %q, want group", o.Kind)
	}
	for _, id := range []string{"master3/s6", "master3/s7"} {
		if o := byID(t, p, id); o.Parent != "master3/s5" {
			t.Errorf("%s Parent = %q, want master3/s5", id, o.Parent)
		}
	}
	if p.ID != "master3" || p.Name != "Pair" {
		t.Errorf("page ID, Name = %q, %q, want master3, Pair", p.ID, p.Name)
	}
}

// TestBuildMasterFrameNW checks the NW row: BuildMaster on V2's
// master2 gives master2/s5 box [0,0,72,54] in its own PageSheet
// frame (PageHeight 0.75 in, y down from the top), not y -54.
func TestBuildMasterFrameNW(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	m, ok := d.Master(2)
	if !ok {
		t.Fatalf("Master(2) not found, Masters = %v", d.Masters)
	}
	p, err := BuildMaster(d, r, m, Options{})
	if err != nil {
		t.Fatalf("BuildMaster(master2): %v", err)
	}
	o := byID(t, p, "master2/s5")
	if o.Box == nil {
		t.Fatal("master2/s5 Box = nil, want [0,0,72,54]")
	}
	if got := *o.Box; got.X != 0 || got.Y != 0 || got.W != 72 || got.H != 54 {
		t.Errorf("master2/s5 Box = %v, want [0,0,72,54]", got)
	}
	if !p.HasSize || p.W != 1 || p.H != 0.75 {
		t.Errorf("master2 W, H, HasSize = %v, %v, %v, want 1, 0.75, true", p.W, p.H, p.HasSize)
	}
}

// TestBuildMasterNoPageHeightNW checks the NW row: a master built
// without PageHeight (a test-built entry whose PageSheet stores no
// PageHeight) leaves box, from and to out and carries the note
// "master has no PageHeight".
func TestBuildMasterNoPageHeightNW(t *testing.T) {
	d, r := openFixture(t, "v2-master")
	m, ok := d.Master(2)
	if !ok {
		t.Fatalf("Master(2) not found, Masters = %v", d.Masters)
	}
	noHeight := *m
	noHeight.PageSheet = nil
	p, err := BuildMaster(d, r, &noHeight, Options{})
	if err != nil {
		t.Fatalf("BuildMaster(master without PageHeight): %v", err)
	}
	for _, o := range p.Objects {
		if o.Box != nil || o.From != nil || o.To != nil {
			t.Errorf("%s Box, From, To = %v, %v, %v, want all nil", o.ID, o.Box, o.From, o.To)
		}
	}
	want := doc.Note{At: "master2", Reason: "master has no PageHeight"}
	found := false
	for _, n := range p.Notes {
		if n == want {
			found = true
		}
	}
	if !found {
		t.Errorf("notes = %v, want %v", p.Notes, want)
	}
}

// TestBuildPageNil checks that BuildPage and BuildMaster refuse a
// nil document, page or master with an error, never a panic.
func TestBuildPageNil(t *testing.T) {
	if _, err := BuildPage(nil, nil, nil, Options{}); err == nil {
		t.Error("BuildPage(nil, nil, nil) succeeded, want an error")
	}
	if _, err := BuildMaster(nil, nil, nil, Options{}); err == nil {
		t.Error("BuildMaster(nil, nil, nil) succeeded, want an error")
	}
}

// deepPage returns a page part with shapes nested depth levels deep:
// a group holding a group holding ..., each with one cell-less member
// shape at the bottom. It builds the nesting in a string, so the
// test holds no per-level structure beyond the bytes.
func deepPage(depth int) string {
	var b strings.Builder
	b.WriteString(`<?xml version='1.0' encoding='utf-8' ?>` + "\n" +
		`<PageContents xmlns='http://schemas.microsoft.com/office/visio/2012/main' xml:space='preserve'><Shapes>`)
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&b, `<Shape ID='%d' Type='Group'><Shapes>`, i)
	}
	fmt.Fprintf(&b, `<Shape ID='%d'/>`, depth+1)
	for range depth {
		b.WriteString(`</Shapes></Shape>`)
	}
	b.WriteString(`</Shapes></PageContents>`)
	return b.String()
}

// openDeep opens a Document built from the V1 fixture's members with
// its page part replaced by a page nested depth levels deep.
func openDeep(t *testing.T, depth int) (*doc.Document, *inherit.Resolver) {
	t.Helper()
	path := vsdxtest.Fixture(t, "v1-minimal")
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open fixture zip: %v", err)
	}
	members := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			_ = zr.Close()
			t.Fatalf("open member %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			_ = rc.Close()
			_ = zr.Close()
			t.Fatalf("read member %s: %v", f.Name, err)
		}
		if err := rc.Close(); err != nil {
			_ = zr.Close()
			t.Fatalf("close member %s: %v", f.Name, err)
		}
		members[f.Name] = buf.String()
	}
	if err := zr.Close(); err != nil {
		t.Fatalf("close fixture zip: %v", err)
	}
	members["visio/pages/page1.xml"] = deepPage(depth)
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
	built := filepath.Join(t.TempDir(), "deep.vsdx")
	if err := os.WriteFile(built, data, 0o600); err != nil {
		t.Fatalf("write deep package: %v", err)
	}
	p, err := opc.Open(built)
	if err != nil {
		t.Fatalf("opc.Open(deep): %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close deep package: %v", err)
		}
	})
	d, err := doc.Open(p)
	if err != nil {
		t.Fatalf("doc.Open(deep): %v", err)
	}
	return d, inherit.New(d)
}

// TestBuildPageDeep checks the V16 row: 600-deep nesting is an error
// (the unreadable page), even with the goroutine's max stack at
// 1 MB, and never a crash. The tokenizer's depth cap (512) refuses
// the part before the walk could grow the stack.
func TestBuildPageDeep(t *testing.T) {
	d, r := openDeep(t, 600)
	e, ok := d.Page(0)
	if !ok {
		t.Fatalf("Page(0) not found, Pages = %v", d.Pages)
	}
	done := make(chan error, 1)
	go func() {
		debug.SetMaxStack(1 << 20)
		_, err := BuildPage(d, r, e, Options{})
		done <- err
	}()
	if err := <-done; err == nil {
		t.Error("BuildPage(600-deep) succeeded, want an error (unreadable page)")
	} else if !strings.Contains(err.Error(), "nested") {
		t.Errorf("BuildPage(600-deep) error = %v, want the tokenizer depth error", err)
	}
}

// TestBuildPageGenerated4000 checks the Generated row: a 4000-shape
// drawing builds without error, with its 2000 connectors as well
// (4000 shapes and one connector for every even shape that has a
// next).
func TestBuildPageGenerated4000(t *testing.T) {
	d, r := openGenerated(t, 4000)
	p := buildPageOf(t, d, r, 0)
	if len(p.Objects) != 6000 {
		t.Fatalf("objects = %d, want 6000 (4000 shapes and 2000 connectors)", len(p.Objects))
	}
	if o := byID(t, p, "page0/s1"); o.Kind != Shape {
		t.Errorf("s1 kind = %q, want shape", o.Kind)
	}
	if o := byID(t, p, "page0/s4001"); o.Kind != Connector {
		t.Errorf("s4001 kind = %q, want connector (the first generated connector)", o.Kind)
	}
}

// TestScalingBuildPage checks the scaling row: BuildPage on
// Generated(1000) and Generated(4000) (N = 1000, 4N = 4000) allocates
// at most 5.5 times as much (laws.Scales) and the laws.TimeScales time
// ratio is at most 8.
func TestScalingBuildPage(t *testing.T) {
	setup := func(n int) (func() error, error) {
		d, r := openGenerated(t, n)
		e, ok := d.Page(0)
		if !ok {
			return nil, fmt.Errorf("Generated(%d): no page 0", n)
		}
		return func() error {
			p, err := BuildPage(d, r, e, Options{})
			if err != nil {
				return err
			}
			if len(p.Objects) == 0 {
				return fmt.Errorf("Generated(%d): no objects", n)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx BuildPage", setup)
	laws.TimeScales(t, "vsdx BuildPage", setup)
}
