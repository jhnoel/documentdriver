package ops

import (
	"fmt"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// Address builders for tests and NewPlan. Each parses s as the plan
// decoder does and panics on an address that does not parse (a test's
// own mistake, never input).

// mustRef parses s as a reference, panicking when it does not parse.
func mustRef(s string) model.Ref {
	r, err := model.ParseRef(s)
	if err != nil {
		panic(fmt.Sprintf("ops: %v", err))
	}
	return r
}

// AtCell is CellAddress s ("Budget!B2").
func AtCell(s string) CellAddress {
	r := mustRef(s)
	return CellAddress{Raw: s, Sheet: r.Sheet, Row: r.Range.From.Row, Col: r.Range.From.Col}
}

// AtRange is RangeAddress s.
func AtRange(s string) RangeAddress { return rangeOf(s, mustRef(s)) }

// AtRows is RowsAddress s ("Budget!5:7").
func AtRows(s string) RowsAddress {
	r := mustRef(s)
	r1, _, r2, _ := r.Range.Bounds()
	return RowsAddress{Raw: s, Sheet: r.Sheet, From: r1, To: r2}
}

// AtCols is ColsAddress s ("Budget!C:E").
func AtCols(s string) ColsAddress {
	r := mustRef(s)
	_, c1, _, c2 := r.Range.Bounds()
	return ColsAddress{Raw: s, Sheet: r.Sheet, From: c1, To: c2}
}

// AtText is TextScope s (a range or "sheet:Name").
func AtText(s string) TextScope {
	r := mustRef(s)
	if r.Kind == model.RefSheet {
		return TextScope{RangeAddress: RangeAddress{Raw: s, Sheet: r.Sheet, Kind: formula.Cells, R1: 1, C1: 1, R2: formula.MaxRows, C2: formula.MaxCols}, Whole: true}
	}
	return TextScope{RangeAddress: rangeOf(s, r)}
}

// AtSheet is SheetAddress s ("sheet:Budget" or "$name").
func AtSheet(s string) SheetAddress {
	if len(s) > 1 && s[0] == '$' {
		return SheetAddress{Raw: s, Alias: s[1:]}
	}
	return SheetAddress{Raw: s, Name: mustRef(s).Sheet}
}

// AtName is NameAddress s ("name:Tax", "name:Budget!Local").
func AtName(s string) NameAddress {
	r := mustRef(s)
	return NameAddress{Raw: s, Name: r.Name, Sheet: r.Sheet}
}

// Ptr returns a pointer to v, for optional fields.
func Ptr[T any](v T) *T { return &v }
