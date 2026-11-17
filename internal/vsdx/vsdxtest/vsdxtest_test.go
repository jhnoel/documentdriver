package vsdxtest

import (
	"archive/zip"
	"bytes"
	"slices"
	"strings"
	"testing"
)

// TestFixture checks chunk F1 row 1: the fixtures V1, V2 and V5 build,
// [Content_Types].xml is the first zip member, and the member list of
// V1 is exactly the list in the row.
func TestFixture(t *testing.T) {
	for _, tc := range []struct {
		name string
		want []string
	}{
		{name: "v1-minimal", want: []string{
			"[Content_Types].xml", "_rels/.rels", "docProps/app.xml",
			"visio/_rels/document.xml.rels", "visio/document.xml",
			"visio/pages/_rels/pages.xml.rels", "visio/pages/page1.xml",
			"visio/pages/pages.xml",
		}},
		{name: "v2-master"},
		{name: "v5-glue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := Fixture(t, tc.name)
			got := memberNames(t, path)
			if len(got) == 0 || got[0] != "[Content_Types].xml" {
				t.Errorf("Fixture(%q) members = %v, want [Content_Types].xml first", tc.name, got)
			}
			if tc.want != nil && !slices.Equal(got, tc.want) {
				t.Errorf("Fixture(%q) members = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestGenerated checks chunk F1 rows 2 and 3: Generated(10) has 15
// Shape elements and 10 Connect rows, and Generated(1000) page XML is
// between 0.3 MB and 3 MB, logged.
func TestGenerated(t *testing.T) {
	for _, tc := range []struct {
		name       string
		n          int
		shapes     int
		connects   int
		min, max   int
		wantCounts bool
	}{
		{name: "ten", n: 10, shapes: 15, connects: 10, wantCounts: true},
		{name: "thousand", n: 1000, min: 300_000, max: 3_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := pageXML(t, Generated(t, tc.n))
			if tc.wantCounts {
				if got := strings.Count(page, "<Shape "); got != tc.shapes {
					t.Errorf("Generated(%d) shapes = %d, want %d", tc.n, got, tc.shapes)
				}
				if got := strings.Count(page, "<Connect "); got != tc.connects {
					t.Errorf("Generated(%d) Connect rows = %d, want %d", tc.n, got, tc.connects)
				}
			}
			if tc.min != 0 {
				t.Logf("Generated(%d) page XML = %d bytes", tc.n, len(page))
				if len(page) < tc.min || len(page) > tc.max {
					t.Errorf("Generated(%d) page XML = %d bytes, want between %d and %d",
						tc.n, len(page), tc.min, tc.max)
				}
			}
		})
	}
}

// memberNames returns the zip member names of the file at path in
// stored order.
func memberNames(t *testing.T, path string) []string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = zr.Close() }()
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

// pageXML returns the visio/pages/page1.xml member of the .vsdx at path.
func pageXML(t *testing.T, path string) string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != "visio/pages/page1.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open page member of %s: %v", path, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			_ = rc.Close()
			t.Fatalf("read page member of %s: %v", path, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("close page member of %s: %v", path, err)
		}
		return buf.String()
	}
	t.Fatalf("no visio/pages/page1.xml in %s", path)
	return ""
}
