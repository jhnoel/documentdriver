package span

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

const (
	nsW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsM = "http://schemas.openxmlformats.org/officeDocument/2006/math"
)

// docOf is a main document part with body paragraphs.
func docOf(paras ...string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="` + nsW + `" xmlns:m="` + nsM + `" xmlns:mc="` + model.NSMC + `"` +
		` xmlns:w14="` + model.NSW14 + `" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		strings.Join(paras, "") + `</w:body></w:document>`
}

// packageOf zips a minimal docx around a main document part.
func packageOf(t testing.TB, document string, extra ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="` + ctypes.CTPackageRelationships + `"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="` + ctypes.CTWordprocessingmlDocumentMain + `"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="` + ctypes.RelOfficeDocument + `" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", document},
	} {
		if err := add(z, f.name, f.body); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range extra {
		if err := add(z, f[0], f[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// add stores one member uncompressed.
func add(z *zip.Writer, name, body string) error {
	w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(body))
	return err
}

// workspace opens a docx held in memory for editing and returns its body
// paragraphs (story paragraphs of the main document).
func workspace(t testing.TB, b []byte) (*edit.Workspace, []*tree.Node) {
	t.Helper()
	pkg, err := opc.OpenBytes(b, opc.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	w, err := edit.New(pkg, edit.Options{Source: sha256.Sum256(b)})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := w.Tree(w.DocumentPart())
	if err != nil {
		t.Fatal(err)
	}
	body := model.ChildW(doc.Root(), "body")
	var out []*tree.Node
	for _, p := range model.ParagraphsIn(body) {
		out = append(out, p.Node)
	}
	return w, out
}

// one opens a document of one paragraph p.
func one(t testing.TB, p string) (*edit.Workspace, *tree.Node) {
	t.Helper()
	w, ps := workspace(t, packageOf(t, docOf(p)))
	if len(ps) != 1 {
		t.Fatalf("%d paragraphs", len(ps))
	}
	return w, ps[0]
}

// projection is what a paragraph shows, independent of how its content is
// divided into runs: every character with its leaf name, run properties
// and wrappers, and every marker with its bytes and wrappers, in document
// order.
func projection(p *tree.Node) []string {
	var out []string
	var walk func(n *tree.Node, ctx string)
	walk = func(n *tree.Node, ctx string) {
		for _, c := range n.Children() {
			if !model.IsElement(c) {
				continue
			}
			switch {
			case model.IsW(c, "r"):
				rpr := ""
				if r := model.ChildW(c, "rPr"); r != nil {
					rpr = string(r.Bytes())
				}
				walk(c, ctx+"|run"+rpr)
				continue
			case model.IsLocal(c, "AlternateContent"):
				for _, b := range c.Children() {
					if model.IsElement(b) && b != model.AlternateContentBranch(c) {
						out = append(out, "alt:"+string(b.Bytes())+ctx)
					}
				}
				if b := model.AlternateContentBranch(c); b != nil {
					walk(b, ctx+"|"+string(c.StartTag()))
				}
				continue
			}
			if text, _, ok := model.LeafText(c); ok {
				for _, r := range text {
					out = append(out, string(r)+" "+c.Local()+ctx)
				}
				if model.Classify(c) == model.KindObject {
					out = append(out, "object:"+string(c.Bytes()))
				}
				continue
			}
			switch model.Classify(c) {
			case model.KindProps:
				if !model.IsW(c, "rPr") {
					out = append(out, "props:"+string(c.Bytes())+ctx)
				}
			case model.KindMarker, model.KindStory:
				out = append(out, "marker:"+string(c.Bytes())+ctx)
			default:
				walk(c, ctx+"|"+string(c.StartTag()))
			}
		}
	}
	walk(p, "")
	return out
}

// unitText is the all-view text of the segments under the units.
func unitText(t testing.TB, p *Para, units []*tree.Node) string {
	t.Helper()
	var b strings.Builder
	for _, s := range p.Segs {
		for _, u := range units {
			if model.Within(s.Leaf, u) {
				b.WriteString(s.Text)
				break
			}
		}
	}
	return b.String()
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }
