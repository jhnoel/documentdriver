package sheet

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// The element walkers: pure over one worksheet element's bytes (as
// edit.Sheet.Element or Pieces gives them), on xmlscan; no tree, no
// allocation beyond the slices they return. The kernels that rewrite
// cols, mergeCells, conditional formatting and data validation keep
// their own arithmetic and read the elements only through these.

// ColRun is one <col> of a <cols> element: its whole element span
// cols[Start:End], its start tag, its min and max values and their
// attributes (for spans to rewrite).
type ColRun struct {
	Start, End       int
	Tag              xmlscan.Token
	Min, Max         int
	MinAttr, MaxAttr xmlscan.Attr
}

// ColRuns returns the root start tag of cols (a <cols> element, any
// prefix) and its runs in order. Comments, processing instructions and
// whitespace between runs are skipped. Errors: a child other than col, a
// missing or non-numeric min or max, min > max, runs out of order or
// overlapping, a run past column XFD, and bytes xmlscan cannot lex.
func ColRuns(cols string) (root xmlscan.Token, runs []ColRun, err error) {
	if cols == "" {
		return xmlscan.Token{}, nil, fmt.Errorf("sheet: empty cols element")
	}
	root, err = xmlscan.Next(cols, 0)
	if err != nil {
		return xmlscan.Token{}, nil, err
	}
	if !root.IsElement() || root.Local(cols) != "cols" {
		return xmlscan.Token{}, nil, fmt.Errorf("sheet: invalid cols element")
	}
	if root.Kind == xmlscan.EmptyTag {
		if root.End != len(cols) {
			return xmlscan.Token{}, nil, fmt.Errorf("sheet: trailing content after cols element")
		}
		return root, nil, nil
	}
	prev := 0
	for i := root.End; ; {
		if i >= len(cols) {
			return xmlscan.Token{}, nil, fmt.Errorf("sheet: unclosed cols element")
		}
		t, err := xmlscan.Next(cols, i)
		if err != nil {
			return xmlscan.Token{}, nil, err
		}
		switch t.Kind {
		case xmlscan.Comment, xmlscan.PI:
			i = t.End
		case xmlscan.Text:
			if strings.TrimSpace(cols[t.Start:t.End]) != "" {
				return xmlscan.Token{}, nil, fmt.Errorf("sheet: unexpected cols content")
			}
			i = t.End
		case xmlscan.EndTag:
			if t.Name(cols) != root.Name(cols) {
				return xmlscan.Token{}, nil, fmt.Errorf("sheet: mismatched cols end tag")
			}
			if t.End != len(cols) {
				return xmlscan.Token{}, nil, fmt.Errorf("sheet: trailing content after cols element")
			}
			return root, runs, nil
		case xmlscan.StartTag, xmlscan.EmptyTag:
			if t.Local(cols) != "col" {
				return xmlscan.Token{}, nil, fmt.Errorf("sheet: unexpected cols child %q", t.Name(cols))
			}
			run, end, err := colRun(cols, t)
			if err != nil {
				return xmlscan.Token{}, nil, err
			}
			if run.Min <= prev {
				return xmlscan.Token{}, nil, fmt.Errorf("sheet: column runs out of order or overlapping")
			}
			prev = run.Max
			runs = append(runs, run)
			i = end
		default:
			return xmlscan.Token{}, nil, fmt.Errorf("sheet: unexpected cols child")
		}
	}
}

// colRun reads one <col> tag of cols: its min and max values and their
// attributes, and its whole element end. It is part of ColRuns.
func colRun(cols string, t xmlscan.Token) (ColRun, int, error) {
	minAttr, ok := xmlscan.FindAttr(cols, t, "min")
	if !ok {
		return ColRun{}, 0, fmt.Errorf("sheet: col missing min")
	}
	maxAttr, ok := xmlscan.FindAttr(cols, t, "max")
	if !ok {
		return ColRun{}, 0, fmt.Errorf("sheet: col missing max")
	}
	lo, err := strconv.Atoi(minAttr.Value(cols))
	if err != nil {
		return ColRun{}, 0, fmt.Errorf("sheet: col min is not a number: %w", err)
	}
	hi, err := strconv.Atoi(maxAttr.Value(cols))
	if err != nil {
		return ColRun{}, 0, fmt.Errorf("sheet: col max is not a number: %w", err)
	}
	if lo < 1 || hi > formula.MaxCols || lo > hi {
		return ColRun{}, 0, fmt.Errorf("sheet: invalid col run %d..%d", lo, hi)
	}
	end := t.End
	if t.Kind == xmlscan.StartTag {
		_, end, err = xmlscan.ElementEnd(cols, t)
		if err != nil {
			return ColRun{}, 0, err
		}
	}
	return ColRun{Start: t.Start, End: end, Tag: t, Min: lo, Max: hi, MinAttr: minAttr, MaxAttr: maxAttr}, end, nil
}

// MergeCell is one <mergeCell> of a <mergeCells> element: its whole
// element span, its ref as written and that ref parsed
// (formula.ParseArea).
type MergeCell struct {
	Start, End int
	Ref        string
	Area       formula.Range
}

// MergeList is a <mergeCells> element read: its root start tag, where
// its content ends (the start of </mergeCells>; Root.End for an empty
// tag), its count attribute when it has one, and its cells in order.
type MergeList struct {
	Root       xmlscan.Token
	ContentEnd int
	Count      xmlscan.Attr
	HasCount   bool
	Cells      []MergeCell
}

// Merges reads a <mergeCells> element (any prefix). Errors: a child
// other than mergeCell, a mergeCell without a ref or whose ref is not an
// A1 cell or area, content other than whitespace and comments, and bytes
// xmlscan cannot lex.
func Merges(mergeCells string) (MergeList, error) {
	var list MergeList
	if mergeCells == "" {
		return list, fmt.Errorf("sheet: empty mergeCells element")
	}
	root, err := xmlscan.Next(mergeCells, 0)
	if err != nil {
		return list, err
	}
	if !root.IsElement() || root.Local(mergeCells) != "mergeCells" {
		return list, fmt.Errorf("sheet: invalid mergeCells element")
	}
	list.Root = root
	list.ContentEnd = root.End
	if count, ok := xmlscan.FindAttr(mergeCells, root, "count"); ok {
		list.Count = count
		list.HasCount = true
	}
	if root.Kind == xmlscan.EmptyTag {
		if root.End != len(mergeCells) {
			return MergeList{}, fmt.Errorf("sheet: trailing content after mergeCells element")
		}
		return list, nil
	}
	for i := root.End; ; {
		if i >= len(mergeCells) {
			return MergeList{}, fmt.Errorf("sheet: unclosed mergeCells element")
		}
		t, err := xmlscan.Next(mergeCells, i)
		if err != nil {
			return MergeList{}, err
		}
		switch t.Kind {
		case xmlscan.Comment:
			i = t.End
		case xmlscan.Text:
			if strings.TrimSpace(mergeCells[t.Start:t.End]) != "" {
				return MergeList{}, fmt.Errorf("sheet: unexpected mergeCells content")
			}
			i = t.End
		case xmlscan.EndTag:
			if t.Name(mergeCells) != root.Name(mergeCells) {
				return MergeList{}, fmt.Errorf("sheet: mismatched mergeCells end tag")
			}
			if t.End != len(mergeCells) {
				return MergeList{}, fmt.Errorf("sheet: trailing content after mergeCells element")
			}
			list.ContentEnd = t.Start
			return list, nil
		case xmlscan.StartTag, xmlscan.EmptyTag:
			if t.Local(mergeCells) != "mergeCell" {
				return MergeList{}, fmt.Errorf("sheet: unexpected mergeCells child %q", t.Name(mergeCells))
			}
			refAttr, ok := xmlscan.FindAttr(mergeCells, t, "ref")
			if !ok {
				return MergeList{}, fmt.Errorf("sheet: mergeCell missing ref")
			}
			ref := refAttr.Raw(mergeCells)
			area, ok := formula.ParseArea(ref)
			if !ok {
				return MergeList{}, fmt.Errorf("sheet: invalid merge ref %q", ref)
			}
			end := t.End
			if t.Kind == xmlscan.StartTag {
				_, end, err = xmlscan.ElementEnd(mergeCells, t)
				if err != nil {
					return MergeList{}, err
				}
			}
			list.Cells = append(list.Cells, MergeCell{Start: t.Start, End: end, Ref: ref, Area: area})
			i = end
		default:
			return MergeList{}, fmt.Errorf("sheet: unexpected mergeCells content")
		}
	}
}

// FormulaTexts calls yield for each formula text of a
// conditionalFormatting element (cfRule/formula) or a dataValidations
// element (dataValidation/formula1 and formula2), local naming which:
// rule is the index of the cfRule or dataValidation it belongs to (for a
// caller that drops a whole rule), xml[start:end] the text as written
// (CDATA sections included) and text its decoded value. Another local
// yields nothing. An error from yield stops the walk and is returned.
func FormulaTexts(local, xml string, yield func(rule, start, end int, text string) error) error {
	var ruleLocal string
	switch local {
	case "conditionalFormatting":
		ruleLocal = "cfRule"
	case "dataValidations":
		ruleLocal = "dataValidation"
	default:
		return nil
	}
	if xml == "" {
		return fmt.Errorf("sheet: empty element")
	}
	isFormula := func(name string) bool {
		if local == "conditionalFormatting" {
			return name == "formula"
		}
		return name == "formula1" || name == "formula2"
	}
	type frame struct {
		name, local string
		rule        int
		isFormula   bool
	}
	var stack []frame
	nextRule := 0
	rootSeen := false
	for i := 0; i < len(xml); {
		t, err := xmlscan.Next(xml, i)
		if err != nil {
			return err
		}
		switch t.Kind {
		case xmlscan.Comment, xmlscan.PI:
			i = t.End
		case xmlscan.Text:
			if len(stack) > 0 && stack[len(stack)-1].isFormula {
				top := stack[len(stack)-1]
				if err := yield(top.rule, t.Start, t.End, xmlscan.DecodeText(xml[t.Start:t.End])); err != nil {
					return err
				}
			}
			i = t.End
		case xmlscan.CData:
			if len(stack) > 0 && stack[len(stack)-1].isFormula {
				top := stack[len(stack)-1]
				if err := yield(top.rule, t.Start, t.End, xml[t.Start+9:t.End-3]); err != nil {
					return err
				}
			}
			i = t.End
		case xmlscan.EndTag:
			if len(stack) == 0 || stack[len(stack)-1].name != t.Name(xml) {
				return fmt.Errorf("sheet: mismatched tag")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && t.End != len(xml) {
				return fmt.Errorf("sheet: trailing content after root element")
			}
			i = t.End
		case xmlscan.StartTag, xmlscan.EmptyTag:
			name := t.Name(xml)
			localName := t.Local(xml)
			if !rootSeen {
				if localName != local {
					return fmt.Errorf("sheet: unexpected root %q", name)
				}
				rootSeen = true
				if t.Kind == xmlscan.EmptyTag {
					if t.End != len(xml) {
						return fmt.Errorf("sheet: trailing content after root element")
					}
					return nil
				}
				stack = append(stack, frame{name: name, local: localName, rule: -1})
				i = t.End
				continue
			}
			if len(stack) == 0 {
				return fmt.Errorf("sheet: multiple roots")
			}
			rule := -1
			formula := false
			if len(stack) == 1 && localName == ruleLocal {
				rule = nextRule
				nextRule++
			} else if len(stack) == 2 && stack[1].local == ruleLocal && isFormula(localName) {
				formula = true
				rule = stack[1].rule
			}
			if t.Kind == xmlscan.EmptyTag {
				i = t.End
				continue
			}
			stack = append(stack, frame{name: name, local: localName, rule: rule, isFormula: formula})
			i = t.End
		default:
			return fmt.Errorf("sheet: unexpected markup")
		}
	}
	if !rootSeen || len(stack) != 0 {
		return fmt.Errorf("sheet: incomplete XML")
	}
	return nil
}
