package ops

import (
	"fmt"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
)

// The merge kernel (chunk K12): pure over a <mergeCells> element.

// AddMerge returns mergeCells (Sheet.Element's bytes, "" for none) with
// <mergeCell ref="area"/> appended and count updated (made when ""). An
// area overlapping an existing merge is a Rejection (operation_failed,
// detail the merge it overlaps).
func AddMerge(mergeCells, prefix, area string) (string, error) {
	r, err := mergeArea(area)
	if err != nil {
		return "", err
	}
	if mergeCells == "" {
		return "<" + prefix + "mergeCells count=\"1\">" + "<" + prefix + "mergeCell ref=\"" + area + "\"/>" + "</" + prefix + "mergeCells>", nil
	}
	list, err := sheet.Merges(mergeCells)
	if err != nil {
		return "", err
	}
	a, b, c, d := r.Bounds()
	for _, cell := range list.Cells {
		x, y, z, w := cell.Area.Bounds()
		if a <= z && x <= c && b <= w && y <= d {
			return "", &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "merge overlaps an existing merge", Detail: cell.Ref}
		}
	}
	child := "<" + prefix + "mergeCell ref=\"" + area + "\"/>"
	var s string
	if list.Root.Kind == xmlscan.EmptyTag {
		name := list.Root.Name(mergeCells)
		s = mergeCells[:list.Root.End-2] + ">" + child + "</" + name + ">" + mergeCells[list.Root.End:]
	} else {
		s = mergeCells[:list.ContentEnd] + child + mergeCells[list.ContentEnd:]
	}
	return setMergeCount(s, len(list.Cells)+1)
}

// RemoveMerge returns mergeCells without the mergeCell whose ref is area
// (compared normalised), count updated; "" when it empties (R7). A
// missing merge is a Rejection (not_found).
func RemoveMerge(mergeCells, area string) (string, error) {
	r, err := mergeArea(area)
	if err != nil {
		return "", err
	}
	if mergeCells == "" {
		return "", &Rejection{Code: cli.CodeNotFound, Field: "at", Msg: "merge not found"}
	}
	list, err := sheet.Merges(mergeCells)
	if err != nil {
		return "", err
	}
	a, b, c, d := r.Bounds()
	for _, cell := range list.Cells {
		x, y, z, w := cell.Area.Bounds()
		if a == x && b == y && c == z && d == w {
			if len(list.Cells) == 1 {
				return "", nil
			}
			s := mergeCells[:cell.Start] + mergeCells[cell.End:]
			return setMergeCount(s, len(list.Cells)-1)
		}
	}
	return "", &Rejection{Code: cli.CodeNotFound, Field: "at", Msg: "merge not found"}
}

// mergeArea parses s as an A1 cell or area (formula.ParseArea); s that
// is not one is the merge kernel's "invalid merge range" error.
func mergeArea(s string) (formula.Range, error) {
	r, ok := formula.ParseArea(s)
	if !ok {
		return formula.Range{}, fmt.Errorf("invalid merge range %q", s)
	}
	return r, nil
}

// setMergeCount returns s with its mergeCells count attribute set to n
// (xmlscan.EditTag on the root tag: an existing count keeps its quote
// and spacing, a missing one is inserted right after the last
// attribute, or after the tag name).
func setMergeCount(s string, n int) (string, error) {
	root, err := xmlscan.Next(s, 0)
	if err != nil {
		return "", err
	}
	tag, err := xmlscan.EditTag(s, root, xmlscan.AttrEdit{Name: "count", Value: strconv.Itoa(n)})
	if err != nil {
		return "", err
	}
	return s[:root.Start] + tag + s[root.End:], nil
}
