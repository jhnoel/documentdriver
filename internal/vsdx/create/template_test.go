package create

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// goldenDir returns the designer-authored golden directory
// testdata/vsdx/create/blank from the repository root.
func goldenDir(t *testing.T) string {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return filepath.Join(root, "testdata", "vsdx", "create", "blank")
}

// goldenMembers lists the golden members (slash-relative, without
// PROVENANCE.md) in sorted order.
func goldenMembers(t *testing.T) []string {
	t.Helper()
	dir := goldenDir(t)
	var members []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "PROVENANCE.md" {
			return nil
		}
		members = append(members, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk golden: %v", err)
	}
	slices.Sort(members)
	return members
}

// zipBodies returns the member names in zip order and their bodies.
func zipBodies(t *testing.T, data []byte) ([]string, map[string][]byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	bodies := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
		r, err := f.Open()
		if err != nil {
			t.Fatalf("open member %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(r)
		if err != nil {
			_ = r.Close()
			t.Fatalf("read member %s: %v", f.Name, err)
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close member %s: %v", f.Name, err)
		}
		bodies[f.Name] = body
	}
	return names, bodies
}

// TestTemplateGolden checks the CT1 rows: Template at the fixed clock
// has exactly the golden's members, [Content_Types].xml first then
// sorted, each byte-equal to testdata/vsdx/create/blank/<member>.
func TestTemplateGolden(t *testing.T) {
	data, err := Template(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	names, bodies := zipBodies(t, data)
	if len(names) == 0 || names[0] != "[Content_Types].xml" {
		t.Fatalf("first member = %v, want [Content_Types].xml first", names)
	}
	if !slices.IsSorted(names[1:]) {
		t.Fatalf("members after [Content_Types].xml = %v, want sorted", names[1:])
	}
	want := goldenMembers(t)
	got := slices.Clone(names)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("members = %v, want golden %v", got, want)
	}
	dir := goldenDir(t)
	for _, name := range names {
		wantBody, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read golden %s: %v", name, err)
		}
		if !bytes.Equal(bodies[name], wantBody) {
			t.Errorf("member %s differs from golden\n got %q\nwant %q", name, bodies[name], wantBody)
		}
	}
}

// TestTemplateSecondClock checks the CT1 row: Template at
// 2024-05-06T07:08:09Z differs from the golden clock only in
// core.xml's two times.
func TestTemplateSecondClock(t *testing.T) {
	before, err := Template(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	after, err := Template(time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC))
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	_, beforeBodies := zipBodies(t, before)
	_, afterBodies := zipBodies(t, after)
	for name, body := range beforeBodies {
		if name == "docProps/core.xml" {
			continue
		}
		if !bytes.Equal(body, afterBodies[name]) {
			t.Errorf("member %s differs between clocks, want only core.xml to differ", name)
		}
	}
	core := string(afterBodies["docProps/core.xml"])
	stamp := "2024-05-06T07:08:09Z"
	if n := strings.Count(core, stamp); n != 2 {
		t.Errorf("core.xml holds %d occurrences of %q, want 2", n, stamp)
	}
	if strings.Contains(core, "2000-01-01T00:00:00Z") {
		t.Errorf("core.xml at the 2024 clock still holds the 2000 stamp: %q", core)
	}
	rebased := strings.ReplaceAll(core, stamp, "2000-01-01T00:00:00Z")
	if rebased != string(beforeBodies["docProps/core.xml"]) {
		t.Errorf("core.xml at the 2024 clock differs from the golden clock beyond its two times\n got %q\nwant %q", core, beforeBodies["docProps/core.xml"])
	}
}

// TestTemplateDeterministic checks the CT1 row: two calls give equal
// bytes.
func TestTemplateDeterministic(t *testing.T) {
	clock := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	a, err := Template(clock)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	b, err := Template(clock)
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("two Template calls gave different bytes (%d vs %d bytes)", len(a), len(b))
	}
}

// TestTemplateOpens checks the CT1 row: opc.OpenBytes opens the
// template.
func TestTemplateOpens(t *testing.T) {
	data, err := Template(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Template: %v", err)
	}
	p, err := opc.OpenBytes(data, opc.Limits{})
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("close package: %v", err)
		}
	})
	for _, name := range []string{"[Content_Types].xml", "visio/document.xml", "visio/pages/pages.xml", "visio/pages/page1.xml", "visio/windows.xml", "docProps/core.xml", "docProps/app.xml"} {
		if !p.Has(name) {
			t.Errorf("opened package has no part %s", name)
		}
	}
}
