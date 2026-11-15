package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/revise"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// setStory is header.set and footer.set (headers.rs set_chrome_typed):
// the section's story of kind gets html's paragraphs as its whole
// content, in an existing story or a new one. The html accounts for the
// story's equations and placeholders (contract section 7, addresses
// counted across the story); kept ones move into the new paragraphs
// (equations are copied in track mode, the old one deleted with its
// paragraph). kind first turns the section's title page on (a tracked
// section property change); kind even turns the document's even and odd
// headers on, direct only (Word records no revision for a setting).
//
// Direct mode replaces the story's content. Track mode lays the change
// out so that accept-all leaves exactly the new content and reject-all
// the old (docxdriver's tracked_story): old paragraphs' content deleted
// and every old mark but the last; new paragraphs inserted before the
// last old one, the last new one's content joined into it with its
// properties recorded in a w:pPrChange. A new story's reference cannot
// be tracked, so rejecting leaves it empty (KeepsAdditions).
func (env *Env) setStory(c chrome, at *SectionAddress, k *ChromeKind, src string, del []string) error {
	kind := kindOf(k)
	sect, n, err := env.storySection(at)
	if err != nil {
		return err
	}
	items, err := html.ParseFragment(src, math.LaTeXToOMML)
	if err != nil {
		return reject("invalid_operation", "html", "%s", err.Error())
	}
	paras, err := build.Paragraphs(items)
	if err != nil {
		return buildRejection(err, "html")
	}
	if len(paras) == 0 {
		paras = []build.Para{{Attrs: html.ParaAttrs{Tag: "p"}}}
	}
	track := env.Mode == ModeTrack
	if track && kind == "even" && !env.settingsOn("evenAndOddHeaders") {
		return reject("refused_in_mode", "kind", "%s kind even cannot be tracked while even and odd headers are off: turning them on changes a document setting (w:evenAndOddHeaders) that Word records no revision for; run document.set_even_odd_headers in a direct plan first", c.word+".set")
	}
	ref := storyRef(sect, c, kind)
	part := ""
	if ref != nil {
		if part, err = env.storyPart(ref); err != nil {
			return err
		}
		if part == "" {
			return reject("unreadable_target", "at", "section %d's %s %s reference does not resolve to a part of the package", n, kind, c.word)
		}
	}

	// What the old story holds, and what the html sends back.
	id := &build.Existing{Equations: build.Equations{}, Placeholders: map[string]model.Placeholder{}}
	var have []string
	if part != "" {
		root, err := env.storyRoot(part)
		if err != nil {
			return reject("unreadable_target", "at", "%s: %s", part, err.Error())
		}
		eqs, phs, order, _ := storyContentRefs(root)
		have = order
		for ref, e := range eqs {
			latex, _, _ := math.OMMLToLaTeX(e)
			id.Equations[ref] = build.Equation{Node: e, LaTeX: latex}
		}
		id.Placeholders = phs
		if track {
			for _, ch := range root.Elements() {
				if !isParagraphOrMarker(ch) {
					return reject("refused_in_mode", "html", "%s cannot be tracked here: the current story holds a %s, and only paragraphs are tracked in a header or footer; use a direct plan", c.word+".set", ch.Local())
				}
			}
		}
	}
	var sent []string
	for _, p := range paras {
		for _, a := range p.Atoms {
			if !isKept(a) {
				continue
			}
			sent = append(sent, a.Ref)
			if track && a.Kind == html.AtomOpaque {
				return reject("refused_in_mode", "html", "%s keeps placeholder %s: moving existing content into a new paragraph cannot be tracked; use a direct plan, or list it in delete", c.word+".set", a.Ref)
			}
		}
	}
	if err := accountFor(have, sent, del, "html"); err != nil {
		return err
	}

	sh, err := env.sectionHandle(sect)
	if err != nil {
		return err
	}
	if kind == "first" {
		if err := env.titlePage(sh, sect); err != nil {
			return err
		}
	}
	if kind == "even" && !track {
		if err := env.evenAndOdd(); err != nil {
			return err
		}
	}
	if part == "" {
		if part, err = env.newStory(sh, sect, c, kind); err != nil {
			return err
		}
	}
	root, err := env.storyRoot(part)
	if err != nil {
		return err
	}
	h := env.Story(part, root)
	eqTemplate := ""
	for _, e := range id.Equations {
		eqTemplate = math.RunProperties(e.Node)
		break
	}
	opt := build.Options{Identity: id, Link: env.storyLink(part), EquationRunProperties: eqTemplate, ComplexFields: track}
	lists := &build.Lists{New: build.NewListIn(env.WS)}
	olds := root.Elements()
	fill := func(p *tree.Node, para build.Para) ([][]*tree.Node, error) {
		if err := build.Properties(h, p, para.Attrs); err != nil {
			return nil, buildRejection(err, "html")
		}
		if err := lists.Apply(h, p, para.Attrs); err != nil {
			return nil, buildRejection(err, "html")
		}
		content, err := build.Inline(h, p, nil, para.Atoms, opt)
		if err != nil {
			return nil, buildRejection(err, "html")
		}
		return content.Groups, nil
	}
	newParagraph := func(before *tree.Node) (*tree.Node, error) {
		nodes, err := h.Workspace().Parse(root, []byte(`<w:p/>`), map[string]string{"w": model.NSW})
		if err != nil || len(nodes) != 1 {
			return nil, errors.Join(errors.New("ops: a new paragraph did not parse"), err)
		}
		return nodes[0], h.PlaceAt(root, nodes[0], before)
	}
	// The tracked layout serves direct mode too (the Reviser accepts what
	// it authored), so others' pending revisions in the story stay as
	// they are; only a story the layout cannot hold (a table, a content
	// control, a kept placeholder) is replaced outright, direct only.
	if !track && !trackable(olds, paras) {
		for _, para := range paras {
			p, err := newParagraph(nil)
			if err != nil {
				return err
			}
			if _, err := fill(p, para); err != nil {
				return err
			}
		}
		for _, o := range olds {
			if err := h.RemoveNode(o); err != nil {
				return err
			}
		}
	} else if err := env.trackStory(h, olds, paras, newParagraph, fill); err != nil {
		return err
	}
	env.Deleted = append(env.Deleted, del...)
	return nil
}

// trackStory lays a tracked story replacement out (see setStory).
func (env *Env) trackStory(h *core.Handle, olds []*tree.Node, paras []build.Para,
	newParagraph func(before *tree.Node) (*tree.Node, error), fill func(*tree.Node, build.Para) ([][]*tree.Node, error)) error {
	var oldPs []*tree.Node
	for _, o := range olds {
		if model.IsW(o, "p") {
			oldPs = append(oldPs, o)
		}
	}
	for i, o := range oldPs {
		if err := expandSimpleFields(h, o); err != nil {
			return err
		}
		if err := env.Rev.DeleteContent(h, o); err != nil {
			if errors.Is(err, revise.ErrStructuredTag) {
				return reject("refused_in_mode", "html", "a paragraph of the current story holds a content control, which a tracked deletion cannot hold; use a direct plan")
			}
			return err
		}
		if i+1 < len(oldPs) && model.ParagraphMarkDel(o) == nil {
			if _, err := env.Rev.MarkParagraph(h, o, "del"); err != nil {
				return err
			}
		}
	}
	var last *tree.Node
	if len(oldPs) > 0 {
		last = oldPs[len(oldPs)-1]
	}
	for i, para := range paras {
		p, err := newParagraph(last)
		if err != nil {
			return err
		}
		groups, err := fill(p, para)
		if err != nil {
			return err
		}
		for _, g := range groups {
			if _, err := env.Rev.Insert(h, g); err != nil {
				return err
			}
		}
		final := i+1 == len(paras)
		switch {
		case !final:
			if _, err := env.Rev.MarkParagraph(h, p, "ins"); err != nil {
				return err
			}
		case last != nil:
			// The last new paragraph's content joins the last old one,
			// which takes its properties (its own in a w:pPrChange).
			for _, ch := range p.Elements() {
				if model.IsW(ch, "pPr") {
					continue
				}
				if err := h.Append(last, ch); err != nil {
					return err
				}
			}
			if _, err := env.Rev.AdoptProperties(h, last, p); err != nil {
				return err
			}
			if err := h.RemoveNode(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// titlePage turns section sect's title page on (w:titlePg), a tracked
// section property change in track mode; nothing when it is on.
func (env *Env) titlePage(h *core.Handle, sect *tree.Node) error {
	if t := model.ChildW(sect, "titlePg"); t != nil {
		if v, _ := model.Attr(t, "val"); v != "0" && v != "false" && v != "off" {
			return nil
		}
	}
	_, err := env.Rev.Property(h, sect, func() error {
		t, err := h.EnsureIn(sect, docxedit.W("titlePg"))
		if err != nil {
			return err
		}
		if _, ok := model.Attr(t, "val"); ok {
			return h.SetNodeAttr(t, qname(t, "val"), "1")
		}
		return nil
	})
	return err
}

// emptySettings is a settings part without settings.
const emptySettings = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + `<w:settings xmlns:w="` + model.NSW + `"/>`

// evenAndOdd turns the document's even and odd headers on, adding the
// settings part when there is none.
func (env *Env) evenAndOdd() error {
	if env.settingsOn("evenAndOddHeaders") {
		return nil
	}
	part, ok := env.settingsPart()
	if !ok {
		part = "/word/settings.xml"
		if _, err := env.WS.AddPart(ctypes.RelSettings, ctypes.CTWordprocessingmlSettings, part, emptySettings); err != nil {
			return err
		}
	}
	h, err := env.WS.Core().Part(part)
	if err != nil {
		return err
	}
	root, err := env.storyRoot(part)
	if err != nil {
		return err
	}
	e, err := h.EnsureIn(root, docxedit.W("evenAndOddHeaders"))
	if err != nil {
		return err
	}
	if _, ok := model.Attr(e, "val"); ok {
		return h.SetNodeAttr(e, qname(e, "val"), "1")
	}
	return nil
}

// storyLink returns the hyperlink allocator of a story part: an existing
// external hyperlink relationship to the target, else a new one, with
// the relationships prefix declared on the part's root.
func (env *Env) storyLink(part string) func(string) (string, error) {
	return func(target string) (string, error) {
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
}

// isParagraphOrMarker reports whether a story child is a paragraph or a
// range marker: the content a tracked story change can hold.
func isParagraphOrMarker(ch *tree.Node) bool {
	return model.IsW(ch, "p") || model.Classify(ch) == model.KindMarker
}

// trackable reports whether a story replacement fits the tracked layout:
// the old story holds paragraphs and markers only, without content
// controls, and the html keeps no placeholder.
func trackable(olds []*tree.Node, paras []build.Para) bool {
	for _, o := range olds {
		if !isParagraphOrMarker(o) || model.IsW(o, "p") && revise.ContentBlocker(o) != nil {
			return false
		}
	}
	for _, p := range paras {
		for _, a := range p.Atoms {
			if a.Kind == html.AtomOpaque {
				return false
			}
		}
	}
	return true
}
