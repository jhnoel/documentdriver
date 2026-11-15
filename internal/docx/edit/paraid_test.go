package edit

import (
	"crypto/sha256"
	"os"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/opc"
)

const w14Decl = ` xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"`

// idDoc has a valid id, a missing one, a malformed one and a duplicate.
const idDoc = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document` + nsDecl + w14Decl + `><w:body>` +
	`<w:p w14:paraId="1A2B3C4D"><w:r><w:t>a</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t>b</w:t></w:r></w:p>` +
	`<w:p w14:paraId="ZZ"><w:r><w:t>c</w:t></w:r></w:p>` +
	`<w:p w14:paraId="1A2B3C4D"><w:r><w:t>d</w:t></w:r></w:p>` +
	`<w:sectPr/></w:body></w:document>`

// openIDs opens a package whose main document is doc, seeded by its
// bytes, and returns the workspace and the file's bytes.
func openIDs(t *testing.T, doc string, parts map[string]string) (*Workspace, []byte) {
	t.Helper()
	all := map[string]string{"word/document.xml": doc}
	for k, v := range parts {
		all[k] = v
	}
	path := writeDocx(t, all)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pkg.Close() })
	w, err := New(pkg, Options{Source: sha256.Sum256(b)})
	if err != nil {
		t.Fatal(err)
	}
	return w, b
}

func paraIDs(t *testing.T, w *Workspace) []string {
	t.Helper()
	var out []string
	for _, p := range find(t, w, docPart, "p") {
		v, _ := model.Attr(p, "paraId")
		out = append(out, v)
	}
	return out
}

func TestParaIDsMatchTheReadModel(t *testing.T) {
	w, b := openIDs(t, idDoc, nil)
	d, err := model.OpenDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	read := d.ParaIDs()
	want := map[int]string{}
	for _, id := range read.IDs {
		want[id.Ord] = id.ID
	}
	ps := find(t, w, docPart, "p")
	for i, p := range ps {
		id, ok := w.ParaID(p)
		if !ok || id.ID != want[i+1] {
			t.Errorf("paragraph %d: %v %v, read model %s", i+1, id, ok, want[i+1])
		}
	}
	// Only the paragraph without a stored id is repaired: "ZZ" keeps a
	// provisional, unwritten id, and the duplicate is 1A2B3C4D~2.
	if !w.repaired[ps[1]] || w.repaired[ps[2]] || w.repaired[ps[3]] || w.repaired[ps[0]] {
		t.Fatalf("repaired = %v", w.repaired)
	}
	if id, _ := w.ParaID(ps[3]); id.ID != "1A2B3C4D~2" {
		t.Fatalf("duplicate = %s", id.ID)
	}
}

func TestFinishNoOpWritesNothing(t *testing.T) {
	w, _ := openIDs(t, idDoc, nil)
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	ch, err := w.Package().ChangedParts()
	if err != nil || len(ch) != 0 {
		t.Fatalf("changed %v %v", ch, err)
	}
}

func TestFinishPersistsRepairsWhenOnlyAHeaderChanged(t *testing.T) {
	doc := strings.Replace(idDoc, w14Decl, "", 1)
	doc = strings.ReplaceAll(doc, ` w14:paraId="1A2B3C4D"`, "")
	doc = strings.ReplaceAll(doc, ` w14:paraId="ZZ"`, "")
	w, _ := openIDs(t, doc, nil)
	var want []string
	for _, p := range find(t, w, docPart, "p") {
		id, _ := w.ParaID(p)
		want = append(want, id.ID)
	}
	hp := find(t, w, hdrPart, "p")[0]
	if err := w.Target(hdrPart, "p", hp).SetText(hp.Find(wURI, "t"), "x"); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := paraIDs(t, w); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("written %v, want %v", got, want)
	}
	root, _ := w.Tree(docPart)
	if tag := string(root.Root().StartTag()); !strings.Contains(tag, `mc:Ignorable="w14"`) || !strings.Contains(tag, w14Decl) {
		t.Fatalf("root %s", tag)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestRepairsAreBoundToNodes(t *testing.T) {
	w, _ := openIDs(t, idDoc, nil)
	ps := find(t, w, docPart, "p")
	before, _ := w.ParaID(ps[1])
	// Insert a paragraph at the front: docxdriver would re-seed the
	// repaired ids by their new story positions.
	body := find(t, w, docPart, "body")[0]
	nodes, err := w.Core().Parse(body, []byte(`<w:p><w:r><w:t>new</w:t></w:r></w:p>`), map[string]string{"w": wURI})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Core().Insert(docPart, core.Before(ps[0]), nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if got, _ := model.Attr(ps[1], "paraId"); got != before.ID {
		t.Fatalf("repaired id %s, bound %s", got, before.ID)
	}
	created, _ := model.Attr(nodes[0], "paraId")
	if !model.IsValidParaID(created) {
		t.Fatalf("created paragraph id %q", created)
	}
	// Stored malformed and duplicate ids are left as they are; the new
	// paragraph's id is unique.
	got := paraIDs(t, w)
	if strings.Join(got, " ") != strings.Join([]string{created, "1A2B3C4D", before.ID, "ZZ", "1A2B3C4D"}, " ") {
		t.Fatalf("ids %v", got)
	}
	for _, id := range got[1:] {
		if id == created {
			t.Fatalf("created id %s clashes", created)
		}
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestCopyLosesItsOriginalsID(t *testing.T) {
	w, _ := openIDs(t, idDoc, nil)
	ps := find(t, w, docPart, "p")
	cp, err := w.Core().Copy(ps[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Core().Insert(docPart, core.Before(ps[0]), cp, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	if got, _ := model.Attr(ps[0], "paraId"); got != "1A2B3C4D" {
		t.Fatalf("original id %s", got)
	}
	if got, _ := model.Attr(cp, "paraId"); got == "1A2B3C4D" || !model.IsValidParaID(got) {
		t.Fatalf("copy id %s", got)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}
