package plan

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// splicedSlide patches slide1 by replacing its first <a:off .../> element
// and returns the patch and that element's span.
func splicedSlide(t *testing.T, w *toyWorkspace) (*splice.Patch, splice.Span) {
	t.Helper()
	pt, err := w.pkg.Part(slide1)
	if err != nil {
		t.Fatal(err)
	}
	orig, _, err := pt.OriginalString()
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Index(orig, "<a:off ")
	if s < 0 {
		t.Fatal("no a:off in the fixture")
	}
	e := s + strings.Index(orig[s:], "/>") + 2
	p := splice.New(orig)
	if err := p.Replace(s, e, `<a:off x="1" y="2"/>`); err != nil {
		t.Fatal(err)
	}
	if err := pt.SetPatch(p); err != nil {
		t.Fatal(err)
	}
	return p, splice.Span{Start: s, End: e}
}

// A spliced part is verified by its spans, with no tree: changes inside
// the declared spans commit, the slide's units are not compared (its
// tree is never built), and the saved part is the patch's result.
func TestDeclareSplicedCommits(t *testing.T) {
	for _, c := range []struct {
		name    string
		declare func(sp splice.Span) []splice.Span
	}{
		{"exact", func(sp splice.Span) []splice.Span { return []splice.Span{sp} }},
		{"wider", func(sp splice.Span) []splice.Span { return []splice.Span{{Start: sp.Start - 5, End: sp.End + 5}} }},
		{"touching halves", func(sp splice.Span) []splice.Span {
			mid := (sp.Start + sp.End) / 2
			return []splice.Span{{Start: mid, End: sp.End}, {Start: sp.Start, End: mid}}
		}},
		{"overlapping, one long", func(sp splice.Span) []splice.Span {
			return []splice.Span{{Start: sp.Start - 1, End: sp.End + 1}, {Start: sp.Start, End: sp.Start + 1}}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx, w, path, _, _ := begin(t)
			var want string
			if err := tx.Run("splice", func() error {
				p, sp := splicedSlide(t, w)
				want = p.String()
				w.ws.DeclareSpliced(slide1, c.declare(sp)...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := w.Verify(); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			pkg, err := opc.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer pkg.Close()
			pt, err := pkg.Part(slide1)
			if err != nil {
				t.Fatal(err)
			}
			if b, err := pt.Bytes(); err != nil || string(b) != want {
				t.Fatalf("saved part is not the patch's result: %v", err)
			}
		})
	}
}

// An insertion point at either end of a declared span is inside it.
func TestDeclareSplicedInsertionAtEnds(t *testing.T) {
	tx, w, _, _, _ := begin(t)
	_, sp := splicedSlide(t, w)
	pt, err := w.pkg.Part(slide1)
	if err != nil {
		t.Fatal(err)
	}
	p := pt.Patch()
	if err := p.Replace(sp.End, sp.End, `<a:ext cx="1" cy="1"/>`); err != nil {
		t.Fatal(err)
	}
	w.ws.DeclareSpliced(slide1, sp)
	if err := w.Verify(); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
}

// A change outside the declared spans is refused, and so is a spliced
// part changed other than by its patch; nothing is written.
func TestDeclareSplicedRefuses(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(t *testing.T, w *toyWorkspace)
	}{
		{"outside", func(t *testing.T, w *toyWorkspace) {
			_, sp := splicedSlide(t, w)
			w.ws.DeclareSpliced(slide1, splice.Span{Start: 0, End: sp.Start})
		}},
		{"straddles the end", func(t *testing.T, w *toyWorkspace) {
			_, sp := splicedSlide(t, w)
			w.ws.DeclareSpliced(slide1, splice.Span{Start: sp.Start, End: sp.End - 1})
		}},
		{"gap between spans", func(t *testing.T, w *toyWorkspace) {
			_, sp := splicedSlide(t, w)
			w.ws.DeclareSpliced(slide1, splice.Span{Start: sp.Start, End: sp.Start + 2}, splice.Span{Start: sp.Start + 3, End: sp.End})
		}},
		{"no spans", func(t *testing.T, w *toyWorkspace) {
			splicedSlide(t, w)
			w.ws.DeclareSpliced(slide1)
		}},
		{"changed by tree", func(t *testing.T, w *toyWorkspace) {
			sh := shapes(t, w)
			if err := sh[0].SetAttr("spliced", "no"); err != nil {
				t.Fatal(err)
			}
			w.ws.DeclareSpliced(slide1, splice.Span{Start: 0, End: 1 << 30})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx, w, path, b, mtime := begin(t)
			if err := tx.Run(c.name, func() error { c.run(t, w); return nil }); err != nil {
				t.Fatal(err)
			}
			if err := w.Verify(); !errors.Is(err, ErrUndeclared) {
				t.Fatalf("Verify: %v, want ErrUndeclared", err)
			}
			if err := tx.Commit(); !errors.Is(err, ErrUndeclared) {
				t.Fatalf("Commit: %v, want ErrUndeclared", err)
			}
			assertUntouched(t, path, b, mtime)
		})
	}
}
