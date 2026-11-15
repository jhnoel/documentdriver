package ops

import (
	"bytes"
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// InsertParagraph is paragraph.insert (docxdriver's insert_paragraph).
type InsertParagraph struct {
	At       InsertAnchor     `json:"at"`
	Position InsertPosition   `json:"position"`
	HTML     ParagraphContent `json:"html" default:"\"\""`
	Style    *string          `json:"style" default:"null"`
	Alias    *string          `json:"as" default:"null"`
}

// opParagraphInsert is paragraph.insert's entry in the operation table.
var opParagraphInsert = OpDef{
	Name: "paragraph.insert", Oracle: "insert_paragraph", New: func() Spec { return &InsertParagraph{} },
	Moved: "`html` ← `with`",
	Rules: insertParagraphRules,
}

// Run inserts one new paragraph, or one per item of a list html, before
// or after a paragraph or a body-level table of the main document
// (typed.rs insert_paragraph_typed). Each gets a fresh w14:paraId from
// the allocator (slot n for item n), an explicit style alone, or else
// the properties Word's Enter gives a paragraph typed beside a paragraph
// anchor (inherit_paragraph_properties), and its inline content through
// the build lowering. The content goes in as the author's w:ins and the
// new paragraph mark as an inserted mark; direct mode accepts exactly
// those. Items inserted after one anchor, by this operation or earlier
// ones, keep the plan's order.
func (o *InsertParagraph) Run(env *Env) error {
	part, x, err := env.Main()
	if err != nil {
		return err
	}
	anchor, address, isTable, err := env.insertAnchor(x, o.At)
	if err != nil {
		return err
	}
	contents := make([]paragraphHTML, len(o.HTML.Items))
	fields := make([]string, len(o.HTML.Items))
	for i, s := range o.HTML.Items {
		fields[i] = "html"
		if o.HTML.List {
			fields[i] += "[" + strconv.Itoa(i) + "]"
		}
		c, err := parseParagraphHTML(s)
		if err != nil {
			return reject("invalid_operation", fields[i], "%s", err.Error())
		}
		// A new paragraph replaces nothing, so an equation's data-ref (the
		// address read gave the equation it was copied from) names
		// nothing here: it is ignored, and the LaTeX is written.
		for k, a := range c.Atoms {
			if a.Kind == html.AtomEquation && a.AtomObject != nil && a.Ref != "" {
				obj := *a.AtomObject
				obj.Ref = ""
				c.Atoms[k].AtomObject = &obj
			}
		}
		contents[i] = c
	}
	if len(contents) == 0 {
		contents, fields = []paragraphHTML{{}}, []string{"html"}
	}
	var styles *model.XML
	if sx, ok := env.part(env.stylesPart()); ok {
		styles = sx
	}
	if o.Style != nil && !paragraphStyleDeclared(styles, *o.Style) {
		return reject("not_found", "style", "style %q is not a declared paragraph style — paragraph.insert requires an existing paragraph style", *o.Style)
	}
	for i, c := range contents {
		if c.Attrs == nil || o.Style != nil {
			continue
		}
		if id := build.StyleOf(*c.Attrs); id != "" && !paragraphStyleDeclared(styles, id) {
			return reject("not_found", fields[i], "style %q (from <%s>) is not a declared paragraph style of the document", id, c.Attrs.Tag)
		}
	}
	c := anchor.Parent()
	if _, ok := docxedit.Type(c); !ok {
		return reject("operation_failed", "at", "a paragraph cannot be inserted beside %s: its container %s is not a story paragraphs are placed in", address, c.Name())
	}
	before := o.Position == "before"
	reg := env.WS.Registry()

	// The new paragraphs are placed empty, side by side, then filled
	// through one region over them all.
	h, err := env.WS.Core().Range(part, "paragraph.insert at "+address, anchor, anchor)
	if err != nil {
		return err
	}
	ps := make([]*tree.Node, len(contents))
	for i := range ps {
		parsed, err := h.Workspace().Parse(c, []byte(`<w:p/>`), map[string]string{"w": model.NSW})
		if err != nil || len(parsed) != 1 {
			return errors.Join(errors.New("ops: a new paragraph did not parse"), err)
		}
		p := parsed[0]
		ref := anchor
		if !before {
			ref = model.NextElement(reg.InsertAfter(anchor))
		}
		if err := h.PlaceAt(c, p, ref); err != nil {
			return err
		}
		if !before {
			reg.NoteInsertedAfter(anchor, p)
		}
		ps[i] = p
	}
	region, err := env.WS.Core().Range(part, "paragraph.insert at "+address, ps[0], ps[len(ps)-1])
	if err != nil {
		return err
	}
	if err := env.WS.Core().EnsureRootNamespace(part, docxedit.W14.Prefix, docxedit.W14.URIs[0], true); err != nil {
		return err
	}
	opt := build.Options{Link: env.hyperlink}
	var ids []string
	for i, p := range ps {
		got, err := env.WS.ParaIDs(part, env.Index, uint64(i), 1)
		if err != nil {
			return err
		}
		id := got[0]
		if err := region.SetNodeAttr(p, docxedit.W14.Prefix+":paraId", id); err != nil {
			return err
		}
		switch {
		case contents[i].Attrs != nil:
			// The block element's properties, and no others: not the
			// anchor's (an explicit style field still wins).
			if err := build.Properties(region, p, *contents[i].Attrs); err != nil {
				return buildRejection(err, fields[i])
			}
			if o.Style != nil {
				if err := setStyle(region, p, *o.Style); err != nil {
					return err
				}
			}
		case o.Style != nil:
			if err := setStyle(region, p, *o.Style); err != nil {
				return err
			}
		case !isTable:
			if err := inheritProperties(region, styles, anchor, p, before); err != nil {
				return err
			}
		}
		content, err := build.Inline(region, p, nil, contents[i].Atoms, opt)
		if err != nil {
			return buildRejection(err, fields[i])
		}
		for _, g := range content.Groups {
			if _, err := env.Rev.Insert(region, g); err != nil {
				return err
			}
		}
		if _, err := env.Rev.MarkParagraph(region, p, "ins"); err != nil {
			return err
		}
		reg.BindParagraph(id, p)
		ids = append(ids, id)
	}
	env.Created = append(env.Created, ids...)
	return nil
}

// insertAnchor resolves paragraph.insert's at: a main-document paragraph
// (by id or alias), or "table:N", the Nth table among the body's
// effective blocks as they stand (render.rs body_table_insert_targets).
func (env *Env) insertAnchor(x *model.XML, at InsertAnchor) (anchor *tree.Node, address string, isTable bool, err error) {
	rest, ok := strings.CutPrefix(string(at), "table:")
	if !ok {
		p, id, err := env.paragraph(ParagraphAddress(at))
		return p, id, false, err
	}
	n, _ := strconv.Atoi(rest) //nolint:errcheck // decoding wrote N >= 1
	var tables []*tree.Node
	if body := x.Body(); body != nil {
		for _, b := range effectiveBlocks(body) {
			if model.IsW(b, "tbl") {
				tables = append(tables, b)
			}
		}
	}
	if n < 1 || n > len(tables) {
		return nil, "", true, reject("not_found", "at", "no body-level table %d (document has %d)", n, len(tables))
	}
	return tables[n-1], string(at), true, nil
}

// effectiveBlocks are a story container's paragraphs and tables,
// through content controls, custom XML and the effective branch of
// alternate content (render.rs effective_story_children).
func effectiveBlocks(container *tree.Node) []*tree.Node {
	var out []*tree.Node
	var walk func(c *tree.Node, depth int)
	walk = func(c *tree.Node, depth int) {
		if depth > tree.MaxDepth {
			return
		}
		for _, n := range c.Elements() {
			switch {
			case model.IsW(n, "p") || model.IsW(n, "tbl"):
				out = append(out, n)
			case n.Local() == "AlternateContent":
				if b := model.AlternateContentBranch(n); b != nil {
					walk(b, depth+1)
				}
			case model.IsW(n, "sdt"):
				if sc := model.ChildW(n, "sdtContent"); sc != nil {
					walk(sc, depth+1)
				}
			case model.IsW(n, "sdtContent") || model.IsW(n, "customXml"):
				walk(n, depth+1)
			}
		}
	}
	walk(container, 0)
	return out
}

// buildRejection maps a refusal of the build lowering to the operation's
// refusal at field; other errors are the engine's.
func buildRejection(err error, field string) error {
	var ref *build.RefError
	switch {
	case errors.As(err, &ref):
		return reject("not_found", field, "%s", err.Error())
	case errors.Is(err, build.ErrContent):
		return reject("invalid_operation", field, "%s", err.Error())
	case errors.Is(err, build.ErrStrict):
		return reject("operation_failed", field, "%s", err.Error())
	}
	return err
}

// stylesPart is the styles part beside the main document part.
func (env *Env) stylesPart() string {
	return path.Join(path.Dir(strings.TrimPrefix(env.WS.DocumentPart(), "/")), "styles.xml")
}

// paragraphStyles are the styles part's paragraph styles (a w:style
// without w:type is one), in document order.
func paragraphStyles(styles *model.XML) []*tree.Node {
	if styles == nil || styles.Root() == nil {
		return nil
	}
	var out []*tree.Node
	styles.Root().Walk(func(n *tree.Node) bool {
		if model.IsW(n, "style") {
			if t, ok := model.Attr(n, "type"); !ok || t == "paragraph" {
				out = append(out, n)
			}
			return false
		}
		return true
	})
	return out
}

// paragraphStyleDeclared reports whether the styles part declares a
// paragraph style with id (layout.rs paragraph_style_exists).
func paragraphStyleDeclared(styles *model.XML, id string) bool {
	for _, s := range paragraphStyles(styles) {
		if model.AttrOr(s, "styleId") == id {
			return true
		}
	}
	return false
}

// nextParagraphStyle is the style Word gives a paragraph typed after one
// in style (its w:next; style "" is the default paragraph style), and the
// default paragraph style (layout.rs next_paragraph_style).
func nextParagraphStyle(styles *model.XML, style string) (next, def string) {
	all := paragraphStyles(styles)
	for _, s := range all {
		if v := model.AttrOr(s, "default"); v == "1" || v == "true" || v == "on" {
			def = model.AttrOr(s, "styleId")
			break
		}
	}
	id := style
	if id == "" {
		id = def
	}
	if id == "" {
		return "", def
	}
	for _, s := range all {
		if model.AttrOr(s, "styleId") == id {
			if n := model.ChildW(s, "next"); n != nil {
				next = model.AttrOr(n, "val")
			}
			break
		}
	}
	return next, def
}

// setStyle gives paragraph p the paragraph style id.
func setStyle(h *core.Handle, p *tree.Node, id string) error {
	ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	ps, err := h.EnsureIn(ppr, docxedit.W("pStyle"))
	if err != nil {
		return err
	}
	return h.SetNodeAttr(ps, qname(ps, "val"), id)
}

// inheritProperties gives the new paragraph p the properties Word's
// Enter gives a paragraph typed beside anchor (layout.rs
// inherit_paragraph_properties): after a paragraph whose style names a
// different next style (a heading), that style alone (nothing when it is
// the default style); otherwise a copy of the anchor's base properties
// (w:pPr without its mark's run properties, section properties or a
// pending change).
func inheritProperties(h *core.Handle, styles *model.XML, anchor, p *tree.Node, before bool) error {
	appr := model.ChildW(anchor, "pPr")
	style := ""
	if appr != nil {
		if ps := model.ChildW(appr, "pStyle"); ps != nil {
			style = model.AttrOr(ps, "val")
		}
	}
	if !before {
		next, def := nextParagraphStyle(styles, style)
		current := style
		if current == "" {
			current = def
		}
		if next != "" && next != current {
			if next != def {
				return setStyle(h, p, next)
			}
			return nil
		}
	}
	if appr == nil {
		return nil
	}
	var base bytes.Buffer
	var kept []*tree.Node
	for _, c := range appr.Elements() {
		if model.IsW(c, "rPr") || model.IsW(c, "sectPr") || model.IsW(c, "pPrChange") {
			continue
		}
		base.Write(c.Bytes())
		kept = append(kept, c)
	}
	if base.Len() == 0 {
		return nil
	}
	frag := "<" + appr.Name() + ">" + base.String() + "</" + appr.Name() + ">"
	nodes, err := h.Workspace().Parse(appr, []byte(frag), nil)
	if err != nil || len(nodes) != 1 {
		return errors.Join(errors.New("ops: copying the anchor's paragraph properties"), err)
	}
	// A copy, for the integrity gate: it carries the anchor's faults.
	h.Workspace().RecordCopy(nodes[0], appr)
	h.Workspace().RecordCopies(nodes[0], kept)
	return h.PlaceNew(p, nodes[0])
}

// hyperlink returns the id of an external hyperlink relationship of the
// main document part to target: an existing one to the same target, else
// a new one (chrome.rs ensure_external_hyperlink), with the
// relationships prefix declared on the part's root.
func (env *Env) hyperlink(target string) (string, error) {
	part := env.WS.DocumentPart()
	rels, err := env.WS.Package().Rels(part)
	if err != nil {
		return "", err
	}
	for _, rel := range rels.All() {
		if ctypes.Is(rel.Type, ctypes.RelHyperlink) && rel.External() && rel.Target == target {
			return rel.ID, env.declareRelPrefix(part)
		}
	}
	env.WS.Core().DeclarePart(rels.Name())
	id, err := rels.Add(ctypes.RelHyperlink, target, true)
	if err != nil {
		return "", err
	}
	return id, env.declareRelPrefix(part)
}

// declareRelPrefix makes sure the part's root binds a prefix to the
// relationships namespace (r, transitional, unless one is bound).
func (env *Env) declareRelPrefix(part string) error {
	doc, err := env.WS.Tree(part)
	if err != nil {
		return err
	}
	if root := doc.Root(); root != nil {
		for _, uri := range core.REL.URIs {
			if p, ok := root.LookupNamespace(uri); ok && p != "" {
				return nil
			}
		}
	}
	return env.WS.Core().EnsureRootNamespace(part, core.REL.Prefix, core.REL.URIs[0], false)
}
