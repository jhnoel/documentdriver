package view

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/repo"
)

// fixtures are the repository's docx fixtures and, when DOCX_CORPUS is
// set, the corpus documents, opened; a document that does not open is
// skipped.
func fixtures(t *testing.T) map[string]*model.Document {
	t.Helper()
	root, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	globs := []string{
		filepath.Join(root, "testdata", "docx", "*.docx"),
		filepath.Join(root, "tools", "oracle", "testdata", "write", "*.docx"),
	}
	if dir := os.Getenv("DOCX_CORPUS"); dir != "" {
		globs = append(globs, filepath.Join(dir, "*", "*.docx"))
	}
	out := map[string]*model.Document{}
	for _, g := range globs {
		paths, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			d, err := model.OpenDocument(b)
			if err != nil {
				continue
			}
			out[p] = d
		}
	}
	if len(out) == 0 {
		t.Fatal("no fixtures")
	}
	return out
}

// referenceRunFormat is runFormat without memoisation: the resolution
// the value-keyed cache must reproduce.
func referenceRunFormat(s *Styles, run *tree.Node, original bool) fmtState {
	m := map[string]string{}
	s.defaults(true, m)
	for p := run.Parent(); p != nil; p = p.Parent() {
		if model.IsW(p, "p") {
			id, has := s.paragraphStyle(p, original)
			s.applyChain(id, has, true, m)
			break
		}
	}
	if rpr := model.PropertyBag(run, "rPr", original); rpr != nil {
		if rs := model.ChildW(rpr, "rStyle"); rs != nil {
			if v, ok := model.Attr(rs, "val"); ok {
				s.applyChain(v, true, true, m)
			}
		}
		collectCSS(rpr, true, m)
	}
	f, err := applyInlineCSS(fmtState{}, cssString(m))
	if err != nil {
		f = fmtState{}
	}
	return f
}

// referenceInheritable is the inheritable run CSS of formatting f as
// the projection defines it: inlineCSS(f) parsed back by declarations,
// filtered to the inheritable properties, without color under a link.
func referenceInheritable(f fmtState, hasLink bool) map[string]string {
	decls, err := declarations(inlineCSS(f))
	if err != nil {
		decls = map[string]string{}
	}
	maps.DeleteFunc(decls, func(k, _ string) bool { return !inheritableRunCSS[k] })
	if hasLink {
		delete(decls, "color")
	}
	return decls
}

// TestRunFormatMemo checks the cached, typed run formatting against the
// CSS round trip it replaces for every run of every fixture part, in
// both property views, and the typed inheritable formatting against
// its CSS definition.
func TestRunFormatMemo(t *testing.T) {
	for path, d := range fixtures(t) {
		s := LoadStyles(d.Pkg)
		for _, part := range d.Pkg.Parts() {
			name := part.Name
			if !strings.HasSuffix(name, ".xml") {
				continue
			}
			x, ok := d.Pkg.Part(name)
			if !ok || x.Root() == nil {
				continue
			}
			x.Root().Walk(func(n *tree.Node) bool {
				if !model.IsW(n, "r") {
					return true
				}
				for _, original := range []bool{false, true} {
					got, want := s.runFormat(n, original), referenceRunFormat(s, n, original)
					if got != want {
						t.Errorf("%s %s: run format %+v, want %+v", path, name, got, want)
						return false
					}
					for _, link := range []bool{false, true} {
						m := map[string]string{}
						inheritable(got, link).declare(m)
						if ref := referenceInheritable(got, link); !maps.Equal(m, ref) {
							t.Errorf("%s %s: inheritable %v, want %v", path, name, m, ref)
							return false
						}
					}
				}
				return true
			})
		}
	}
}

// TestRunFormatEdges checks the typed resolution on the properties the
// CSS round trip refuses or drops: sizes outside 0.5pt to 100000pt and
// font names that are empty, unsafe or "unset".
func TestRunFormatEdges(t *testing.T) {
	var body strings.Builder
	for _, rpr := range []string{
		`<w:sz w:val="0"/>`, `<w:sz w:val="1"/>`, `<w:sz w:val="200000"/>`, `<w:sz w:val="200001"/>`,
		`<w:rFonts w:ascii="unset"/>`, `<w:rFonts w:ascii="UnSet"/>`, `<w:rFonts w:ascii="a!b"/>`,
		`<w:rFonts w:ascii=" "/>`, `<w:rFonts w:ascii="&quot; x &quot;"/>`, `<w:rFonts w:ascii="&quot;&quot;"/>`,
		`<w:rFonts w:ascii="&quot;unset&quot;"/>`, `<w:rFonts w:ascii="URL(x)"/>`, `<w:rFonts w:hAnsi="H"/>`,
		`<w:u w:val="single"/><w:strike/><w:u w:val="none"/>`, `<w:strike w:val="0"/>`, `<w:vertAlign w:val="x"/>`,
		`<w:color w:val="abcdef"/><w:highlight w:val="nope"/><w:highlight w:val="yellow"/><w:b w:val="off"/><w:i/>`,
	} {
		body.WriteString(`<w:p><w:r><w:rPr>` + rpr + `</w:rPr><w:t>x</w:t></w:r><w:r><w:t>y</w:t></w:r></w:p>`)
	}
	d := build(t, map[string]string{"word/document.xml": docHead + body.String() + `</w:body></w:document>`})
	s := LoadStyles(d.Pkg)
	n := 0
	d.XML.Root().Walk(func(r *tree.Node) bool {
		if !model.IsW(r, "r") {
			return true
		}
		n++
		got, want := s.runFormat(r, false), referenceRunFormat(s, r, false)
		if got != want {
			t.Errorf("%s: %+v, want %+v", r.Bytes(), got, want)
		}
		m := map[string]string{}
		inheritable(got, false).declare(m)
		if ref := referenceInheritable(got, false); !maps.Equal(m, ref) {
			t.Errorf("%s: inheritable %v, want %v", r.Bytes(), m, ref)
		}
		return true
	})
	if n != 34 {
		t.Fatalf("%d runs", n)
	}
}
