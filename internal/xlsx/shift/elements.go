package shift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Element rewrites one top-level element of the edited sheet other than
// sheetData (local its local name, xml its bytes as Sheet.Pieces gives
// them) for e on that sheet (chunk K8a: the table and the simple
// references; cols is ShiftCols, cols.go, K8b; conditional formatting and
// data validation are ShiftConditionalFormatting and
// ShiftDataValidations, sqref.go, K8c). The classification is a closed
// table in this file (elementRules), one test per entry, and an element
// the table does not list is refused when e could affect it and kept
// otherwise:
//
//   - rewritten: dimension@ref; cols (min/max runs shifted on column
//     edits, split or shortened on deletes, unchanged on row edits);
//     mergeCells (refs shifted; a merge a delete removes or cuts to one
//     cell goes; an emptied mergeCells is removed, R7; count kept
//     right); conditionalFormatting (sqref shifted, cells removed from
//     it; cfRule/formula texts shifted; an emptied one removed);
//     dataValidations (sqref and formula1/formula2 as above; count);
//     hyperlinks (hyperlink@ref; one a delete removes goes; the
//     relationship it names is left, unused: the next save by Excel
//     drops it); autoFilter@ref; sheetViews (selection@activeCell and
//     @sqref, pane@topLeftCell; one a delete removes whole stays at its
//     address, as does a dimension); rowBreaks and colBreaks (brk@id;
//     count, manualBreakCount).
//   - refused when affected (*Refused): extLst children with x14
//     references at or after the point; drawing and legacyDrawing are
//     checked by Guard, not here (their anchors live in other parts).
//   - kept as they are: sheetPr, sheetFormatPr, sheetCalcPr,
//     sheetProtection, protectedRanges (refused when affected),
//     printOptions, pageMargins, pageSetup, headerFooter, tableParts
//     (tables are Tables'), and the rest of the table.
//
// The output is "" when the element is removed.
func Element(local, xml string, sh formula.Sheets, e formula.Edit) (string, Stats, error) {
	if _, err := formula.Shift("", formula.Sheets{Own: e.Sheet}, e); err != nil {
		return "", Stats{}, err
	}
	rule, ok := elementRules[local]
	if !ok {
		return "", Stats{}, &Refused{Construct: "unknown worksheet element " + local}
	}
	switch rule {
	case "keep":
		return xml, Stats{}, nil
	case "cols":
		out, err := ShiftCols(xml, e)
		return out, Stats{}, err
	case "cf":
		return ShiftConditionalFormatting(xml, sh, e)
	case "dv":
		return ShiftDataValidations(xml, sh, e)
	default:
		out, err := elementSimple(local, xml, e, rule)
		return out, Stats{}, err
	}
}

// Each worksheet child is explicitly classified; sheetData has its own kernel.
var elementRules = map[string]string{
	"sheetPr": "keep", "dimension": "refs", "sheetViews": "refs", "sheetFormatPr": "keep", "cols": "cols",
	"sheetCalcPr": "keep", "sheetProtection": "keep", "protectedRanges": "guard", "scenarios": "keep",
	"autoFilter": "refs", "sortState": "keep", "dataConsolidate": "keep", "customSheetViews": "keep",
	"mergeCells": "refs", "phoneticPr": "keep", "conditionalFormatting": "cf", "dataValidations": "dv",
	"hyperlinks": "refs", "printOptions": "keep", "pageMargins": "keep", "pageSetup": "keep", "headerFooter": "keep",
	"rowBreaks": "breaks", "colBreaks": "breaks", "customProperties": "keep", "cellWatches": "keep",
	"ignoredErrors": "keep", "smartTags": "keep", "drawing": "keep", "legacyDrawing": "keep", "legacyDrawingHF": "keep",
	"drawingHF": "keep", "picture": "keep", "oleObjects": "keep", "controls": "keep", "webPublishItems": "keep",
	"tableParts": "keep", "extLst": "guard",
}

// elementFrame is one open element while rewriting a simple element.
type elementFrame struct {
	tag    xmlscan.Token
	remove bool
}

// elementSimple rewrites one simple element of src (a refs, guard or
// breaks element of Element's table) for e.
func elementSimple(local, src string, e formula.Edit, rule string) (string, error) {
	var patches []xmlscan.Edit
	var stack []elementFrame
	var root xmlscan.Token
	seen, kept, manual := false, 0, 0
	rows := e.Kind == formula.InsertRows || e.Kind == formula.DeleteRows
	for pos := 0; pos < len(src); {
		t, err := xmlscan.Next(src, pos)
		if err != nil {
			return "", err
		}
		pos = t.End
		if !t.IsElement() && t.Kind != xmlscan.EndTag {
			continue
		}
		name := t.Local(src)
		if t.Kind == xmlscan.EndTag {
			if len(stack) == 0 || stack[len(stack)-1].tag.Local(src) != name {
				return "", fmt.Errorf("mismatched element tag")
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.remove {
				patches = append(patches, xmlscan.Edit{Start: f.tag.Start, End: t.End, Text: ""})
			}
			if rule == "guard" && name == "sqref" {
				value := xmlscan.DecodeAttr(strings.TrimSpace(src[f.tag.End:t.Start]))
				if err = elementGuard(value, e); err != nil {
					return "", err
				}
			}
			continue
		}
		if len(stack) == 0 {
			if seen || name != local {
				return "", fmt.Errorf("unexpected element root")
			}
			seen = true
			root = t
		}
		remove := false
		if rule == "guard" {
			for _, attrName := range []string{"sqref", "ref"} {
				a, ok := xmlscan.FindAttr(src, t, attrName)
				if !ok {
					if name == "protectedRange" && attrName == "sqref" {
						return "", fmt.Errorf("missing protected range sqref")
					}
					continue
				}
				value := xmlscan.DecodeAttr(a.Raw(src))
				if err = elementGuard(value, e); err != nil {
					return "", err
				}
			}
		}
		if rule == "refs" {
			attrs := []string{}
			switch {
			case (local == "dimension" || local == "autoFilter") && name == local:
				attrs = []string{"ref"}
			case local == "mergeCells" && name == "mergeCell", local == "hyperlinks" && name == "hyperlink":
				attrs = []string{"ref"}
			case local == "sheetViews" && name == "selection":
				attrs = []string{"activeCell", "sqref"}
			case local == "sheetViews" && name == "pane":
				attrs = []string{"topLeftCell"}
			}
			for _, attrName := range attrs {
				a, ok := xmlscan.FindAttr(src, t, attrName)
				if !ok {
					continue
				}
				value := xmlscan.DecodeAttr(a.Raw(src))
				shifted, err := ShiftSqref(value, e)
				if err != nil {
					return "", err
				}
				if shifted == "" {
					if local == "mergeCells" || local == "hyperlinks" {
						remove = true
						break
					}
					if local == "sheetViews" || local == "dimension" {
						// A delete removed every cell the selection, pane
						// or dimension names: it stays at its address, as
						// Excel leaves a selection (the cells there are
						// now the ones after the delete); the dimension is
						// a hint that never shrinks (V: refusing here
						// refused a column delete on 105 corpus files).
						continue
					}
					return "", &Refused{Construct: local + " reference"}
				}
				if local == "mergeCells" {
					r, bad, ok := formula.ParseRefBody(shifted)
					if !ok || bad {
						return "", fmt.Errorf("invalid merge ref")
					}
					if shifted != value && (e.Kind == formula.DeleteRows || e.Kind == formula.DeleteCols) && r.From.Row == r.To.Row && r.From.Col == r.To.Col {
						remove = true
						break
					}
				}
				if shifted != value {
					patches = append(patches, xmlscan.Edit{Start: a.ValueStart, End: a.ValueEnd, Text: xmlscan.EscapeAttr(shifted, a.Quote(src))})
				}
			}
			if (local == "mergeCells" && name == "mergeCell") || (local == "hyperlinks" && name == "hyperlink") {
				if !remove {
					kept++
				}
			}
		}
		if rule == "breaks" && name == "brk" {
			a, ok := xmlscan.FindAttr(src, t, "id")
			if !ok {
				return "", fmt.Errorf("missing break id")
			}
			n, err := strconv.Atoi(a.Raw(src))
			if err != nil || n < 0 {
				return "", fmt.Errorf("invalid break id")
			}
			if (local == "rowBreaks") == rows {
				if e.Kind == formula.DeleteRows || e.Kind == formula.DeleteCols {
					if n >= e.At && n < e.At+e.Count {
						remove = true
					} else if n >= e.At+e.Count {
						n -= e.Count
					}
				} else if n >= e.At {
					n += e.Count
				}
				lim := formula.MaxCols
				if rows {
					lim = formula.MaxRows
				}
				if n > lim {
					return "", &Refused{Construct: "page break outside grid"}
				}
				if !remove && strconv.Itoa(n) != a.Raw(src) {
					patches = append(patches, xmlscan.Edit{Start: a.ValueStart, End: a.ValueEnd, Text: strconv.Itoa(n)})
				}
			}
			if !remove {
				kept++
				if a, ok := xmlscan.FindAttr(src, t, "man"); ok && (a.Raw(src) == "1" || a.Raw(src) == "true") {
					manual++
				}
			}
		}
		if t.Kind == xmlscan.EmptyTag {
			if remove {
				patches = append(patches, xmlscan.Edit{Start: t.Start, End: t.End, Text: ""})
			}
		} else {
			stack = append(stack, elementFrame{tag: t, remove: remove})
		}
	}
	if !seen || len(stack) != 0 {
		return "", fmt.Errorf("incomplete element XML")
	}
	if (local == "mergeCells" || local == "hyperlinks" || rule == "breaks") && kept == 0 {
		return "", nil
	}
	if local == "mergeCells" || rule == "breaks" {
		counts := []struct {
			name string
			n    int
		}{{"count", kept}}
		if rule == "breaks" {
			counts = append(counts, struct {
				name string
				n    int
			}{"manualBreakCount", manual})
		}
		var edits []xmlscan.AttrEdit
		for _, count := range counts {
			if a, ok := xmlscan.FindAttr(src, root, count.name); ok && a.Raw(src) != strconv.Itoa(count.n) {
				edits = append(edits, xmlscan.AttrEdit{Name: count.name, Value: strconv.Itoa(count.n)})
			}
		}
		if len(edits) > 0 {
			tag, err := xmlscan.EditTag(src, root, edits...)
			if err != nil {
				return "", err
			}
			patches = append(patches, xmlscan.Edit{Start: root.Start, End: root.End, Text: tag})
		}
	}
	out, err := xmlscan.Apply(src, patches)
	if err != nil {
		return "", err
	}
	return out, nil
}

// elementGuard reports whether a guarded reference value would be
// affected by e.
func elementGuard(value string, e formula.Edit) error {
	for _, s := range strings.Fields(value) {
		r, bad, ok := formula.ParseRefBody(s)
		if !ok || bad {
			return fmt.Errorf("invalid guarded reference %q", s)
		}
		rows := e.Kind == formula.InsertRows || e.Kind == formula.DeleteRows
		hit := false
		if rows {
			hit = r.Kind == formula.Cols || max(r.From.Row, r.To.Row) >= e.At
		} else {
			hit = r.Kind == formula.Rows || max(r.From.Col, r.To.Col) >= e.At
		}
		if hit {
			return &Refused{Construct: "protected or extension reference"}
		}
	}
	return nil
}
