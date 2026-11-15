package build

import (
	"errors"
	"strings"
	"testing"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

const phDoc = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
	`<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>a</w:t><w:sym w:font="Symbol" w:char="F020"/></w:r>` +
	`<w:fldSimple w:instr="AUTHOR"><w:r><w:t>Ada</w:t></w:r></w:fldSimple></w:p>` +
	`</w:body></w:document>`

// phFixture returns a region over a private copy of phDoc's body, its
// paragraph, a new empty paragraph after it, and the resolver of the
// paragraph's placeholders.
func phFixture(t *testing.T) (*core.Handle, *tree.Node, *tree.Node, *Existing) {
	t.Helper()
	d, err := tree.Parse([]byte(phDoc))
	if err != nil {
		t.Fatal(err)
	}
	body := d.Root().Elements()[0]
	h := core.Private(docxedit.Format, body)
	nodes, err := h.Workspace().Parse(body, []byte(`<w:p/>`), nsW)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.PlaceAt(body, nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	old := body.Elements()[0]
	list, blocked := model.Placeholders(old)
	if blocked != "" || len(list) != 2 {
		t.Fatalf("placeholders %d blocked %q", len(list), blocked)
	}
	id := &Existing{Placeholders: map[string]model.Placeholder{}}
	for i, ph := range list {
		id.Placeholders["0000000A/opaque:"+string(rune('1'+i))] = ph
	}
	return h, old, nodes[0], id
}

// TestPlaceholderMoves: a placeholder sent back moves the original
// nodes (an object of a run into a new run with the run's properties),
// is left out of the insertion groups, and cannot appear twice; its
// text must be the content's; an unknown ref is a RefError.
func TestPlaceholderMoves(t *testing.T) {
	h, old, p, id := phFixture(t)
	sym := old.Elements()[0].Elements()[2]
	c, err := Inline(h, p, nil, atoms(t, `x<span data-field="AUTHOR" data-ref="0000000A/opaque:2" contenteditable="false">Ada</span><opaque data-ref="0000000a/opaque:1" data-kind="symbol"></opaque>y`), Options{Identity: id})
	if err != nil {
		t.Fatal(err)
	}
	want := `<w:p><w:r><w:t>x</w:t></w:r><w:fldSimple w:instr="AUTHOR"><w:r><w:t>Ada</w:t></w:r></w:fldSimple><w:r><w:rPr><w:b/></w:rPr><w:sym w:font="Symbol" w:char="F020"/></w:r><w:r><w:t>y</w:t></w:r></w:p>`
	if got := string(p.Bytes()); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if sym.Parent() == old.Elements()[0] {
		t.Error("the symbol was copied, not moved")
	}
	if len(c.Groups) != 2 || len(c.Groups[0]) != 1 || len(c.Groups[1]) != 1 {
		t.Errorf("groups %v", c.Groups)
	}

	h, _, p, id = phFixture(t)
	_, err = Inline(h, p, nil, atoms(t, `<opaque data-ref="0000000A/opaque:1"></opaque><opaque data-ref="0000000A/opaque:1"></opaque>`), Options{Identity: id})
	if !errors.Is(err, ErrContent) {
		t.Errorf("twice: %v", err)
	}
	h, _, p, id = phFixture(t)
	_, err = Inline(h, p, nil, atoms(t, `<span data-field="AUTHOR" data-ref="0000000A/opaque:2">Bob</span>`), Options{Identity: id})
	if err == nil || !strings.Contains(err.Error(), "not editable") {
		t.Errorf("edited field text: %v", err)
	}
	var ref *RefError
	h, _, p, id = phFixture(t)
	if _, err := Inline(h, p, nil, atoms(t, `<opaque data-ref="0000000A/opaque:3"></opaque>`), Options{Identity: id}); !errors.As(err, &ref) {
		t.Errorf("unknown ref: %v", err)
	}
	if _, err := Inline(h, p, nil, atoms(t, `<opaque data-ref="0000000A/opaque:1"></opaque>`), Options{}); !errors.As(err, &ref) {
		t.Errorf("no resolver: %v", err)
	}
}
