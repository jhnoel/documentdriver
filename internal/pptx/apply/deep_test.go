package apply

import (
	"fmt"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

// TestDeepGroups edits the innermost shape of a slide nested as deep as
// the tokenizer allows: the workspace resolver, the commit-time verifier
// and the result's read model all walk the group hierarchy.
func TestDeepGroups(t *testing.T) {
	path := testdeck.MinimalWith(t, map[string][]byte{"ppt/slides/slide1.xml": testdeck.DeepGroupsSlide(tree.MaxDepth)})
	id := testdeck.DeepGroupsShapeID(tree.MaxDepth)
	plan := fmt.Sprintf(`{"ops": [
		{"op": "object.set", "at": "slide1/s%d", "rot": 15},
		{"op": "object.move", "at": "slide1/s%d", "dx": 10}
	]}`, id, id-1)
	res, err := Run(path, []byte(plan), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != path {
		t.Fatalf("saved to %q", res.Saved)
	}
	s, err := readSlide(path, "slide1")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.Objects); n != tree.MaxDepth-8+1 {
		t.Fatalf("%d objects after the edit", n)
	}
}
