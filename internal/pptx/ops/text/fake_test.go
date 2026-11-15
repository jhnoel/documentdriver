package text

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

const nsDecl = `xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
	`xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main"`

// sp returns a shape element holding body (a p:txBody, or "" for none).
func sp(body string) string {
	return `<p:sp ` + nsDecl + `><p:nvSpPr><p:cNvPr id="2" name="Box"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/>` + body + `</p:sp>`
}

// fake implements Object over one document per branch, editing by
// splicing bytes and re-parsing, so that tests need no tree mutators.
type fake struct {
	t        testing.TB
	docs     []*tree.Document
	replaces int
}

func newFake(t testing.TB, branches ...string) *fake {
	t.Helper()
	f := &fake{t: t}
	for _, b := range branches {
		d, err := tree.Parse([]byte(b))
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
		f.docs = append(f.docs, d)
	}
	return f
}

func (f *fake) Branches() int         { return len(f.docs) }
func (f *fake) Elem(i int) *tree.Node { return f.docs[i].Root() }
func (f *fake) xml(i int) string      { return string(f.docs[i].Bytes()) }
func (f *fake) body(i int) *tree.Node { return txBody(f.Elem(i)) }
func (f *fake) html(i int) string     { return Render(ReadBody(f.body(i))) }
func (f *fake) reparse(i int, b []byte) error {
	d, err := tree.Parse(b)
	if err != nil {
		return err
	}
	f.docs[i] = d
	return nil
}

// Ensure supports what Set asks for: p:txBody on a p:sp and a:bodyPr in
// it.
func (f *fake) Ensure(parent edit.Path, c edit.Name) error {
	for i, d := range f.docs {
		src := d.Bytes()
		switch {
		case len(parent) == 0 && c.Local() == "txBody":
			if f.body(i) != nil {
				continue
			}
			end := bytes.LastIndex(src, []byte("</p:sp>"))
			if err := f.reparse(i, splice(src, end, end, "<p:txBody/>")); err != nil {
				return err
			}
		case len(parent) == 1 && c.Local() == "bodyPr":
			b := f.body(i)
			if derive.Child(b, derive.DML, "bodyPr") != nil {
				continue
			}
			s, e, _ := b.Span()
			if err := f.reparse(i, splice(src, s, e, "<p:txBody><a:bodyPr/></p:txBody>")); err != nil {
				return err
			}
		default:
			return fmt.Errorf("fake: Ensure %v %s not supported", parent, c)
		}
	}
	return nil
}

func splice(src []byte, s, e int, ins string) []byte {
	out := append([]byte{}, src[:s]...)
	out = append(out, ins...)
	return append(out, src[e:]...)
}

func (f *fake) ReplaceParagraphs(frags [][]byte) error {
	f.replaces++
	if len(frags) != len(f.docs) {
		return fmt.Errorf("fake: %d fragments for %d branches", len(frags), len(f.docs))
	}
	for i, frag := range frags {
		if frag == nil {
			continue
		}
		if err := f.reparse(i, spliceParagraphs(f.docs[i].Bytes(), f.body(i), frag)); err != nil {
			return fmt.Errorf("fake: result does not parse: %w", err)
		}
	}
	return nil
}
