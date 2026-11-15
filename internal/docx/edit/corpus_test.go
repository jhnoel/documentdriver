package edit

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// defectiveRPr reports whether run properties rpr already break the
// declaration order of the run-property choice or hold elements the
// schema table does not know: pre-existing defects an edit must keep.
func defectiveRPr(rpr *tree.Node) bool {
	slots, _ := schema.Slots("w:CT_RPr")
	last := -1
	for _, c := range rpr.Elements() {
		k := -1
		if WML.Has(c) {
			for i, x := range slots[0].Names {
				if x.Local == c.Local() {
					k = i
				}
			}
			if c.Local() == "rPrChange" {
				k = len(slots[0].Names)
			}
		}
		if k < 0 || k < last {
			return true
		}
		last = k
	}
	return false
}

// TestCorpusEditsKeepPreExistingDefects adds a run property beside run
// properties that are already out of order or hold unknown elements, in
// every corpus document that has such a run (DOCX_CORPUS, the
// LibreOffice corpus directory): the edit succeeds, verifies, and the
// existing children stay byte for byte in their order.
func TestCorpusEditsKeepPreExistingDefects(t *testing.T) {
	dir := os.Getenv("DOCX_CORPUS")
	if dir == "" {
		t.Skip("DOCX_CORPUS not set")
	}
	var files []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(strings.ToLower(p), ".docx") {
			files = append(files, p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Document by document in parallel; each reports whether it was
	// edited.
	tested := 0
	for _, r := range corpustest.Run(t, corpustest.Files(files, dir), func(t *testing.T, file corpustest.File, edited *bool) {
		f := file.Path
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		pkg, err := opc.OpenBytes(b, opc.Limits{})
		if err != nil {
			return // not a package this test can edit
		}
		w, err := New(pkg, Options{Source: sha256.Sum256(b)})
		if err != nil {
			return
		}
		doc, err := w.Tree(w.DocumentPart())
		if err != nil {
			return
		}
		var rpr *tree.Node
		doc.Node().Walk(func(n *tree.Node) bool {
			if rpr != nil {
				return false
			}
			if is(n, "rPr") && is(n.Parent(), "r") && defectiveRPr(n) {
				rpr = n
			}
			return true
		})
		if rpr == nil {
			return
		}
		p := rpr
		for p != nil && !is(p, "p") {
			p = p.Parent()
		}
		if p == nil {
			return
		}
		var add string
		for _, cand := range []string{"vanish", "caps", "smallCaps", "emboss", "imprint"} {
			if model.ChildW(rpr, cand) == nil {
				add = cand
				break
			}
		}
		var before [][]byte
		for _, c := range rpr.Elements() {
			before = append(before, c.Bytes())
		}
		h := w.Target(w.DocumentPart(), "p", p)
		n, err := h.EnsureIn(rpr, W(add))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			return
		}
		var after [][]byte
		for _, c := range rpr.Elements() {
			if c != n {
				after = append(after, c.Bytes())
			}
		}
		if !slicesEqualBytes(before, after) {
			t.Errorf("%s: existing run properties changed", f)
		}
		if err := w.Finish(); err != nil {
			t.Errorf("%s: Finish: %v", f, err)
			return
		}
		if err := w.Verify(); err != nil {
			t.Errorf("%s: Verify: %v", f, err)
			return
		}
		*edited = true
	}) {
		if r.Value {
			tested++
		}
	}
	t.Logf("%d documents with pre-existing run-property defects edited", tested)
	if tested == 0 && !corpustest.Sharded() {
		t.Fatal("no corpus document has a run-property defect to edit beside")
	}
}

func slicesEqualBytes(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
