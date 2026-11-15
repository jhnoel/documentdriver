package read

import (
	"fmt"
	"math"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Styles is what read needs of styles.xml: the number format of each
// cellXfs entry, classified. Its memory is O(cellXfs).
type Styles struct {
	kinds []numfmt.Kind
	codes []string
}

// parseStyles reads a parsed styles part: numFmts/numFmt (numFmtId,
// formatCode; a custom id overrides a built-in one, as Excel does) and
// cellXfs/xf@numFmtId in order (absent is 0). Each xf's code is the
// custom format of its id, else numfmt.BuiltIn(id), else "" (an id with
// no format is General); its kind is numfmt.Classify(code), General for
// "". Elements in other namespaces are ignored; both main namespaces
// are read. It never fails on content: a numFmtId that does not parse is
// treated as 0. It fails only when root is not a <styleSheet>.
func parseStyles(doc *tree.Document) (*Styles, error) {
	root := doc.Root()
	if root == nil || !sml.IsMain(root.Space()) || root.Local() != "styleSheet" {
		return nil, fmt.Errorf("styles: the part has no styleSheet root")
	}
	custom := map[uint64]string{}
	if fmts := sml.MainChild(root, "numFmts"); fmts != nil {
		for n := range fmts.ElementsSeq() {
			if !sml.IsMain(n.Space()) || n.Local() != "numFmt" {
				continue
			}
			text, _ := n.Attr("numFmtId")
			id := formatID(text)
			custom[id], _ = n.Attr("formatCode")
		}
	}
	s := &Styles{}
	if xfs := sml.MainChild(root, "cellXfs"); xfs != nil {
		for n := range xfs.ElementsSeq() {
			if !sml.IsMain(n.Space()) || n.Local() != "xf" {
				continue
			}
			text, _ := n.Attr("numFmtId")
			id := formatID(text)
			code, ok := custom[id]
			if !ok && id <= math.MaxInt32 {
				code, _ = numfmt.BuiltIn(int(id))
			}
			kind := numfmt.General
			if code != "" {
				kind = numfmt.Classify(code)
			}
			s.kinds = append(s.kinds, kind)
			s.codes = append(s.codes, code)
		}
	}
	return s, nil
}

// Kind returns the classified number format of cellXfs entry xf, and its
// code (for a note's reason, cut to 64 bytes by the caller). ok is false
// when s is nil (no styles part, or unreadable) or xf is out of range;
// the caller then treats the cell as General (shows the number) and
// adds a note only when s is non-nil and xf is out of range ("style
// index N is not in cellXfs").
func (s *Styles) Kind(xf int) (k numfmt.Kind, code string, ok bool) {
	if s == nil || xf < 0 || xf >= len(s.kinds) {
		return numfmt.General, "", false
	}
	return s.kinds[xf], s.codes[xf], true
}

// Styles returns the workbook's styles, parsed on first use (the
// workbook's RelStyles target, Part.Tree, parseStyles). It is nil when
// the workbook has no styles part, or when it is unreadable (a NotePart
// note: "dates are shown as numbers").
func (b *Book) Styles() *Styles {
	if b.stylesDone {
		return b.styles
	}
	b.stylesDone = true
	part := b.target(ctypes.RelStyles)
	if part == "" {
		return nil
	}
	pt, err := b.pkg.Part(part)
	if err == nil {
		var doc *tree.Document
		doc, err = pt.Tree()
		if err == nil {
			b.styles, err = parseStyles(doc)
		}
	}
	if err != nil {
		b.recordLimit(err)
		b.Notes.Add(model.NotePart, part, "the styles part cannot be read; dates are shown as numbers")
	}
	return b.styles
}

// formatID parses an unsigned numFmtId, treating an unreadable id as 0.
func formatID(text string) uint64 {
	id, err := strconv.ParseUint(text, 10, 32)
	if err != nil {
		return 0
	}
	return id
}
