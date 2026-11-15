package shift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// ShiftSqref shifts an ST_Sqref value (space-separated cells and areas)
// for e on its own sheet (chunk K8c): each area moves or grows as Shift
// moves references; cells a delete removes leave the list; "" when none
// remains.
//
//nolint:revive // exported: the frozen API specifies this name.
func ShiftSqref(sqref string, e formula.Edit) (string, error) {
	var out []string
	for _, ref := range strings.Fields(sqref) {
		_, bad, ok := formula.ParseRefBody(ref)
		if !ok {
			return "", fmt.Errorf("invalid sqref %q", ref)
		}
		if bad {
			// #REF!, a defect the input already has: it names nothing
			// and is left as it is.
			out = append(out, ref)
			continue
		}
		// Whole rows and columns ("27:27", "C:E") shift as Shift moves
		// them.
		shifted, err := formula.Shift(ref, formula.Sheets{Own: e.Sheet}, e)
		if err != nil {
			return "", err
		}
		if shifted != "#REF!" {
			out = append(out, shifted)
		}
	}
	return strings.Join(out, " "), nil
}

// ShiftConditionalFormatting rewrites one <conditionalFormatting>
// element: its sqref (ShiftSqref) and every <formula> text of its rules
// (formula.Shift with sh); "" when its sqref empties.
//
//nolint:revive // exported: the frozen API specifies this name.
func ShiftConditionalFormatting(xml string, sh formula.Sheets, e formula.Edit) (string, Stats, error) {
	return rewriteSqXML(xml, sh, e, false)
}

// ShiftDataValidations rewrites a <dataValidations> element: each
// dataValidation's sqref and formula1/formula2 texts; one whose sqref
// empties goes; count kept right; "" when none remains.
//
//nolint:revive // exported: the frozen API specifies this name.
func ShiftDataValidations(xml string, sh formula.Sheets, e formula.Edit) (string, Stats, error) {
	return rewriteSqXML(xml, sh, e, true)
}

// frame is one open element while rewriting an sqref element.
type frame struct {
	tag    xmlscan.Token
	remove bool
}

// rewriteSqXML rewrites one <conditionalFormatting> element (dv false)
// or one <dataValidations> element (dv true) of src for e: the root
// sqref (conditional formatting) or each dataValidation's sqref, and
// every formula text of a rule that stays, with the count kept right.
func rewriteSqXML(src string, sh formula.Sheets, e formula.Edit, dv bool) (string, Stats, error) {
	local := "conditionalFormatting"
	if dv {
		local = "dataValidations"
	}
	var patches []xmlscan.Edit
	var stack []frame
	var stats Stats
	var root xmlscan.Token
	var removed []bool
	kept, rules := 0, 0
	rootSeen := false
	for pos := 0; pos < len(src); {
		t, err := xmlscan.Next(src, pos)
		if err != nil {
			return "", Stats{}, err
		}
		pos = t.End
		if !t.IsElement() && t.Kind != xmlscan.EndTag {
			continue
		}
		name := t.Local(src)
		if t.Kind == xmlscan.EndTag {
			if len(stack) == 0 || stack[len(stack)-1].tag.Local(src) != name {
				return "", Stats{}, fmt.Errorf("mismatched XML tag")
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.remove && (len(stack) == 0 || !stack[len(stack)-1].remove) {
				patches = append(patches, xmlscan.Edit{Start: f.tag.Start, End: t.End, Text: ""})
			}
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1].remove {
			if t.Kind != xmlscan.EmptyTag {
				stack = append(stack, frame{tag: t, remove: true})
			}
			continue
		}
		isRoot := len(stack) == 0
		if isRoot {
			if rootSeen {
				return "", Stats{}, fmt.Errorf("multiple XML roots")
			}
			rootSeen = true
			root = t
			if name != local {
				return "", Stats{}, fmt.Errorf("unexpected XML root")
			}
		}
		target := (!dv && isRoot) || (dv && len(stack) == 1 && name == "dataValidation")
		rule := -1
		if dv && len(stack) == 1 && name == "dataValidation" {
			rule = rules
			rules++
			removed = append(removed, false)
		}
		remove := false
		if target {
			a, ok := xmlscan.FindAttr(src, t, "sqref")
			if !ok {
				return "", Stats{}, fmt.Errorf("missing sqref")
			}
			val := xmlscan.DecodeAttr(a.Raw(src))
			shifted, err := ShiftSqref(val, e)
			if err != nil {
				return "", Stats{}, err
			}
			if shifted == "" {
				remove = true
				if rule >= 0 {
					removed[rule] = true
				}
			} else {
				kept++
				if shifted != val {
					patches = append(patches, xmlscan.Edit{Start: a.ValueStart, End: a.ValueEnd, Text: xmlscan.EscapeAttr(shifted, a.Quote(src))})
				}
			}
		}
		if t.Kind == xmlscan.EmptyTag {
			if remove {
				patches = append(patches, xmlscan.Edit{Start: t.Start, End: t.End, Text: ""})
			}
			continue
		}
		stack = append(stack, frame{tag: t, remove: remove})
	}
	if len(stack) != 0 || !rootSeen {
		return "", Stats{}, fmt.Errorf("incomplete XML")
	}
	if kept == 0 {
		return "", Stats{}, nil
	}
	err := sheet.FormulaTexts(local, src, func(rule, start, end int, text string) error {
		if rule >= 0 && rule < len(removed) && removed[rule] {
			return nil
		}
		shifted, err := formula.Shift(text, sh, e)
		if err != nil {
			return err
		}
		if shifted != text {
			patches = append(patches, xmlscan.Edit{Start: start, End: end, Text: xmlscan.EscapeText(shifted)})
			stats.Adjusted++
		}
		return nil
	})
	if err != nil {
		return "", Stats{}, err
	}
	if dv {
		if a, ok := xmlscan.FindAttr(src, root, "count"); ok && a.Raw(src) != strconv.Itoa(kept) {
			tag, err := xmlscan.EditTag(src, root, xmlscan.AttrEdit{Name: "count", Value: strconv.Itoa(kept)})
			if err != nil {
				return "", Stats{}, err
			}
			patches = append(patches, xmlscan.Edit{Start: root.Start, End: root.End, Text: tag})
		}
	}
	out, err := xmlscan.Apply(src, patches)
	if err != nil {
		return "", Stats{}, err
	}
	return out, stats, nil
}
