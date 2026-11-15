// Package create builds a new document from document HTML (the contract's
// create): an authored blank template (template.go) whose body the build
// lowering fills, gated by integrity.GateCreated, so a created file has no
// fault at all.
//
// Scope: paragraphs, headings (h1-h6, a <p class> naming a template
// style), inline content (formatted runs, links, breaks, tabs,
// equations, page-number and page-count fields), tables (build's table
// lowering: spans, nested tables, the style read writes for a table, row
// or cell), lists (each <ol>/<ul> a numbering definition of its own),
// sections (<section/>, first-page) and their headers and footers
// (default, first, even), and images (data: URLs of PNG, JPEG or GIF,
// one media part per distinct image). read's whole-document markup is
// accepted unchanged. Notes are refused with invalid_operation naming
// the element.
package create

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/integrity"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
)

// Error is a refusal of the input: a contract error code and the path of
// what is at fault ("html", "html<table>", "html@class").
type Error struct {
	Code, Path, Msg string
}

// Error returns the message.
func (e *Error) Error() string { return e.Msg }

// refuse returns an invalid-operation error for path with a formatted message.
func refuse(path, format string, args ...any) error {
	return &Error{Code: "invalid_operation", Path: path, Msg: fmt.Sprintf(format, args...)}
}

// Build returns the package bytes of a new document whose body is the
// document HTML input. The same input gives the same bytes.
func Build(input string) ([]byte, error) { return BuildAt(input, nil) }

// BuildAt is Build that calls mark (when not nil) where it holds the most
// (the lowered paragraphs; the filled package; the gate's check), for the
// memory budget (laws.WithinMemory).
func BuildAt(input string, mark func(string)) ([]byte, error) {
	if mark == nil {
		mark = func(string) {}
	}
	sections, err := html.ParseDocument(input, math.LaTeXToOMML)
	if err != nil {
		return nil, htmlRefusal(err)
	}
	mark("create parsed")
	var secs []section
	for _, sec := range sections {
		if sec.BodyErr != nil {
			return nil, refuse("html", "%s", sec.BodyErr.Error())
		}
		blocks, err := build.Blocks(sec.Body)
		if err != nil {
			return nil, contentRefusal(err)
		}
		if err := checkStyles(blocks); err != nil {
			return nil, err
		}
		s := section{title: sec.TitlePage, blocks: blocks}
		for _, slot := range sec.Slots {
			path := "html<header>"
			if slot.Scope == "ftr" {
				path = "html<footer>"
			}
			if slot.Err != nil {
				return nil, refuse(path, "%s", slot.Err.Error())
			}
			sb, err := build.Blocks(slot.Items)
			if err != nil {
				return nil, contentRefusal(err)
			}
			if err := checkStyles(sb); err != nil {
				return nil, err
			}
			s.stories = append(s.stories, story{scope: slot.Scope, kind: slot.Kind, blocks: sb})
		}
		secs = append(secs, s)
	}
	mark("create lowered")
	total := 0
	for _, s := range secs {
		total += len(s.blocks)
	}
	if total == 0 {
		return nil, refuse("html", "create html produced no content: it needs at least one paragraph")
	}
	tpl, err := template()
	if err != nil {
		return nil, err
	}
	out, err := fill(tpl, input, secs, mark)
	if err != nil {
		return nil, err
	}
	if err := integrity.GateCreatedAt(out, mark); err != nil {
		return nil, &Error{Code: "invalid_output", Path: "html", Msg: err.Error()}
	}
	return out, nil
}

// section is one section of the input: its title-page flag, its body
// blocks and its headers and footers.
type section struct {
	title   bool
	blocks  []build.Block
	stories []story
}

// story is a header or footer of a section.
type story struct {
	scope, kind string
	blocks      []build.Block
}

// checkStyles refuses a class naming no style of the template, in
// paragraphs and table cells alike.
func checkStyles(blocks []build.Block) error {
	for _, b := range blocks {
		if b.Table != nil {
			for _, r := range b.Table.Rows {
				for _, c := range r.Cells {
					if err := checkStyles(c.Blocks); err != nil {
						return err
					}
				}
			}
			continue
		}
		if a := b.Para.Attrs; a.Class.OK && !templateStyles[a.Class.V] {
			return refuse("html@class", "class %q names no paragraph style of a new document", a.Class.V)
		}
	}
	return nil
}

// htmlRefusal maps a refusal of the HTML front end to the input's.
func htmlRefusal(err error) error {
	if _, ok := errors.AsType[*html.ImageSourceError](err); ok {
		return refuse("html<img>", "%s", err.Error())
	}
	return refuse("html", "%s", err.Error())
}

// contentRefusal maps a refusal of the build lowering to the input's.
func contentRefusal(err error) error {
	var el *build.ElementError
	if errors.As(err, &el) {
		return refuse("html<"+el.Element+">", "%s", err.Error())
	}
	var at *build.AttributeError
	if errors.As(err, &at) {
		return refuse("html@"+at.Attr, "%s", err.Error())
	}
	if errors.Is(err, build.ErrContent) {
		return refuse("html", "%s", err.Error())
	}
	return err
}

// fill places the sections in the template's body and returns the
// package: each section's paragraphs, a copy of the template's section
// properties ending every section but the last (on its last paragraph,
// an empty one when it has none), its headers and footers as new parts
// referenced from its section properties, w:titlePg for a first-page
// section, and the document's even and odd headers when a story is even.
func fill(tpl []byte, input string, secs []section, mark func(string)) ([]byte, error) {
	pkg, err := opc.OpenBytes(tpl, opc.DefaultLimits())
	if err != nil {
		return nil, err
	}
	ws, err := docxedit.New(pkg, docxedit.Options{Source: sha256.Sum256([]byte(input))})
	if err != nil {
		return nil, err
	}
	part := ws.DocumentPart()
	doc, err := ws.Tree(part)
	if err != nil {
		return nil, err
	}
	body := (&model.XML{Doc: doc}).Body()
	final := model.ChildW(body, "sectPr")
	h := ws.Core().Story(part, "create", body)
	lists := &build.Lists{New: build.NewListIn(ws)}
	even := false
	pics := newMedia(ws)
	opt := build.Options{Link: linker(ws, part), Image: pics.in(part)}
	for i, sec := range secs {
		last, err := placeBlocks(h, body, final, sec.blocks, lists, opt, i+1 < len(secs) || len(sec.blocks) > 0, func(j int) {
			if i == len(secs)/2 && j == len(sec.blocks)/2 {
				mark("create placing") // half placed, half to place
			}
		})
		if err != nil {
			return nil, err
		}
		sect := final
		if i+1 < len(secs) {
			cp, err := h.Workspace().Copy(final)
			if err != nil {
				return nil, err
			}
			ppr, err := h.EnsureIn(last, docxedit.W("pPr"))
			if err != nil {
				return nil, err
			}
			if err := h.PlaceNew(ppr, cp); err != nil {
				return nil, err
			}
			sect = cp
		}
		if sec.title {
			if _, err := h.EnsureIn(sect, docxedit.W("titlePg")); err != nil {
				return nil, err
			}
		}
		for _, st := range sec.stories {
			if st.kind == "even" {
				even = true
			}
			if err := addStory(ws, h, sect, st, lists, pics); err != nil {
				return nil, err
			}
		}
	}
	if even {
		if err := evenAndOdd(ws); err != nil {
			return nil, err
		}
	}
	// Finish gives every new paragraph its id.
	if err := ws.Finish(); err != nil {
		return nil, err
	}
	if err := ws.Verify(); err != nil {
		return nil, err
	}
	mark("create filled")
	var b bytes.Buffer
	if err := pkg.WriteZip(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// placeBlocks builds blocks in container before at and returns the last
// paragraph placed at the top level. With needLast, a container whose
// blocks are none, or end in a table, gets an empty paragraph after them
// (a section break rides on a paragraph). Each block's input is dropped
// once it is in the tree, so the lowered input and the tree it becomes
// are not held whole at once; progress is called before each block.
func placeBlocks(h *core.Handle, container, at *tree.Node, blocks []build.Block, lists *build.Lists, opt build.Options, needLast bool, progress func(int)) (*tree.Node, error) {
	para := func(h *core.Handle, container, at *tree.Node, p build.Para) error {
		_, err := place(h, container, at, p, lists, opt)
		return err
	}
	var last *tree.Node
	for j := range blocks {
		progress(j)
		b := blocks[j]
		blocks[j] = build.Block{}
		if b.Table != nil {
			if _, err := build.PlaceTable(h, container, at, b.Table, para); err != nil {
				return nil, contentRefusal(err)
			}
			last = nil
			continue
		}
		p, err := place(h, container, at, *b.Para, lists, opt)
		if err != nil {
			return nil, err
		}
		last = p
	}
	if last == nil && needLast {
		return place(h, container, at, build.Para{Attrs: html.ParaAttrs{Tag: "p"}}, lists, opt)
	}
	return last, nil
}

// place builds one paragraph in container before at.
func place(h *core.Handle, container, at *tree.Node, para build.Para, lists *build.Lists, opt build.Options) (*tree.Node, error) {
	nodes, err := h.Workspace().Parse(container, []byte(`<w:p/>`), map[string]string{"w": model.NSW})
	if err != nil || len(nodes) != 1 {
		return nil, errors.Join(errors.New("create: a new paragraph did not parse"), err)
	}
	p := nodes[0]
	if err := h.PlaceAt(container, p, at); err != nil {
		return nil, err
	}
	if err := build.Properties(h, p, para.Attrs); err != nil {
		return nil, contentRefusal(err)
	}
	if err := lists.Apply(h, p, para.Attrs); err != nil {
		return nil, contentRefusal(err)
	}
	atoms, err := fresh(para.Atoms)
	if err != nil {
		return nil, err
	}
	if _, err := build.Inline(h, p, nil, atoms, opt); err != nil {
		return nil, contentRefusal(err)
	}
	return p, nil
}

// fresh is a paragraph's atoms as new content: read's markup carries the
// addresses of the document it was read from, which a new document does
// not have. An equation's data-ref is dropped (the equation is built from
// its LaTeX, as table.insert's cells do); a placeholder, content the HTML
// cannot express, is refused, since there is nothing to copy it from.
func fresh(atoms []html.Atom) ([]html.Atom, error) {
	for i, a := range atoms {
		switch {
		case a.Kind == html.AtomOpaque:
			return nil, refuse("html", "data-ref %s is a placeholder for content of the document read showed it from (%s); a new document cannot copy it: remove the element, or write the content in HTML", a.Ref, placeholderKind(a))
		case a.Kind == html.AtomEquation && a.Ref != "":
			// The atoms are this lowering's own, dropped once placed.
			obj := *a.AtomObject
			obj.Ref = ""
			atoms[i].AtomObject = &obj
		}
	}
	return atoms, nil
}

// placeholderKind names what a placeholder stands for, for a refusal.
func placeholderKind(a html.Atom) string {
	if a.PlaceholderKind != "" {
		return a.PlaceholderKind
	}
	return "an object"
}

// linker returns the hyperlink allocator of part: the part's existing
// external hyperlinks by target, then each one added, so a link costs
// the same however many came before.
func linker(ws *docxedit.Workspace, part string) func(string) (string, error) {
	var links map[string]string
	return func(href string) (string, error) {
		rels, err := ws.Package().Rels(part)
		if err != nil {
			return "", err
		}
		if links == nil {
			links = map[string]string{}
			for _, rel := range rels.All() {
				if _, seen := links[rel.Target]; !seen && ctypes.Is(rel.Type, ctypes.RelHyperlink) && rel.External() {
					links[rel.Target] = rel.ID
				}
			}
		}
		if id, ok := links[href]; ok {
			return id, nil
		}
		ws.Core().DeclarePart(rels.Name())
		id, err := rels.Add(ctypes.RelHyperlink, href, true)
		if err == nil {
			links[href] = id
		}
		return id, err
	}
}

// addStory writes a header or footer as a new part referenced from sect.
func addStory(ws *docxedit.Workspace, h *core.Handle, sect *tree.Node, st story, lists *build.Lists, pics *media) error {
	word, rel, ct, ref := "header", ctypes.RelHeader, ctypes.CTWordprocessingmlHeader, "headerReference"
	if st.scope == "ftr" {
		word, rel, ct, ref = "footer", ctypes.RelFooter, ctypes.CTWordprocessingmlFooter, "footerReference"
	}
	name := ws.PartName("word", word, ".xml")
	empty := xmlDecl + `<w:` + st.scope + ` xmlns:w="` + model.NSW + `" xmlns:r="` + core.REL.URIs[0] + `"/>`
	rid, err := ws.AddPart(rel, ct, name, empty)
	if err != nil {
		return err
	}
	nodes, err := h.Workspace().Parse(sect, []byte(`<w:`+ref+` w:type="`+st.kind+`" r:id="`+rid+`"/>`),
		map[string]string{"w": model.NSW, "r": core.REL.URIs[0]})
	if err != nil || len(nodes) != 1 {
		return errors.Join(errors.New("create: a story reference did not parse"), err)
	}
	var at *tree.Node
	for _, c := range sect.Elements() {
		if !model.IsW(c, "headerReference") && !model.IsW(c, "footerReference") {
			at = c
			break
		}
	}
	if err := h.PlaceAt(sect, nodes[0], at); err != nil {
		return err
	}
	doc, err := ws.Tree(name)
	if err != nil {
		return err
	}
	root := doc.Root()
	sh := ws.Core().Story(name, "create "+word, root)
	_, err = placeBlocks(sh, root, nil, st.blocks, lists, build.Options{Link: linker(ws, name), Image: pics.in(name)}, true, func(int) {})
	return err
}

// evenAndOdd turns the template's even and odd headers on.
func evenAndOdd(ws *docxedit.Workspace) error {
	const part = "/word/settings.xml"
	h, err := ws.Core().Part(part)
	if err != nil {
		return err
	}
	doc, err := ws.Tree(part)
	if err != nil {
		return err
	}
	_, err = h.EnsureIn(doc.Root(), docxedit.W("evenAndOddHeaders"))
	return err
}
