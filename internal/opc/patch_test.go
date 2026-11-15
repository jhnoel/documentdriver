package opc

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
)

const sheetPart = "/xl/worksheets/sheet1.xml"

// bigSheet returns a worksheet of rows rows by 10 cells, large enough
// that deflate writes several blocks.
func bigSheet(rows int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d">`, r)
		for c := range 10 {
			fmt.Fprintf(&b, `<c r="%c%d"><v>%d</v></c>`, 'A'+c, r, r*c)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

// writeWorkbook writes a minimal package holding sheet, stored with
// method, and returns its path.
func writeWorkbook(t testing.TB, sheet string, method uint16) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.xlsx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, m := range []struct {
		name, body string
		method     uint16
	}{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`, zip.Deflate},
		{"xl/worksheets/sheet1.xml", sheet, method},
		{"xl/other.xml", `<other/>`, zip.Deflate},
	} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: m.name, Method: m.method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// cellEdits patches some cells of a bigSheet: values replaced, a row
// deleted, a row inserted.
func cellEdits(t testing.TB, orig string) *splice.Patch {
	t.Helper()
	p := splice.New(orig)
	for _, r := range []int{1, 7, 500, 2999} {
		cell := fmt.Sprintf(`<c r="B%d"><v>%d</v></c>`, r, r)
		i := strings.Index(orig, cell)
		if i < 0 {
			t.Fatalf("no cell B%d", r)
		}
		if err := p.Replace(i, i+len(cell), fmt.Sprintf(`<c r="B%d" t="inlineStr"><is><t>new %d</t></is></c>`, r, r)); err != nil {
			t.Fatal(err)
		}
	}
	row := `<row r="1000">`
	s := strings.Index(orig, row)
	e := strings.Index(orig, `<row r="1001">`)
	if err := p.Replace(s, e, ""); err != nil {
		t.Fatal(err)
	}
	// e is the deleted row's end: an insertion may touch it.
	if err := p.Replace(e, e, `<row r="1000" spans="1:1"/>`); err != nil {
		t.Fatal(err)
	}
	return p
}

func sheetOriginal(t testing.TB, p *Package) (*Part, string) {
	t.Helper()
	pt, err := p.Part(sheetPart)
	if err != nil {
		t.Fatal(err)
	}
	orig, _, err := pt.OriginalString()
	if err != nil {
		t.Fatal(err)
	}
	return pt, orig
}

// SetPatch writes the same bytes as SetBytes of the patched content,
// deflated or stored, through SaveAs and Seal.
func TestSetPatchMatchesSetBytes(t *testing.T) {
	for _, method := range []uint16{zip.Deflate, zip.Store} {
		t.Run(strconv.Itoa(int(method)), func(t *testing.T) {
			path := writeWorkbook(t, bigSheet(3000), method)
			dir := t.TempDir()

			pa := open(t, path)
			pt, orig := sheetOriginal(t, pa)
			patch := cellEdits(t, orig)
			if err := pt.SetPatch(patch); err != nil {
				t.Fatal(err)
			}
			if pt.Patch() != patch {
				t.Fatal("Patch does not return the patch set")
			}
			if ch, err := pt.Modified(); err != nil || !ch {
				t.Fatalf("Modified %v, %v", ch, err)
			}
			if names, err := pa.ChangedParts(); err != nil || len(names) != 1 || names[0] != sheetPart {
				t.Fatalf("ChangedParts %v, %v", names, err)
			}
			want := patch.String()
			if b, err := pt.Bytes(); err != nil || string(b) != want {
				t.Fatalf("Bytes: %v", err)
			}
			sa, err := pa.Seal()
			if err != nil {
				t.Fatal(err)
			}
			a := filepath.Join(dir, "a.xlsx")
			if err := pa.SaveAs(a); err != nil {
				t.Fatal(err)
			}

			pb := open(t, path)
			ptb, _ := sheetOriginal(t, pb)
			ptb.SetBytes([]byte(want))
			sb, err := pb.Seal()
			if err != nil {
				t.Fatal(err)
			}
			b := filepath.Join(dir, "b.xlsx")
			if err := pb.SaveAs(b); err != nil {
				t.Fatal(err)
			}

			ba, bb := readFile(t, a), readFile(t, b)
			if !bytes.Equal(ba, bb) {
				t.Fatal("SetPatch and SetBytes saves differ")
			}
			if !bytes.Equal(sa.Bytes(), sb.Bytes()) || !bytes.Equal(sa.Bytes(), ba) {
				t.Fatal("sealed bytes differ from the save")
			}
			re := open(t, a)
			rpt, err := re.Part(sheetPart)
			if err != nil {
				t.Fatal(err)
			}
			if got, err := rpt.Bytes(); err != nil || string(got) != want {
				t.Fatalf("reopened part differs: %v", err)
			}
		})
	}
}

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: a test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A patch that changes nothing (empty, identity, or edits that cancel
// out) leaves the package unmodified, and a save writes the original.
func TestSetPatchNoOp(t *testing.T) {
	path := writeWorkbook(t, bigSheet(20), zip.Deflate)
	ageFile(t, path)
	s := snap(t, path)
	for _, name := range []string{"empty", "identity", "cancelling"} {
		p := open(t, path)
		pt, orig := sheetOriginal(t, p)
		patch := splice.New(orig)
		row := `<row r="3">`
		i := strings.Index(orig, row)
		switch name {
		case "identity":
			if err := patch.Replace(i, i+len(row), row); err != nil {
				t.Fatal(err)
			}
		case "cancelling":
			cell := `<c r="A3"><v>0</v></c>`
			j := i + len(row)
			if err := patch.Replace(j, j, cell); err != nil {
				t.Fatal(err)
			}
			if err := patch.Replace(j, j+len(cell), ""); err != nil {
				t.Fatal(err)
			}
			if patch.Empty() {
				t.Fatal("cancelling edits left the patch empty")
			}
		}
		if err := pt.SetPatch(patch); err != nil {
			t.Fatal(err)
		}
		if ch, err := p.Modified(); err != nil || ch {
			t.Fatalf("%s: Modified %v, %v", name, ch, err)
		}
		if err := p.Save(); err != nil {
			t.Fatal(err)
		}
		assertUntouched(t, path, s)
		p.Close()
	}
}

func TestSetPatchRefusals(t *testing.T) {
	path := writeWorkbook(t, bigSheet(5), zip.Deflate)
	p := open(t, path)
	pt, orig := sheetOriginal(t, p)

	if err := pt.SetPatch(splice.New(orig + " ")); err == nil {
		t.Error("a patch over other bytes accepted")
	}
	added, err := p.AddPart("/xl/new.xml", []byte("<n/>"), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := added.SetPatch(splice.New("<n/>")); err == nil {
		t.Error("a patch on an added part accepted")
	}
	other, err := p.Part("/xl/other.xml")
	if err != nil {
		t.Fatal(err)
	}
	other.SetBytes([]byte("<other a='1'/>"))
	if err := other.SetPatch(splice.New("<other/>")); err == nil {
		t.Error("a patch on a replaced part accepted")
	}

	// An unmodified tree is dropped; a modified one refuses.
	if _, err := pt.Tree(); err != nil {
		t.Fatal(err)
	}
	patch := splice.New(orig)
	if err := pt.SetPatch(patch); err != nil {
		t.Fatalf("unmodified tree: %v", err)
	}
	if _, err := pt.Tree(); !errors.Is(err, ErrSpliced) {
		t.Errorf("Tree of a patched part: %v, want ErrSpliced", err)
	}
	pt.SetBytes([]byte(orig))
	if pt.Patch() != nil {
		t.Error("SetBytes kept the patch")
	}
	q := open(t, path)
	qt, qorig := sheetOriginal(t, q)
	doc, err := qt.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Root().SetAttr("a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := qt.SetPatch(splice.New(qorig)); err == nil {
		t.Error("a patch over a modified tree accepted")
	}
}

// A patch whose result would not be well formed fails the save, which
// writes nothing.
func TestSetPatchMalformedWritesNothing(t *testing.T) {
	path := writeWorkbook(t, bigSheet(5), zip.Deflate)
	ageFile(t, path)
	s := snap(t, path)
	p := open(t, path)
	pt, orig := sheetOriginal(t, p)
	patch := splice.New(orig)
	i := strings.Index(orig, `<row r="2">`)
	if err := patch.Replace(i, i, "<row>"); err != nil {
		t.Fatal(err)
	}
	if err := pt.SetPatch(patch); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(); err == nil || !errors.Is(err, splice.ErrReplacement) {
		t.Fatalf("save: %v, want ErrReplacement", err)
	}
	if _, err := p.Seal(); err == nil {
		t.Fatal("seal succeeded")
	}
	assertUntouched(t, path, s)
}

// The streamed write is atomic: a fault at any stage, or at any write,
// leaves the original and the target untouched and no temporary file.
func TestSetPatchFaults(t *testing.T) {
	path := writeWorkbook(t, bigSheet(3000), zip.Deflate)
	ageFile(t, path)
	s := snap(t, path)
	edit := func() *Package {
		p := open(t, path)
		pt, orig := sheetOriginal(t, p)
		if err := pt.SetPatch(cellEdits(t, orig)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Count the writes of a clean save, to fail each in turn.
	writes := 0
	restore := SetFaultHookFor(path, func(stage, _ string) error {
		if stage == StageWrite {
			writes++
		}
		return nil
	})
	p := edit()
	if err := p.SaveAs(filepath.Join(t.TempDir(), "count.xlsx")); err != nil {
		t.Fatal(err)
	}
	restore()
	if writes < 2 {
		t.Fatalf("only %d writes", writes)
	}
	type fault struct {
		stage string
		n     int
	}
	var faults []fault
	for _, st := range []string{StageCreate, StageChmod, StageSync, StageClose, StageRename} {
		faults = append(faults, fault{st, 0})
	}
	for n := 1; n <= writes; n++ {
		faults = append(faults, fault{StageWrite, n})
	}
	for _, f := range faults {
		for _, inPlace := range []bool{true, false} {
			dir := t.TempDir()
			dst := filepath.Join(dir, "out.xlsx")
			k := 0
			restore := SetFaultHookFor(path, func(stage, _ string) error {
				if stage != f.stage {
					return nil
				}
				k++
				if f.n == 0 || k == f.n {
					return errors.New("injected")
				}
				return nil
			})
			p := edit()
			var err error
			if inPlace {
				err = p.SaveAndClose()
			} else {
				err = p.SaveAs(dst)
				p.Close()
			}
			restore()
			if err == nil {
				t.Fatalf("%s #%d in place %v: save succeeded", f.stage, f.n, inPlace)
			}
			assertUntouched(t, path, s)
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("%s #%d: left %v", f.stage, f.n, entries)
			}
			if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
				t.Fatalf("%s #%d: left %d files beside the original", f.stage, f.n, len(entries)-1)
			}
		}
	}
}

// Forget drops an unmodified part's bytes; a re-read gives them back,
// and a save after it writes nothing. A part a save or reader needs is
// kept.
func TestForget(t *testing.T) {
	path := writeWorkbook(t, bigSheet(50), zip.Deflate)
	ageFile(t, path)
	s := snap(t, path)
	p := open(t, path)
	pt, orig := sheetOriginal(t, p)
	if !pt.Forget() {
		t.Fatal("unmodified part not forgotten")
	}
	if pt.m.loaded || pt.m.orig != "" {
		t.Fatal("bytes still cached")
	}
	again, _, err := pt.OriginalString()
	if err != nil || again != orig {
		t.Fatalf("re-read differs: %v", err)
	}
	if !pt.Forget() {
		t.Fatal("not forgotten again after a re-read")
	}
	if b, err := pt.Bytes(); err != nil || string(b) != orig {
		t.Fatalf("Bytes after Forget: %v", err)
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	assertUntouched(t, path, s)

	q := open(t, path)
	qt, _ := sheetOriginal(t, q)
	if _, err := qt.Tree(); err != nil {
		t.Fatal(err)
	}
	if qt.Forget() {
		t.Error("a part with a tree forgotten")
	}
	r := open(t, path)
	rt, rorig := sheetOriginal(t, r)
	patch := splice.New(rorig)
	i := strings.Index(rorig, `<row r="2">`)
	if err := patch.Replace(i, i, `<row r="1" spans="1:1"/>`); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetPatch(patch); err != nil {
		t.Fatal(err)
	}
	if rt.Forget() {
		t.Error("a patched part forgotten")
	}
	other, err := r.Part("/xl/other.xml")
	if err != nil {
		t.Fatal(err)
	}
	other.SetBytes([]byte("<other b='2'/>"))
	if other.Forget() {
		t.Error("a replaced part forgotten")
	}
	added, err := r.AddPart("/xl/added.xml", []byte("<a/>"), "")
	if err != nil {
		t.Fatal(err)
	}
	if added.Forget() {
		t.Error("an added part forgotten")
	}
}
