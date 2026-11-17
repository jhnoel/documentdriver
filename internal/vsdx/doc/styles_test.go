package doc

import (
	"archive/zip"
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// openBuilt opens a Document built from the V1 fixture's members, as
// read from the zip F1 built, with mutate applied to the member map
// before F1's builder (opc.BuildZip) writes them as a fresh package.
func openBuilt(t *testing.T, mutate func(members map[string]string)) *Document {
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
	mutate(members)
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
	built := filepath.Join(t.TempDir(), "built.vsdx")
	if err := os.WriteFile(built, data, 0o600); err != nil {
		t.Fatalf("write built package: %v", err)
	}
	p, err := opc.Open(built)
	if err != nil {
		t.Fatalf("opc.Open(built): %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close built package: %v", err)
		}
	})
	d, err := Open(p)
	if err != nil {
		t.Fatalf("Open(built): %v", err)
	}
	return d
}

// hasNote reports whether d has a note whose reason contains sub.
func hasNote(d *Document, sub string) bool {
	for _, n := range d.Notes {
		if strings.Contains(n.Reason, sub) {
			return true
		}
	}
	return false
}

// TestReadStylesV1 checks the D3 V1 rows: FaceNames ["Calibri",
// "Arial"], Colors {24: "#7F7F7F", 25: "#5B9BD5"}, StyleSheet IDs
// {0, 1, 3, 6}, Styles[3] LineStyle 6 HasLine, Styles[0] HasLine
// false, and Recalc false.
func TestReadStylesV1(t *testing.T) {
	d := openFixture(t, "v1-minimal")
	wantFaces := []string{"Calibri", "Arial"}
	if !slices.Equal(d.FaceNames, wantFaces) {
		t.Errorf("FaceNames = %v, want %v", d.FaceNames, wantFaces)
	}
	wantColors := map[int]string{24: "#7F7F7F", 25: "#5B9BD5"}
	if !maps.Equal(d.Colors, wantColors) {
		t.Errorf("Colors = %v, want %v", d.Colors, wantColors)
	}
	wantIDs := []uint32{0, 1, 3, 6}
	if len(d.Styles) != len(wantIDs) {
		t.Fatalf("Styles = %v, want IDs %v", d.Styles, wantIDs)
	}
	for _, id := range wantIDs {
		if _, ok := d.Styles[id]; !ok {
			t.Errorf("Styles has no ID %d, want %v", id, wantIDs)
		}
	}
	if s := d.Styles[3]; s == nil || s.LineStyle != 6 || !s.HasLine {
		t.Errorf("Styles[3] = %+v, want LineStyle 6 HasLine", s)
	}
	if s := d.Styles[0]; s == nil || s.HasLine {
		t.Errorf("Styles[0] = %+v, want HasLine false", s)
	}
	if d.Recalc {
		t.Error("Recalc = true, want false")
	}
}

// TestReadCustomRecalc checks the D3 Recalc row: V1 plus a custom.xml
// member built from bytes, with RecalcDocument <vt:bool>true</vt:bool>,
// gives Recalc true.
func TestReadCustomRecalc(t *testing.T) {
	d := openBuilt(t, func(members map[string]string) {
		members["docProps/custom.xml"] = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" ` +
			`xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
			`<property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="RecalcDocument">` +
			`<vt:bool>true</vt:bool></property></Properties>`
		rels := members["_rels/.rels"]
		rels = strings.Replace(rels, "</Relationships>",
			`<Relationship Id="rId99" Type="`+ctypes.RelCustomProperties+`" Target="docProps/custom.xml"/></Relationships>`, 1)
		members["_rels/.rels"] = rels
		ct := members["[Content_Types].xml"]
		ct = strings.Replace(ct, "</Types>",
			`<Override PartName="/docProps/custom.xml" ContentType="`+ctypes.CTCustomProperties+`"/></Types>`, 1)
		members["[Content_Types].xml"] = ct
	})
	if !d.Recalc {
		t.Errorf("Recalc = false, want true (Notes %v)", d.Notes)
	}
}

// TestReadStylesDefects checks the D3 defect rows: a repeated
// StyleSheet ID is left out with a note, and a ColorEntry with IX 3 is
// left out with a note.
func TestReadStylesDefects(t *testing.T) {
	dup := openBuilt(t, func(members map[string]string) {
		members["visio/document.xml"] = strings.Replace(members["visio/document.xml"],
			"</StyleSheets>", "<StyleSheet ID='3' NameU='Dup' Name='Dup'/></StyleSheets>", 1)
	})
	if len(dup.Styles) != 4 {
		t.Errorf("duplicate ID: Styles = %v, want 4 IDs", dup.Styles)
	}
	if s := dup.Styles[3]; s == nil || s.NameU != "Normal" {
		t.Errorf("duplicate ID: Styles[3] = %+v, want the first Normal", s)
	}
	if !hasNote(dup, "duplicate style sheet") {
		t.Errorf("duplicate ID: Notes = %v, want a duplicate note", dup.Notes)
	}

	low := openBuilt(t, func(members map[string]string) {
		members["visio/document.xml"] = strings.Replace(members["visio/document.xml"],
			"</Colors>", "<ColorEntry IX='3' RGB='#000000'/></Colors>", 1)
	})
	if _, ok := low.Colors[3]; ok {
		t.Errorf("IX 3: Colors = %v, want 3 left out", low.Colors)
	}
	if !maps.Equal(low.Colors, map[int]string{24: "#7F7F7F", 25: "#5B9BD5"}) {
		t.Errorf("IX 3: Colors = %v, want the V1 colours", low.Colors)
	}
	if !hasNote(low, "below 24") {
		t.Errorf("IX 3: Notes = %v, want a below-24 note", low.Notes)
	}
}

// TestScalingStyles carries the D3 scaling row: readStyles on N and
// 4N style sheets allocates in proportion to N (laws.Scales, N named
// by laws.Small and laws.Large).
func TestScalingStyles(t *testing.T) {
	laws.Scales(t, "vsdx doc readStyles", func(n int) (func() error, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "<VisioDocument xmlns='http://schemas.microsoft.com/office/visio/2012/main'><StyleSheets>")
		for i := range n {
			fmt.Fprintf(&b, "<StyleSheet ID='%d' NameU='S%d' LineStyle='0'/>", i, i)
		}
		b.WriteString("</StyleSheets></VisioDocument>")
		doc, err := tree.ParseString(b.String())
		if err != nil {
			return nil, err
		}
		root := doc.Root()
		return func() error {
			d := &Document{Part: "/visio/document.xml"}
			d.readStyles(root)
			if len(d.Styles) != n {
				return fmt.Errorf("readStyles: %d styles, want %d", len(d.Styles), n)
			}
			return nil
		}, nil
	})
}
