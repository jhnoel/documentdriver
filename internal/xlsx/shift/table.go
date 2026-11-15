package shift

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// Table rewrites a table part (xml its whole text) on the edited sheet
// for e: a row edit moves or grows table@ref, autoFilter@ref and
// sortState@ref (and sortCondition@ref) (R9); a column edit that
// crosses the table, a delete of its header row or of its last data
// row, and a row delete that removes it whole are *Refused. A column
// edit wholly left of the table moves it; wholly right leaves it. The
// output is xml itself when nothing changes. It reads the part with the
// lexer conventions of the sheet code (no tree: the function is pure
// over bytes) and copies every byte it does not rewrite.
func Table(xml string, e formula.Edit) (string, error) {
	// Validate the edit even when no reference needs moving.
	if _, err := formula.Shift("", formula.Sheets{Own: e.Sheet}, e); err != nil {
		return "", err
	}
	type ref struct {
		start, end     int
		value, element string
	}
	var refs []ref
	var stack []string
	for i := 0; i < len(xml); {
		t, err := xmlscan.Next(xml, i)
		if err != nil {
			return "", err
		}
		i = t.End
		switch t.Kind {
		case xmlscan.Text, xmlscan.Comment, xmlscan.CData, xmlscan.PI, xmlscan.Doctype:
			continue
		case xmlscan.EndTag:
			if len(stack) == 0 {
				return "", fmt.Errorf("table: unexpected end tag")
			}
			stack = stack[:len(stack)-1]
			continue
		}
		name := t.Local(xml)
		wanted := len(stack) == 0 && name == "table" || len(stack) == 1 && stack[0] == "table" && (name == "autoFilter" || name == "sortState") || len(stack) == 2 && stack[0] == "table" && (stack[1] == "sortState" || stack[1] == "autoFilter") && name == "sortState" || len(stack) >= 2 && stack[0] == "table" && stack[len(stack)-1] == "sortState" && name == "sortCondition"
		if wanted {
			if a, ok := xmlscan.FindAttr(xml, t, "ref"); ok {
				refs = append(refs, ref{a.ValueStart, a.ValueEnd, a.Raw(xml), name})
			}
		}
		if t.Kind == xmlscan.StartTag {
			stack = append(stack, name)
		}
	}
	if len(stack) != 0 {
		return "", fmt.Errorf("table: unclosed tag")
	}
	if len(refs) == 0 || refs[0].element != "table" {
		return "", fmt.Errorf("table: missing table ref")
	}
	r, bad, ok := formula.ParseRefBody(refs[0].value)
	if !ok || bad || r.Kind != formula.Cells {
		return "", fmt.Errorf("table: invalid ref %q", refs[0].value)
	}
	loRow, hiRow := min(r.From.Row, r.To.Row), max(r.From.Row, r.To.Row)
	loCol, hiCol := min(r.From.Col, r.To.Col), max(r.From.Col, r.To.Col)
	end := e.At + e.Count - 1
	if (e.Kind == formula.InsertCols && e.At > loCol && e.At <= hiCol) || (e.Kind == formula.DeleteCols && e.At <= hiCol && end >= loCol) || (e.Kind == formula.DeleteRows && ((e.At <= loRow && end >= loRow) || (e.At <= hiRow && end >= loRow+1 && e.At <= loRow+1 && end >= hiRow))) {
		return "", &Refused{Construct: "a table on " + e.Sheet}
	}
	var edits []xmlscan.Edit
	for _, a := range refs {
		_, bad, ok := formula.ParseRefBody(a.value)
		if !ok || bad {
			return "", fmt.Errorf("table: invalid %s ref %q", a.element, a.value)
		}
		shifted, err := formula.Shift(a.value, formula.Sheets{Own: e.Sheet}, e)
		if err != nil {
			return "", err
		}
		if strings.Contains(shifted, "#REF!") {
			return "", &Refused{Construct: "a table on " + e.Sheet}
		}
		if shifted != a.value {
			edits = append(edits, xmlscan.Edit{Start: a.start, End: a.end, Text: shifted})
		}
	}
	return xmlscan.Apply(xml, edits)
}
