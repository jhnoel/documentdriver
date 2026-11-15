package shift

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Parts are the texts Guard checks, read by Apply: the edited sheet's
// relationship types, its drawing part, its legacy VML drawing and
// comments parts, its threaded comments part, every pivot cache
// definition and every external link part of the workbook ("" or nil
// for none).
type Parts struct {
	SheetRelTypes          []string
	Drawing, VML, Comments string
	ThreadedComments       string
	PivotCaches            []string
	ExternalLinks          []string
}

// Guard refuses e when it would cut or move a construct on the refused
// list that lives outside the sheet's own XML (each a *Refused naming
// it): a pivot table on the sheet (a pivotTable relationship type in
// SheetRelTypes) or a pivot cache whose worksheetSource names the sheet
// at or after the point; drawing anchors (xdr:from/xdr:to rows or
// columns) at or after the point; VML anchors (x:Anchor, x:Row,
// x:Column) and comments (comment@ref, threaded comment@ref) at or
// after the point; an external link part whose sheetNames list the
// sheet. Nothing at or after the point is never a refusal.
func Guard(p Parts, sheet string, e formula.Edit) error {
	for _, rel := range p.SheetRelTypes {
		if rel == ctypes.RelPivotTable {
			return &Refused{Construct: "a pivot table on " + sheet}
		}
	}
	groups := []struct {
		kind  string
		parts []string
	}{
		{"drawing", []string{p.Drawing}}, {"VML drawing", []string{p.VML}},
		{"comments", []string{p.Comments, p.ThreadedComments}},
		{"pivot cache", p.PivotCaches}, {"external link", p.ExternalLinks},
	}
	for _, g := range groups {
		for _, src := range g.parts {
			if src == "" {
				continue
			}
			v := &guardVisitor{kind: g.kind, sheet: sheet, edit: e}
			if err := tree.ScanString(src, v); err != nil {
				return fmt.Errorf("checking %s: %w", g.kind, err)
			}
			if v.hit {
				return &Refused{Construct: "a " + g.kind + " on " + sheet}
			}
		}
	}
	return nil
}

type guardVisitor struct {
	kind, sheet string
	edit        formula.Edit
	stack       []string
	text        strings.Builder
	hit         bool
}

// rows reports whether the edit inserts or deletes rows.
func (v *guardVisitor) rows() bool {
	return v.edit.Kind == formula.InsertRows || v.edit.Kind == formula.DeleteRows
}

// rangeHit reports whether reference s reaches the edit point, erroring on a bad reference.
func (v *guardVisitor) rangeHit(s string) (bool, error) {
	r, ok := formula.ParseRange(s)
	if !ok {
		return false, fmt.Errorf("invalid reference %q", s)
	}
	if v.rows() {
		if r.Kind == formula.Cols {
			return true, nil
		}
		return max(r.From.Row, r.To.Row) >= v.edit.At, nil
	}
	if r.Kind == formula.Rows {
		return true, nil
	}
	return max(r.From.Col, r.To.Col) >= v.edit.At, nil
}

// Start records whether an element of the visited part reaches the edit point.
func (v *guardVisitor) Start(n *tree.Node) error {
	name := n.Local()
	v.stack = append(v.stack, name)
	v.text.Reset()
	switch v.kind {
	case "pivot cache":
		if name == "worksheetSource" {
			s, _ := n.Attr("sheet")
			if strings.EqualFold(s, v.sheet) {
				ref, ok := n.Attr("ref")
				if !ok {
					v.hit = true
				} else {
					hit, err := v.rangeHit(ref)
					if err != nil {
						return err
					}
					v.hit = v.hit || hit
				}
			}
		}
	case "external link":
		if name == "sheetName" && len(v.stack) >= 2 && v.stack[len(v.stack)-2] == "sheetNames" {
			s, _ := n.Attr("val")
			v.hit = v.hit || strings.EqualFold(s, v.sheet)
		}
	case "comments":
		if name == "comment" || name == "threadedComment" {
			ref, ok := n.Attr("ref")
			if ok {
				hit, err := v.rangeHit(ref)
				if err != nil {
					return err
				}
				v.hit = v.hit || hit
			}
		}
	}
	return nil
}

// End records whether a drawing or VML anchor coordinate reaches the edit point.
func (v *guardVisitor) End(n *tree.Node) error {
	name := n.Local()
	text := strings.TrimSpace(v.text.String())
	if v.kind == "drawing" && len(v.stack) >= 2 && (v.stack[len(v.stack)-2] == "from" || v.stack[len(v.stack)-2] == "to") && ((v.rows() && name == "row") || (!v.rows() && name == "col")) {
		if err := v.coordinate(text); err != nil {
			return err
		}
	}
	if v.kind == "VML drawing" {
		if (v.rows() && name == "Row") || (!v.rows() && name == "Column") {
			if err := v.coordinate(text); err != nil {
				return err
			}
		}
		if name == "Anchor" {
			fields := strings.Split(text, ",")
			if len(fields) != 8 {
				return fmt.Errorf("invalid VML anchor %q", text)
			}
			indices := []int{0, 4}
			if v.rows() {
				indices = []int{2, 6}
			}
			for _, i := range indices {
				if err := v.coordinate(strings.TrimSpace(fields[i])); err != nil {
					return err
				}
			}
		}
	}
	v.stack = v.stack[:len(v.stack)-1]
	v.text.Reset()
	return nil
}

// Leaf accumulates text-node content for a later anchor check.
func (v *guardVisitor) Leaf(n *tree.Node) error {
	if n.Kind() == tree.TextNode {
		v.text.WriteString(n.RawString())
	}
	return nil
}

// coordinate records an anchor coordinate at or after the edit point, erroring when invalid.
func (v *guardVisitor) coordinate(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid anchor coordinate %q", s)
	}
	if n >= v.edit.At-1 {
		v.hit = true
	}
	return nil
}
