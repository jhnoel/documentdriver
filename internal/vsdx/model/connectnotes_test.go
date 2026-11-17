package model

import (
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
)

// TestConnectNotesMatchBuild checks the notes pass VisitPage uses (RM2)
// against BuildPage on the fixtures with Connects: given the shapes
// BuildPage built, connectNotes gives exactly BuildPage's "connect from
// sheet names no shape" notes, in order.
func TestConnectNotesMatchBuild(t *testing.T) {
	for _, name := range []string{"v5-glue", "v10-defects"} {
		d, r := openFixture(t, name)
		e, ok := d.Page(0)
		if !ok {
			t.Fatalf("%s: no page 0", name)
		}
		built, err := BuildPage(d, r, e, Options{})
		if err != nil {
			t.Fatalf("BuildPage(%s): %v", name, err)
		}
		var want []doc.Note
		for _, n := range built.Notes {
			if n == noShapeNote(built) {
				want = append(want, n)
			}
		}
		if name == "v10-defects" && len(want) == 0 {
			t.Fatalf("%s: BuildPage gave no connect note; the row needs one", name)
		}
		ids := map[uint32]bool{}
		for _, o := range built.Objects {
			ids[o.ShapeID] = true
		}
		contents, err := d.PageContents(e)
		if err != nil {
			t.Fatalf("PageContents(%s): %v", name, err)
		}
		_, connects, _ := directChildren(contents.Root())
		got := &Page{ID: built.ID}
		connectNotes(got, connects, func(id uint32) bool { return ids[id] })
		if !slices.Equal(got.Notes, want) {
			t.Errorf("%s: connectNotes = %v, want BuildPage's %v", name, got.Notes, want)
		}
	}
}

// TestConnectNotesRows checks connectNotes on test-built rows: end and
// control-handle rows naming no shape are notes, a row without
// FromCell and a row naming a built shape are not.
func TestConnectNotesRows(t *testing.T) {
	parsed, err := tree.ParseString(`<Connects>` +
		`<Connect FromSheet='0' FromCell='BeginX' ToSheet='7' ToCell='PinX'/>` +
		`<Connect FromSheet='9' FromCell='EndX' ToSheet='7' ToCell='PinX'/>` +
		`<Connect FromSheet='99' FromCell='Controls.Row_1' ToSheet='7' ToCell='PinX'/>` +
		`<Connect FromSheet='x' FromCell='EndX' ToSheet='7' ToCell='PinX'/>` +
		`<Connect FromSheet='99' ToSheet='7' ToCell='PinX'/>` +
		`<Other FromSheet='99' FromCell='EndX'/>` +
		`</Connects>`)
	if err != nil {
		t.Fatal(err)
	}
	p := &Page{ID: "page0"}
	connectNotes(p, parsed.Root(), func(id uint32) bool { return id == 9 })
	if len(p.Notes) != 3 {
		t.Errorf("connectNotes = %v, want 3 notes (FromSheet 0, 99 on a control handle, unparsable)", p.Notes)
	}
	connectNotes(nil, parsed.Root(), nil)
	connectNotes(p, nil, nil)
}
