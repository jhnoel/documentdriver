package build

import (
	"errors"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Options are one lowering's settings.
type Options struct {
	// EquationRunProperties is the w:rPr (w prefix) every run of a new
	// equation carries, the formatting template of the content it joins
	// (math.RunProperties of a replaced equation); "" for none.
	EquationRunProperties string
	// Identity resolves items that carry an existing identity (an
	// equation's data-ref). Nil: there is no content being replaced, and
	// every data-ref is a RefError.
	Identity Resolver
	// ComplexFields writes a field as the complex field runs (begin,
	// instruction, separate, result, end) rather than a w:fldSimple: a
	// tracked insertion holds runs, so a tracked field is those runs
	// (docxdriver's expand_simple_fields), and rejecting leaves no empty
	// w:fldSimple behind for Word to recompute.
	ComplexFields bool
	// Link returns the relationship id of an external hyperlink to href
	// from the part being built (the caller allocates it). Nil: links are
	// refused.
	Link func(href string) (string, error)
	// Image adds the media part of an image's data: URL, related from
	// the part being built (the caller allocates it and dedupes), and
	// gives the picture's relationship id, drawing id and own size. Nil:
	// images are refused.
	Image func(src string) (Picture, error)
}

// Content is what Inline placed.
type Content struct {
	// Nodes are the top-level elements placed in the paragraph, in order:
	// runs, hyperlinks, simple fields and equations.
	Nodes []*tree.Node
	// Groups are runs of consecutive sibling elements (runs and equations)
	// that one revision wrapper may hold, placeholders (existing content,
	// moved) left out: the content placed directly in
	// the paragraph, and the content of each hyperlink or field, which a
	// w:ins cannot hold whole. A tracking operation wraps each in its own
	// w:ins (revise.Reviser.Insert).
	Groups [][]*tree.Node
}

// nsW are the prefixes new fragments use; the edit core maps them to the
// family member the part declares.
var nsW = map[string]string{"w": model.NSW, "r": core.REL.URIs[0]}

// builder places one paragraph's content.
type builder struct {
	h   *core.Handle
	p   *tree.Node
	opt Options
	out Content
	// moved are the placeholders already placed, by key, and kept the
	// nodes placing them put in the content (no revision wraps those).
	moved, kept map[*tree.Node]bool
}

// elem parses frag (one element, w and r prefixes) as a detached element
// for placement under at.
func (b *builder) elem(at *tree.Node, frag string) (*tree.Node, error) {
	nodes, err := b.h.Workspace().Parse(at, []byte(frag), nsW)
	if err != nil {
		return nil, err
	}
	if len(nodes) != 1 {
		return nil, errors.New("build: a fragment did not parse as one element")
	}
	return nodes[0], nil
}

// wq is the qualified name of WordprocessingML attribute local on e.
func wq(e *tree.Node, local string) string {
	if p, ok := e.LookupNamespace(e.Space()); ok && p != "" && docxedit.WML.Has(e) {
		return p + ":" + local
	}
	return "w:" + local
}

// Inline places atoms, one paragraph's inline content, in paragraph p
// (an element h's region holds) before its child before, or at its end
// when before is nil, and returns what it placed. Consecutive characters
// with the same formatting share a run and a w:t; a break or a tab rides
// in the run before it; a change of formatting starts a new run; content
// with a link goes into one w:hyperlink per link, content of a field
// (PAGE, NUMPAGES) into a w:fldSimple. Refusals of the content wrap
// ErrContent; an unknown data-ref is a RefError.
func Inline(h *core.Handle, p, before *tree.Node, atoms []html.Atom, opt Options) (Content, error) {
	b := &builder{h: h, p: p, opt: opt}
	for i := 0; i < len(atoms); {
		j := i + 1
		for j < len(atoms) && atoms[j].Link == atoms[i].Link && atoms[j].Field == atoms[i].Field {
			j++
		}
		if err := b.wrapper(before, atoms[i:j]); err != nil {
			return Content{}, err
		}
		i = j
	}
	return b.out, nil
}

// wrapper places atoms, which share a link and a field, in their wrapper
// elements (none, a hyperlink, a simple field, or both) before before.
func (b *builder) wrapper(before *tree.Node, atoms []html.Atom) error {
	container, at := b.p, before
	link, field := atoms[0].Link, atoms[0].Field
	place := func(n *tree.Node) error {
		if err := b.h.PlaceAt(container, n, at); err != nil {
			return err
		}
		if container == b.p {
			b.out.Nodes = append(b.out.Nodes, n)
		}
		return nil
	}
	if link.OK {
		if b.opt.Link == nil {
			return contentErr("links cannot be written here")
		}
		rid, err := b.opt.Link(link.V)
		if err != nil {
			return err
		}
		hl, err := b.elem(container, `<w:hyperlink/>`)
		if err != nil {
			return err
		}
		if err := place(hl); err != nil {
			return err
		}
		if err := b.h.SetNodeAttr(hl, relID(hl), rid); err != nil {
			return err
		}
		container, at = hl, nil
	}
	if field.OK && b.opt.ComplexFields {
		instr := strings.TrimSpace(field.V)
		if instr != "PAGE" && instr != "NUMPAGES" {
			return contentErr("only PAGE and NUMPAGES fields can be written")
		}
		return b.complexField(container, at, instr, atoms, place)
	}
	if field.OK {
		instr := strings.TrimSpace(field.V)
		if instr != "PAGE" && instr != "NUMPAGES" {
			return contentErr("only PAGE and NUMPAGES fields can be written")
		}
		fs, err := b.elem(container, `<w:fldSimple/>`)
		if err != nil {
			return err
		}
		if err := place(fs); err != nil {
			return err
		}
		// No w:dirty: a dirty field makes Word ask to update fields that
		// "may refer to other files" on open; PAGE and NUMPAGES are
		// refreshed at layout anyway, and the result run caches "1".
		for _, a := range [][2]string{{"instr", field.V}} {
			if err := b.h.SetNodeAttr(fs, wq(fs, a[0]), a[1]); err != nil {
				return err
			}
		}
		container, at = fs, nil
	}
	group, err := b.runs(container, at, atoms)
	if err != nil {
		return err
	}
	if container == b.p {
		b.out.Nodes = append(b.out.Nodes, group...)
	}
	var cur []*tree.Node
	for _, n := range group {
		if b.kept[n] {
			if len(cur) > 0 {
				b.out.Groups = append(b.out.Groups, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, n)
	}
	if len(cur) > 0 {
		b.out.Groups = append(b.out.Groups, cur)
	}
	return nil
}

// relID is the qualified name of the relationship id attribute on e: r:id
// with the prefix the part binds to the relationships namespace.
func relID(e *tree.Node) string {
	for _, uri := range core.REL.URIs {
		if p, ok := e.LookupNamespace(uri); ok && p != "" {
			return p + ":id"
		}
	}
	return "r:id"
}

// runs places atoms as runs and equations in container before at and
// returns them in order.
func (b *builder) runs(container, at *tree.Node, atoms []html.Atom) ([]*tree.Node, error) {
	var placed []*tree.Node
	var run *tree.Node
	var runFmt html.Fmt
	ensure := func(f html.Fmt) error {
		if run != nil && runFmt == f {
			return nil
		}
		r, err := b.elem(container, `<w:r/>`)
		if err != nil {
			return err
		}
		if err := b.h.PlaceAt(container, r, at); err != nil {
			return err
		}
		if err := runProperties(b.h, r, f); err != nil {
			return err
		}
		run, runFmt = r, f
		placed = append(placed, r)
		return nil
	}
	add := func(frag string) error {
		n, err := b.elem(run, frag)
		if err != nil {
			return err
		}
		return b.h.PlaceAt(run, n, nil)
	}
	for i := 0; i < len(atoms); i++ {
		a := atoms[i]
		switch a.Kind {
		case html.AtomChar:
			var text strings.Builder
			text.WriteString(a.Text)
			for i+1 < len(atoms) && atoms[i+1].Kind == html.AtomChar && atoms[i+1].Fmt == a.Fmt {
				i++
				text.WriteString(atoms[i].Text)
			}
			if err := ensure(a.Fmt); err != nil {
				return nil, err
			}
			if err := add(textElement(text.String())); err != nil {
				return nil, err
			}
		case html.AtomBr:
			if err := ensure(runFmt); err != nil {
				return nil, err
			}
			frag := `<w:br/>`
			if t := a.Br.DialectType(); t.OK {
				frag = `<w:br w:type="` + t.V + `"/>`
			}
			if err := add(frag); err != nil {
				return nil, err
			}
		case html.AtomTab:
			f := runFmt
			if run == nil {
				f = a.Fmt
			}
			if err := ensure(f); err != nil {
				return nil, err
			}
			if err := add(`<w:tab/>`); err != nil {
				return nil, err
			}
		case html.AtomEquation:
			eq, err := b.equation(container, a)
			if err != nil {
				return nil, err
			}
			if err := b.h.PlaceAt(container, eq, at); err != nil {
				return nil, err
			}
			placed = append(placed, eq)
			run = nil
		case html.AtomOpaque:
			nodes, err := b.placeholder(container, at, a)
			if err != nil {
				return nil, err
			}
			placed = append(placed, nodes...)
			run = nil
		case html.AtomImage:
			if b.opt.Image == nil {
				return nil, unsupported("img")
			}
			pic, err := b.opt.Image(a.Src)
			if err != nil {
				return nil, err
			}
			cx, cy, err := extent(a, pic)
			if err != nil {
				return nil, err
			}
			// A picture is a run of its own, with the run formatting
			// the HTML gives it.
			run = nil
			if err := ensure(a.Fmt); err != nil {
				return nil, err
			}
			if err := add(drawing(a, pic, cx, cy)); err != nil {
				return nil, err
			}
			run = nil
		case html.AtomNote:
			if a.Endnote {
				return nil, unsupported("endnote")
			}
			return nil, unsupported("footnote")
		case html.AtomMilestone:
			return nil, contentErr("comment markers are not content: comments are separate operations")
		}
	}
	return placed, nil
}

// textElement is a w:t holding text, with xml:space="preserve" when
// whitespace at its ends or a run of spaces inside would otherwise be
// open to collapsing.
func textElement(text string) string {
	esc := string(tree.EscapeText(text))
	if strings.HasPrefix(text, " ") || strings.HasSuffix(text, " ") || strings.Contains(text, "  ") {
		return `<w:t xml:space="preserve">` + esc + `</w:t>`
	}
	return `<w:t>` + esc + `</w:t>`
}

// complexField places a field as its complex field runs in container
// before at: begin (not dirty: see wrapper), the instruction,
// separate, the result's runs, end. They are one group, as the runs a
// tracked insertion holds.
func (b *builder) complexField(container, at *tree.Node, instr string, atoms []html.Atom, place func(*tree.Node) error) error {
	var group []*tree.Node
	char := func(frag string) error {
		r, err := b.elem(container, `<w:r>`+frag+`</w:r>`)
		if err != nil {
			return err
		}
		if err := place(r); err != nil {
			return err
		}
		group = append(group, r)
		return nil
	}
	if err := char(`<w:fldChar w:fldCharType="begin"/>`); err != nil {
		return err
	}
	if err := char(`<w:instrText xml:space="preserve"> ` + string(tree.EscapeText(instr)) + ` </w:instrText>`); err != nil {
		return err
	}
	if err := char(`<w:fldChar w:fldCharType="separate"/>`); err != nil {
		return err
	}
	result, err := b.runs(container, at, atoms)
	if err != nil {
		return err
	}
	if container == b.p {
		b.out.Nodes = append(b.out.Nodes, result...)
	}
	group = append(group, result...)
	if err := char(`<w:fldChar w:fldCharType="end"/>`); err != nil {
		return err
	}
	b.out.Groups = append(b.out.Groups, group)
	return nil
}
