package shift

import (
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// ShiftCols rewrites a <cols> element for e (chunk K8b): a column insert
// moves runs at or after the point and widens a run that spans it (the
// new columns take the run's attributes, as Excel does); a delete
// shortens or removes the runs it covers; a row edit returns xml as it
// is. Runs stay sorted and disjoint; "" when every run goes.
//
//nolint:revive // exported: the frozen API names this operation ShiftCols.
func ShiftCols(xml string, e formula.Edit) (string, error) {
	if e.Kind == formula.InsertRows || e.Kind == formula.DeleteRows || xml == "" {
		return xml, nil
	}
	if (e.Kind != formula.InsertCols && e.Kind != formula.DeleteCols) || e.At < 1 || e.At > formula.MaxCols || e.Count < 1 || e.Count > formula.MaxCols || (e.Kind == formula.DeleteCols && e.Count > formula.MaxCols-e.At+1) {
		return "", fmt.Errorf("invalid column edit")
	}
	_, runs, err := sheet.ColRuns(xml)
	if err != nil {
		return "", err
	}
	var edits []xmlscan.Edit
	kept := 0
	for _, run := range runs {
		lo, hi := run.Min, run.Max
		if e.Kind == formula.InsertCols {
			if lo >= e.At {
				lo += e.Count
			}
			if hi >= e.At {
				hi += e.Count
			}
			if hi > formula.MaxCols {
				return "", fmt.Errorf("column run exceeds sheet")
			}
		} else {
			endDelete := e.At + e.Count - 1
			if hi >= e.At {
				if lo > endDelete {
					lo -= e.Count
					hi -= e.Count
				} else if lo >= e.At && hi <= endDelete {
					edits = append(edits, xmlscan.Edit{Start: run.Start, End: run.End, Text: ""})
					continue
				} else {
					if lo >= e.At {
						lo = e.At
					}
					if hi > endDelete {
						hi -= e.Count
					} else {
						hi = e.At - 1
					}
				}
			}
		}
		kept++
		if lo == run.Min && hi == run.Max {
			continue
		}
		edits = append(edits,
			xmlscan.Edit{Start: run.MinAttr.ValueStart, End: run.MinAttr.ValueEnd, Text: strconv.Itoa(lo)},
			xmlscan.Edit{Start: run.MaxAttr.ValueStart, End: run.MaxAttr.ValueEnd, Text: strconv.Itoa(hi)},
		)
	}
	if kept == 0 {
		return "", nil
	}
	return xmlscan.Apply(xml, edits)
}
