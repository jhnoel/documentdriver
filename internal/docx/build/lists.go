package build

import (
	"errors"
	"strconv"
	"strings"

	docxedit "github.com/jhnoel/documentdriver/internal/docx/edit"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Lists. An HTML list (<ol>, <ul>) is a numbering definition of its own:
// one w:abstractNum with nine levels and one w:num instance, in the
// numbering part's working tree, placed where CT_Numbering puts them (every
// abstractNum before every num). The levels are docxdriver's create
// template (readonly.rs): start, the list's format, "•" or "%N." and a
// hanging indent of a quarter inch per level. Each paragraph of the list
// gets w:numPr with its level and the instance's id.

// ListLevel is one level of a new numbering definition, as list.set's
// levels field gives it; the zero value of a field means "the template's".
type ListLevel struct {
	Format string
	Label  string
	Start  uint32
	// RestartAfter is the 0-based level after which numbering restarts,
	// -1 for none.
	RestartAfter int
	// Indent: left, right and first-line (negative: hanging) in twips;
	// nil for none.
	IndentLeft, IndentRight, FirstLine *int64
	// Marker formatting: nil for none.
	MarkerBold, MarkerItalic *bool
	MarkerFont, MarkerColor  string
}

// TemplateLevels are the nine levels docxdriver's create writes for a list
// of format starting at start.
func TemplateLevels(format string, start uint32) []ListLevel {
	out := make([]ListLevel, 9)
	for i := range out {
		label := "%" + strconv.Itoa(i+1) + "."
		if format == "bullet" {
			label = "•"
		}
		left, hanging := int64(i+1)*720, int64(-360)
		out[i] = ListLevel{Format: format, Label: label, Start: start, RestartAfter: -1, IndentLeft: &left, FirstLine: &hanging}
	}
	return out
}

// NewList writes a numbering definition with levels into root, the
// numbering part's root (held by h): a w:abstractNum with id abstractID
// and a w:num with id numID naming it.
func NewList(h *core.Handle, root *tree.Node, numID, abstractID string, levels []ListLevel) error {
	abs, num, err := listNodes(h, root, numID, abstractID, levels)
	if err != nil {
		return err
	}
	firstNum, cleanup := numberingSlots(root)
	_, err = placeList(h, root, abs, num, firstNum, cleanup)
	return err
}

// listNodes parses a definition's abstractNum and num, detached, for
// placement in root.
func listNodes(h *core.Handle, root *tree.Node, numID, abstractID string, levels []ListLevel) (abs, num *tree.Node, err error) {
	var b strings.Builder
	b.WriteString(`<w:abstractNum w:abstractNumId="` + abstractID + `"><w:multiLevelType w:val="multilevel"/>`)
	for i, l := range levels {
		b.WriteString(`<w:lvl w:ilvl="` + strconv.Itoa(i) + `">`)
		start := l.Start
		if start == 0 {
			start = 1
		}
		b.WriteString(`<w:start w:val="` + strconv.FormatUint(uint64(start), 10) + `"/>`)
		b.WriteString(`<w:numFmt w:val="` + html.EscapeAttr(l.Format) + `"/>`)
		if l.RestartAfter >= 0 {
			b.WriteString(`<w:lvlRestart w:val="` + strconv.Itoa(l.RestartAfter+1) + `"/>`)
		}
		b.WriteString(`<w:lvlText w:val="` + html.EscapeAttr(l.Label) + `"/>`)
		if l.IndentLeft != nil || l.IndentRight != nil || l.FirstLine != nil {
			b.WriteString(`<w:pPr><w:ind`)
			if l.IndentLeft != nil {
				b.WriteString(` w:left="` + strconv.FormatInt(*l.IndentLeft, 10) + `"`)
			}
			if l.IndentRight != nil {
				b.WriteString(` w:right="` + strconv.FormatInt(*l.IndentRight, 10) + `"`)
			}
			if l.FirstLine != nil {
				if *l.FirstLine < 0 {
					b.WriteString(` w:hanging="` + strconv.FormatInt(-*l.FirstLine, 10) + `"`)
				} else {
					b.WriteString(` w:firstLine="` + strconv.FormatInt(*l.FirstLine, 10) + `"`)
				}
			}
			b.WriteString(`/></w:pPr>`)
		}
		if l.MarkerBold != nil || l.MarkerItalic != nil || l.MarkerFont != "" || l.MarkerColor != "" {
			b.WriteString(`<w:rPr>`)
			if l.MarkerFont != "" {
				f := html.EscapeAttr(l.MarkerFont)
				b.WriteString(`<w:rFonts w:ascii="` + f + `" w:hAnsi="` + f + `"/>`)
			}
			for _, t := range []struct {
				local string
				v     *bool
			}{{"b", l.MarkerBold}, {"i", l.MarkerItalic}} {
				if t.v != nil {
					v := "0"
					if *t.v {
						v = "1"
					}
					b.WriteString(`<w:` + t.local + ` w:val="` + v + `"/>`)
				}
			}
			if l.MarkerColor != "" {
				b.WriteString(`<w:color w:val="` + html.EscapeAttr(l.MarkerColor) + `"/>`)
			}
			b.WriteString(`</w:rPr>`)
		}
		b.WriteString(`</w:lvl>`)
	}
	b.WriteString(`</w:abstractNum>`)
	b.WriteString(`<w:num w:numId="` + numID + `"><w:abstractNumId w:val="` + abstractID + `"/></w:num>`)
	nodes, err := h.Workspace().Parse(root, []byte(b.String()), nsW)
	if err != nil || len(nodes) != 2 {
		return nil, nil, errors.Join(errors.New("build: a numbering definition did not parse"), err)
	}
	return nodes[0], nodes[1], nil
}

// numberingSlots are the places CT_Numbering gives new definitions: every
// abstractNum before every num (before the first num), numIdMacAtCleanup
// last (a new num goes before it); nil for none.
func numberingSlots(root *tree.Node) (firstNum, cleanup *tree.Node) {
	for _, c := range root.Elements() {
		switch {
		case firstNum == nil && model.IsW(c, "num"):
			firstNum = c
		case model.IsW(c, "numIdMacAtCleanup"):
			cleanup = c
		}
	}
	return firstNum, cleanup
}

// placeList places a definition's abstractNum and num in root at the
// slots numberingSlots found, and returns the first num as it then
// stands (num, when there was none).
func placeList(h *core.Handle, root, abs, num, firstNum, cleanup *tree.Node) (*tree.Node, error) {
	absAt := firstNum
	if absAt == nil {
		absAt = cleanup
	}
	if err := h.PlaceAt(root, abs, absAt); err != nil {
		return nil, err
	}
	if err := h.PlaceAt(root, num, cleanup); err != nil {
		return nil, err
	}
	if firstNum == nil {
		firstNum = num
	}
	return firstNum, nil
}

// Membership gives paragraph p (held by h) list membership: w:numPr with
// w:ilvl level and w:numId numID, where the schema puts them.
func Membership(h *core.Handle, p *tree.Node, numID string, level uint32) error {
	ppr, err := h.EnsureIn(p, docxedit.W("pPr"))
	if err != nil {
		return err
	}
	np, err := h.EnsureIn(ppr, docxedit.W("numPr"))
	if err != nil {
		return err
	}
	for _, kv := range [][2]string{{"ilvl", strconv.FormatUint(uint64(level), 10)}, {"numId", numID}} {
		c, err := h.EnsureIn(np, docxedit.W(kv[0]))
		if err != nil {
			return err
		}
		if err := h.SetNodeAttr(c, wq(c, "val"), kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// Lists gives the paragraphs of one lowering their numbering: each HTML
// list (by its id in the parsed items) gets a definition of its own, the
// first time a paragraph of it is built.
type Lists struct {
	// New writes a definition of the template levels for format and
	// start into the numbering part and returns its w:numId.
	New func(format string, start uint32) (string, error)
	ids map[uint32]string
}

// Apply gives paragraph p the membership a's list asks for; nothing for a
// paragraph outside a list.
func (l *Lists) Apply(h *core.Handle, p *tree.Node, a html.ParaAttrs) error {
	if !a.List.OK {
		return nil
	}
	if l == nil || l.New == nil {
		return unsupported("ol")
	}
	list := a.List.V
	if l.ids == nil {
		l.ids = map[uint32]string{}
	}
	id, ok := l.ids[list.ID]
	if !ok {
		var err error
		if id, err = l.New(list.Format, list.Start); err != nil {
			return err
		}
		l.ids[list.ID] = id
	}
	return Membership(h, p, id, list.Level)
}
