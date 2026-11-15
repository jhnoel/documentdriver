package ops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/edit"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// TextReplace is text.replace.
type TextReplace struct {
	At         TextScope `json:"at" doc:"A range (Budget!A1:D10) or a sheet (sheet:Budget)."`
	Select     string    `json:"select" doc:"Exact text to find: case-sensitive, no normalisation."`
	With       string    `json:"with" doc:"The replacement text."`
	Occurrence *uint32   `json:"occurrence" default:"null" doc:"1-based: which match, counting every match of every cell in row-major order."`
	All        bool      "json:\"all,omitzero\" default:\"false\" doc:\"Replace every match. Not with `occurrence`.\""
}

// opTextReplace is text.replace's entry in the operation table.
var opTextReplace = OpDef{
	Name: "text.replace", New: func() Spec { return &TextReplace{} },
	Summary: "Replaces exact text inside text cells of a range or a whole sheet. A cell matches when its text contains select; occurrence picks the n-th match in row-major order, all replaces every match. Formulas, numbers and other values are not searched.",
	Rules:   textReplaceRules,
}

// Run (chunk G4) reads the cells of the scope (Sheet.Each) whose value
// is text: shared strings (through the workspace's SST, decoded and
// _xHHHH_-unescaped), inline strings and t="str" formula results are
// not searched (a formula cell is never a match). Matches are counted
// per occurrence of select in a cell's text, cells in row-major order.
//
//   - Without occurrence or all: one match is replaced; none is
//     not_found (path select); more is ambiguous with Matches (path
//     select).
//   - occurrence n with fewer matches: not_found (path occurrence).
//   - A rich-text cell (a shared string with runs <r>, or an inline
//     string with runs) that would change is operation_failed (path
//     at): its runs cannot be kept.
//   - A changed cell is written as an inline string (Sheet.RenderCell
//     with its s kept); the shared strings are never changed (decision
//     11).
//   - A cell in a table's header row that would change is
//     operation_failed (decision 13).
//   - Changed: each changed cell, as a one-cell range.
func (o *TextReplace) Run(env *Env) error {
	s, err := env.Sheet(o.At.Sheet, "at")
	if err != nil {
		return err
	}
	if o.Select == "" || (o.Occurrence != nil && (*o.Occurrence == 0 || o.All)) {
		return Refuse(cli.CodeInvalidOperation, "select", "invalid text selection")
	}
	type candidate struct {
		cell         edit.Cell
		text         string
		first, count int
		index        int
	}
	var candidates []candidate
	var sst *sheet.SST
	matches := 0
	err = s.Each(o.At.R1, o.At.C1, o.At.R2, o.At.C2, func(c edit.Cell) error {
		if c.Raw.F.Present {
			return nil
		}
		var text string
		index := -1
		switch c.Raw.T {
		case "inlineStr":
			if !c.Raw.HasIS {
				return nil
			}
			text = sml.UnescapeX(c.Raw.IS)
		case "s":
			if !c.Raw.HasV {
				return nil
			}
			index, err = strconv.Atoi(c.Raw.V)
			if err != nil || index < 0 {
				return &edit.Unreadable{Target: model.Cell(s.Name(), c.Row, c.Col), Detail: "invalid shared string index"}
			}
			if sst == nil {
				sst, err = env.WS.SST()
				if err != nil {
					return err
				}
			}
			if sst == nil {
				return &edit.Unreadable{Target: model.Cell(s.Name(), c.Row, c.Col), Detail: "shared strings are missing"}
			}
			text, err = sst.Text(index)
			if err != nil {
				return &edit.Unreadable{Target: model.Cell(s.Name(), c.Row, c.Col), Detail: err.Error()}
			}
		default:
			return nil
		}
		n := strings.Count(text, o.Select)
		if n > 0 {
			candidates = append(candidates, candidate{c, text, matches, n, index})
			matches += n
		}
		return nil
	})
	if err != nil {
		return FromEdit(err, "at")
	}
	if o.Occurrence != nil && uint64(*o.Occurrence) > uint64(matches) {
		return Refuse(cli.CodeNotFound, "occurrence", "occurrence not found")
	}
	if matches == 0 {
		return Refuse(cli.CodeNotFound, "select", "text not found")
	}
	if !o.All && o.Occurrence == nil && matches > 1 {
		return &Rejection{Code: cli.CodeAmbiguous, Field: "select", Msg: "text selection is ambiguous", Matches: matches}
	}
	tables, err := env.WS.Tables(s.Name())
	if err != nil {
		return FromEdit(err, "at")
	}
	type change struct {
		row, col int
		xml      string
	}
	var changes []change
	for _, c := range candidates {
		text := c.text
		if o.All {
			text = strings.ReplaceAll(text, o.Select, o.With)
		} else {
			n := 1
			if o.Occurrence != nil {
				n = int(*o.Occurrence)
			}
			if n <= c.first || n > c.first+c.count {
				continue
			}
			start := 0
			for range n - c.first - 1 {
				start += strings.Index(text[start:], o.Select) + len(o.Select)
			}
			pos := start + strings.Index(text[start:], o.Select)
			text = text[:pos] + o.With + text[pos+len(o.Select):]
		}
		if text == c.text {
			continue
		}
		for _, a := range s.Index().Anchors() {
			r1, c1, r2, c2 := a.Ref.Bounds()
			if (r1 != r2 || c1 != c2) && a.Ref.Contains(c.cell.Row, c.cell.Col) {
				return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "cannot replace inside an array or data table", Detail: "an array or data table at " + a.Ref.String()}
			}
		}
		for _, t := range tables {
			r1, _, _, _ := t.Ref.Bounds()
			if t.Ref.Contains(c.cell.Row, c.cell.Col) && c.cell.Row-r1 < t.HeaderRows {
				return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "cannot replace a table header", Detail: "the header of table " + t.Name}
			}
		}
		rich := false
		if c.index >= 0 {
			rich, err = sst.Rich(c.index)
		} else {
			var cell edit.Cell
			cell, err = s.Cell(c.cell.Row, c.cell.Col)
			if err == nil {
				v := &inlineRuns{prefix: s.Prefix()}
				err = tree.ScanString(cell.XML, v)
				rich = v.rich
			}
		}
		if err != nil {
			return FromEdit(err, "at")
		}
		if rich {
			return &Rejection{Code: cli.CodeOperationFailed, Field: "at", Msg: "cannot replace rich text", Detail: "rich text at " + model.Cell(s.Name(), c.cell.Row, c.cell.Col)}
		}
		if !c.cell.Raw.SOK {
			return &edit.Unreadable{Target: model.Cell(s.Name(), c.cell.Row, c.cell.Col), Detail: "invalid cell style"}
		}
		x, err := edit.RenderCell(s.Prefix(), c.cell.Row, c.cell.Col, c.cell.Raw.S, edit.Content{Kind: edit.ContentString, Text: text})
		if err != nil {
			return fmt.Errorf("render replacement: %w", err)
		}
		changes = append(changes, change{c.cell.Row, c.cell.Col, x})
	}
	for _, c := range changes {
		if err := s.SetCell(c.row, c.col, c.xml); err != nil {
			return FromEdit(err, "at")
		}
		env.ChangedRange(s.Name(), c.row, c.col, c.row, c.col)
	}
	return nil
}

// inlineRuns detects direct run children of the inline string without building a tree.
type inlineRuns struct {
	prefix         string
	depth, isDepth int
	rich           bool
}

// Start tracks nesting and marks rich inline strings with direct run children.
func (v *inlineRuns) Start(n *tree.Node) error {
	v.depth++
	if v.depth == 2 && n.Name() == v.prefix+"is" {
		v.isDepth = v.depth
	}
	if v.isDepth > 0 && v.depth == v.isDepth+1 && n.Name() == v.prefix+"r" {
		v.rich = true
	}
	return nil
}

// End unwinds nesting and clears the inline-string depth when its element closes.
func (v *inlineRuns) End(_ *tree.Node) error {
	if v.depth == v.isDepth {
		v.isDepth = 0
	}
	v.depth--
	return nil
}

// Leaf does nothing, as inlineRuns only tracks element nesting.
func (*inlineRuns) Leaf(_ *tree.Node) error { return nil }
