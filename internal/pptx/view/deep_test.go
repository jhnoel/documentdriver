package view

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/pptx/model"
	"github.com/jhnoel/documentdriver/internal/testdeck"
)

const slide1 = "ppt/slides/slide1.xml"

// TestDeepGroups reads a slide nested as deep as the tokenizer allows
// (about 500 groups, each inside the last) through the read model and
// both views, which walk groups recursively; one level deeper is a read
// error, not a crash.
func TestDeepGroups(t *testing.T) {
	path := testdeck.MinimalWith(t, map[string][]byte{slide1: testdeck.DeepGroupsSlide(tree.MaxDepth)})
	s := readSlide1(t, path)
	if s == nil {
		t.Fatal("deepest slide not read")
	}
	groups := tree.MaxDepth - 8
	if len(s.Objects) != groups+1 {
		t.Fatalf("%d objects, want %d", len(s.Objects), groups+1)
	}
	for _, o := range allOptions {
		if msg := equivalent(s, o); msg != "" {
			t.Fatalf("%+v: %s", o, msg)
		}
	}

	path = testdeck.MinimalWith(t, map[string][]byte{slide1: testdeck.DeepGroupsSlide(tree.MaxDepth + 1)})
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ReadSlide(d.Slides[0]); err == nil || !strings.Contains(err.Error(), "nested more than") {
		t.Fatalf("too deep slide: err = %v", err)
	}
}

func readSlide1(t *testing.T, path string) *model.Slide {
	t.Helper()
	p, err := opc.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	d, err := model.OpenDeck(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.ReadSlide(d.Slides[0])
	if err != nil {
		t.Fatal(err)
	}
	return s
}
