package ops

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The rename element kernel (chunk K16): pure.

// RenameElement rewrites one top-level worksheet element for sheet old
// becoming name (local its local name, xml its bytes): in
// conditionalFormatting the <formula> texts, in dataValidations the
// formula1/formula2 texts (formula.Rename), in hyperlinks each
// hyperlink@location (RenameText). Any other element is returned as it
// is. It returns the number of texts changed.
func RenameElement(local, xml, old, name string) (string, int, error) {
	if local != "conditionalFormatting" && local != "dataValidations" && local != "hyperlinks" {
		return xml, 0, nil
	}
	var edits []xmlscan.Edit
	if local == "hyperlinks" {
		v := &renameElementVisitor{src: xml, old: old, name: name}
		if err := tree.ScanString(xml, v); err != nil {
			return "", 0, err
		}
		edits = v.edits
	} else {
		var err error
		edits, err = renameFormulaEdits(local, xml, old, name)
		if err != nil {
			return "", 0, err
		}
	}
	if len(edits) == 0 {
		return xml, 0, nil
	}
	out, err := xmlscan.Apply(xml, edits)
	if err != nil {
		return "", 0, err
	}
	return out, len(edits), nil
}

// renameFormulaEdits returns the byte edits renaming old to name in the
// formula texts of xml's conditionalFormatting or dataValidations
// elements. xml is either that element itself or a wrapper (the
// caller's renameRoot) whose top-level children are such elements.
// FormulaTexts matches by local name, so the wrapper's namespace
// declarations do not matter; each top-level child is walked with
// xmlscan and passed to it as its own substring, with the child's
// start offset added to the spans it yields. The rule index restarts
// per child and rename does not use it.
func renameFormulaEdits(local, xml, old, name string) ([]xmlscan.Edit, error) {
	root, err := xmlscan.Next(xml, 0)
	if err != nil {
		return nil, err
	}
	if !root.IsElement() {
		return nil, fmt.Errorf("rename element: not an element")
	}
	if root.Local(xml) == local {
		_, end, err := xmlscan.ElementEnd(xml, root)
		if err != nil {
			return nil, err
		}
		if end != len(xml) {
			return nil, fmt.Errorf("rename element: trailing content after root element")
		}
		return formulaEdits(local, xml, 0, old, name)
	}
	if root.Kind == xmlscan.EmptyTag {
		if root.End != len(xml) {
			return nil, fmt.Errorf("rename element: trailing content after root element")
		}
		return nil, nil
	}
	var edits []xmlscan.Edit
	for i := root.End; ; {
		if i >= len(xml) {
			return nil, fmt.Errorf("rename element: unclosed wrapper element")
		}
		t, err := xmlscan.Next(xml, i)
		if err != nil {
			return nil, err
		}
		switch t.Kind {
		case xmlscan.Comment, xmlscan.PI, xmlscan.CData, xmlscan.Text:
			i = t.End
		case xmlscan.EndTag:
			if t.Name(xml) != root.Name(xml) {
				return nil, fmt.Errorf("rename element: mismatched wrapper end tag")
			}
			if t.End != len(xml) {
				return nil, fmt.Errorf("rename element: trailing content after root element")
			}
			return edits, nil
		case xmlscan.StartTag, xmlscan.EmptyTag:
			end := t.End
			if t.Kind == xmlscan.StartTag {
				_, end, err = xmlscan.ElementEnd(xml, t)
				if err != nil {
					return nil, err
				}
			}
			if t.Local(xml) == local {
				child, err := formulaEdits(local, xml[t.Start:end], t.Start, old, name)
				if err != nil {
					return nil, err
				}
				edits = append(edits, child...)
			}
			i = end
		default:
			return nil, fmt.Errorf("rename element: unexpected markup")
		}
	}
}

// formulaEdits returns the byte edits renaming old to name in the
// formula texts sheet.FormulaTexts yields for one conditionalFormatting
// or dataValidations element child's bytes, offset by base.
func formulaEdits(local, child string, base int, old, name string) ([]xmlscan.Edit, error) {
	var edits []xmlscan.Edit
	err := sheet.FormulaTexts(local, child, func(_, start, end int, text string) error {
		changed := formula.Rename(text, old, name)
		if changed == text {
			return nil
		}
		edits = append(edits, xmlscan.Edit{Start: base + start, End: base + end, Text: xmlscan.EscapeText(changed)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return edits, nil
}

// renameElementVisitor collects the renames of a hyperlinks element
// scan: the changed hyperlink locations, as byte edits for
// xmlscan.Apply.
type renameElementVisitor struct {
	src   string
	old   string
	name  string
	edits []xmlscan.Edit
}

// Start records the edit for a hyperlink node's location attribute
// whose decoded value renames the sheet, keeping the attribute's
// quote: the replacement is xmlscan.EscapeAttr of the renamed decoded
// value, at the raw span tree.Node.AttrSpan returns.
func (v *renameElementVisitor) Start(n *tree.Node) error {
	if n.Local() != "hyperlink" {
		return nil
	}
	value, ok := n.Attr("location")
	if !ok {
		return nil
	}
	changed := formula.Rename(value, v.old, v.name)
	if changed == value {
		return nil
	}
	start, end, ok := n.AttrSpan("location")
	if !ok || start < 1 || start > len(v.src) {
		return fmt.Errorf("rename element: missing location attribute")
	}
	v.edits = append(v.edits, xmlscan.Edit{Start: start, End: end, Text: xmlscan.EscapeAttr(changed, v.src[start-1])})
	return nil
}

// End does nothing: the hyperlink visitor keeps no element stack.
func (v *renameElementVisitor) End(_ *tree.Node) error {
	return nil
}

// Leaf does nothing: hyperlink locations are read in Start.
func (v *renameElementVisitor) Leaf(_ *tree.Node) error {
	return nil
}
