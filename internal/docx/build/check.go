package build

import (
	"github.com/jhnoel/documentdriver/internal/docx/html"
)

// Inline content. An operation that adds one paragraph's content
// (paragraph.insert, and each item of its list form) takes the safe inline
// set only: text with run formatting, links, breaks, tabs and equations.
// Paragraph and table structure, revision tags (the engine authors
// revisions), pending-format tags, fields, notes, images and comment
// milestones are refused, as docxdriver's parse_with refuses them.

// InlineAtoms checks that items, a parsed fragment of exactly one
// paragraph, hold the safe inline set only and returns their atoms.
// Refusals wrap ErrContent.
func InlineAtoms(items []html.Item) ([]html.Atom, error) { return inlineAtoms(items, true) }

// ParagraphAtoms is InlineAtoms without docxdriver's one-link rule: the
// content of a whole paragraph, as read shows it, may mix linked and
// unlinked text and several links (the lowering builds each link's own
// w:hyperlink). Refusals wrap ErrContent.
func ParagraphAtoms(items []html.Item) ([]html.Atom, error) { return inlineAtoms(items, false) }

// inlineAtoms returns the inline atoms of items, requiring exactly one paragraph.
func inlineAtoms(items []html.Item, oneLink bool) ([]html.Atom, error) {
	paras := 0
	for _, it := range items {
		if it.Kind == html.ItemParaOpen {
			paras++
		}
	}
	if paras != 1 {
		return nil, contentErr("inline HTML only: the content of one paragraph, without block elements")
	}
	var atoms []html.Atom
	link := linkState{off: !oneLink}
	for _, it := range items {
		switch it.Kind {
		case html.ItemParaOpen, html.ItemParaClose:
			continue
		case html.ItemStruct:
			return nil, contentErr("table structure <%s> is not inline content", it.Struct)
		}
		a := it.Atom
		if a.Ins.OK || a.Del.OK {
			return nil, contentErr("revision tags (<ins>, <del>) are not content: the engine records revisions")
		}
		if a.Field.OK {
			return nil, contentErr("fields cannot be written through html")
		}
		switch a.Kind {
		case html.AtomChar:
			if a.FmtPending.OK {
				return nil, contentErr("a pending-format tag is not content: format changes are separate operations")
			}
			if err := link.note(a.Link); err != nil {
				return nil, err
			}
		case html.AtomBr, html.AtomTab:
			if err := link.note(a.Link); err != nil {
				return nil, err
			}
		case html.AtomEquation:
			if err := link.note(html.Opt[string]{}); err != nil {
				return nil, err
			}
		case html.AtomOpaque:
			if err := link.note(a.Link); err != nil {
				return nil, err
			}
		case html.AtomNote:
			return nil, contentErr("footnotes and endnotes are not inline content")
		case html.AtomImage:
			return nil, contentErr("images are not inline content")
		case html.AtomMilestone:
			return nil, contentErr("comment markers are not content: comments are separate operations")
		}
		atoms = append(atoms, a)
	}
	return atoms, nil
}

// linkState is docxdriver's note_link: one paragraph's inline content is
// either wholly inside one hyperlink or has none. (The lowering itself
// builds mixed content; the rule is the inline operations'.)
type linkState struct {
	off  bool
	seen bool
	href html.Opt[string]
}

// note records the first link, refusing differing or mixed links.
func (s *linkState) note(link html.Opt[string]) error {
	if s.off {
		return nil
	}
	if !s.seen {
		s.seen, s.href = true, link
		return nil
	}
	switch {
	case s.href.OK && link.OK && s.href.V != link.V:
		return contentErr("one paragraph's content cannot link to different targets: split it into separate operations")
	case s.href.OK != link.OK:
		return contentErr("the content mixes linked and unlinked text: keep a link's content whole, or split it into separate operations")
	}
	return nil
}
