package edit

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// para returns a handle on the i-th paragraph of the test document.
func para(t *testing.T, i int) (*Workspace, *Handle, *tree.Node) {
	t.Helper()
	w, body := open(t)
	p := paras(body)[i]
	return w, w.Target(docPart, "p", []*tree.Node{p}), p
}

func TestSplitAtKeepsProperties(t *testing.T) {
	w, h, p := para(t, 0)
	r := p.Elements()[0]
	// Give the run properties and a second text through the core.
	if err := h.Ensure(Path{W("r")}, W("rPr")); err != nil {
		t.Fatal(err)
	}
	t0 := r.Elements()[1]
	right, err := h.SplitText(t0, 1)
	if err != nil {
		t.Fatal(err)
	}
	at := 0
	for i, c := range r.Children() {
		if c == right {
			at = i
		}
	}
	r2, err := h.SplitAt(r, at, func(c *tree.Node) bool { return wNS.Is(c, "rPr") })
	if err != nil {
		t.Fatal(err)
	}
	got := string(p.Bytes())
	want := `<w:p><w:r><w:rPr/><w:t>o</w:t></w:r><w:r><w:rPr/><w:t>ne</w:t></w:r></w:p>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	for _, n := range []*tree.Node{r2, right, r2.Elements()[0], r2.Elements()[1]} {
		if !w.Created(n) {
			t.Errorf("%s not recorded as created", n.Name())
		}
	}
	for _, n := range []*tree.Node{p, r, t0} {
		if w.Created(n) {
			t.Errorf("existing %s recorded as created", n.Name())
		}
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	list, err := w.CreatedIn(docPart)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range list {
		names = append(names, n.Name())
	}
	// rPr (Ensure); the right w:t sits inside the new run.
	if got := strings.Join(names, " "); got != "w:rPr w:r" {
		t.Fatalf("CreatedIn = %s", got)
	}
}

func TestWrapUnwrapRename(t *testing.T) {
	w, h, p := para(t, 0)
	r := p.Elements()[0]
	ins, err := h.Wrap(r, r, W("ins"), [][2]string{{"w:id", "1"}, {"w:author", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Rename(r.Elements()[0], "delText"); err != nil {
		t.Fatal(err)
	}
	if err := h.Rename(ins, "del"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(p.Bytes()), `<w:p><w:del w:id="1" w:author="a"><w:r><w:delText>one</w:delText></w:r></w:del></w:p>`; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if err := h.Unwrap(ins); err != nil {
		t.Fatal(err)
	}
	if err := h.SetText(r.Elements()[0], "x & y"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(p.Bytes()), `<w:p><w:r><w:delText>x &amp; y</w:delText></w:r></w:p>`; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestVerbsStayInsideTheHandle(t *testing.T) {
	w, body := open(t)
	ps := paras(body)
	h := w.Target(docPart, "p0", []*tree.Node{ps[0]})
	other := ps[1].Elements()[1] // the second paragraph's run
	if _, err := h.SplitAt(other, 0, nil); !errors.Is(err, ErrOutside) {
		t.Fatalf("SplitAt outside = %v", err)
	}
	if err := h.Rename(ps[0], "tbl"); !errors.Is(err, ErrOutside) {
		t.Fatalf("Rename of the handle's own element = %v", err)
	}
	if _, err := h.SplitText(ps[0].Elements()[0].Elements()[0], 0); err == nil {
		t.Fatal("split at the start of the text")
	}
	if _, err := h.SplitText(ps[0].Elements()[0].Elements()[0], 3); err == nil {
		t.Fatal("split at the end of the text")
	}
	if ps[0].Modified() || ps[1].Modified() {
		t.Fatal("a refused verb changed the document")
	}
}

func TestCreatedByInsertAndRemoved(t *testing.T) {
	w, body := open(t)
	nodes, err := w.Parse(body, []byte(`<w:p><w:r><w:t>new</w:t></w:r></w:p>`), map[string]string{"w": wURI})
	if err != nil {
		t.Fatal(err)
	}
	pos, err := w.SlotEnd(body, "w:CT_Body", W("p"), true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Insert(docPart, pos, nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	if !w.Created(nodes[0].Elements()[0]) {
		t.Fatal("run of an inserted paragraph not created")
	}
	if err := w.Delete(docPart, nodes[0]); err != nil {
		t.Fatal(err)
	}
	list, err := w.CreatedIn(docPart)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("removed unit still listed: %d", len(list))
	}
}
