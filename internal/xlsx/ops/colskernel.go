package ops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The cols kernel (chunk K11): pure over a <cols> element's bytes.

// SetColumns returns cols (the sheet's <cols> element as Sheet.Element
// gives it, "" for none, prefix the sheet's) with columns from..to
// given width (customWidth="1"), hidden (hidden="1"; false removes it)
// and/or style (col@style, a cellXfs index: range.format on whole
// columns; 0 removes it), each nil to leave alone: col runs covering the range are split at its ends so the
// range has runs of its own; attributes of the parts outside are kept;
// a column with no run gets one (min, max, and width when given, else
// the sheet default width 8.43 is not invented: a hidden-only run
// without width carries no width). Runs stay sorted and disjoint and
// adjacent identical runs are not merged (bytes elsewhere stay).
// Errors: from > to, a run that does not parse.
func SetColumns(cols, prefix string, from, to int, width *float64, hidden *bool, style *int) (string, error) {
	if from > to {
		return "", fmt.Errorf("invalid column range")
	}
	if cols == "" {
		cols = "<" + prefix + "cols></" + prefix + "cols>"
	}
	root, runs, err := sheet.ColRuns(cols)
	if err != nil {
		return "", err
	}
	if root.Kind == xmlscan.EmptyTag {
		var out strings.Builder
		out.WriteString(cols[:root.End-2])
		out.WriteString(">")
		if from <= to {
			tag, e := newCol(prefix, from, to, width, hidden, style)
			if e != nil {
				return "", e
			}
			out.WriteString(tag)
		}
		out.WriteString("</" + prefix + "cols>")
		return out.String(), nil
	}
	contentEnd, _, err := xmlscan.ElementEnd(cols, root)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString(cols[:root.End])
	next := from
	prevEnd := root.End
	for _, run := range runs {
		out.WriteString(cols[prevEnd:run.Start])
		if next <= to && next < run.Min {
			stop := run.Min - 1
			if stop > to {
				stop = to
			}
			tag, e := newCol(prefix, next, stop, width, hidden, style)
			if e != nil {
				return "", e
			}
			out.WriteString(tag)
			next = stop + 1
		}
		if run.Max < from || run.Min > to {
			out.WriteString(cols[run.Start:run.End])
		} else {
			if run.Min < from {
				tag, e := xmlscan.EditTag(cols, run.Tag, xmlscan.AttrEdit{Name: "max", Value: strconv.Itoa(from - 1)})
				if e != nil {
					return "", e
				}
				out.WriteString(tag)
				out.WriteString(cols[run.Tag.End:run.End])
			}
			lo := run.Min
			if lo < from {
				lo = from
			}
			hi := run.Max
			if hi > to {
				hi = to
			}
			var edits []xmlscan.AttrEdit
			if lo != run.Min {
				edits = append(edits, xmlscan.AttrEdit{Name: "min", Value: strconv.Itoa(lo)})
			}
			if hi != run.Max {
				edits = append(edits, xmlscan.AttrEdit{Name: "max", Value: strconv.Itoa(hi)})
			}
			edits = append(edits, colAttrEdits(width, hidden, style)...)
			tag, e := xmlscan.EditTag(cols, run.Tag, edits...)
			if e != nil {
				return "", e
			}
			out.WriteString(tag)
			out.WriteString(cols[run.Tag.End:run.End])
			if hi >= next {
				next = hi + 1
			}
			if run.Max > to {
				tag, e := xmlscan.EditTag(cols, run.Tag, xmlscan.AttrEdit{Name: "min", Value: strconv.Itoa(to + 1)})
				if e != nil {
					return "", e
				}
				out.WriteString(tag)
				out.WriteString(cols[run.Tag.End:run.End])
			}
		}
		prevEnd = run.End
	}
	out.WriteString(cols[prevEnd:contentEnd])
	if next <= to {
		tag, e := newCol(prefix, next, to, width, hidden, style)
		if e != nil {
			return "", e
		}
		out.WriteString(tag)
	}
	out.WriteString(cols[contentEnd:])
	return out.String(), nil
}

// colAttrEdits returns the edits for width (and customWidth), hidden
// and style in the fixed order min, max, width, customWidth, hidden,
// style, removing a nil-guarded hidden and style when their value
// removes them. It is the attribute half of SetColumns.
func colAttrEdits(w *float64, h *bool, s *int) []xmlscan.AttrEdit {
	var edits []xmlscan.AttrEdit
	if w != nil {
		edits = append(edits, xmlscan.AttrEdit{Name: "width", Value: strconv.FormatFloat(*w, 'f', -1, 64)})
		edits = append(edits, xmlscan.AttrEdit{Name: "customWidth", Value: "1"})
	}
	if h != nil {
		if *h {
			edits = append(edits, xmlscan.AttrEdit{Name: "hidden", Value: "1"})
		} else {
			edits = append(edits, xmlscan.AttrEdit{Name: "hidden", Remove: true})
		}
	}
	if s != nil {
		if *s != 0 {
			edits = append(edits, xmlscan.AttrEdit{Name: "style", Value: strconv.Itoa(*s)})
		} else {
			edits = append(edits, xmlscan.AttrEdit{Name: "style", Remove: true})
		}
	}
	return edits
}

// newCol returns a <col/> tag for columns a..b with the given format,
// as xmlscan.EditTag on a bare <col/> of the sheet's prefix; new
// attributes appear in the fixed order min, max, width, customWidth,
// hidden, style.
func newCol(prefix string, a, b int, w *float64, h *bool, s *int) (string, error) {
	src := "<" + prefix + "col/>"
	t, err := xmlscan.Next(src, 0)
	if err != nil {
		return "", err
	}
	edits := []xmlscan.AttrEdit{
		{Name: "min", Value: strconv.Itoa(a)},
		{Name: "max", Value: strconv.Itoa(b)},
	}
	edits = append(edits, colAttrEdits(w, h, s)...)
	return xmlscan.EditTag(src, t, edits...)
}
