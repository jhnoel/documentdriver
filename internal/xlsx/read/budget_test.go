package read

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

// bombBook is the shape of apache-poi's poc-shared-strings.xlsx: many
// cells referencing one long shared string (item 0, n characters), cols
// cells a row; plus a shared formula whose master is longer than Excel
// allows, filled down column Z.
func bombBook(n, rows, cols int) fx {
	var b strings.Builder
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&b, `<row r="%d">`, r)
		for c := 1; c <= cols; c++ {
			fmt.Fprintf(&b, `<c r="%s%d" t="s"><v>0</v></c>`, colName(c), r)
		}
		if r == 1 {
			fmt.Fprintf(&b, `<c r="Z1"><f t="shared" ref="Z1:Z%d" si="0">%s</f><v>0</v></c>`, rows, strings.Repeat("A1+", 3000)+"1")
		} else {
			fmt.Fprintf(&b, `<c r="Z%d"><f t="shared" si="0"/><v>0</v></c>`, r)
		}
		b.WriteString(`</row>`)
	}
	return fx{sst: []string{"<t>" + strings.Repeat("x", n) + "</t>"}, sheets: []fxSheet{{name: "B", rows: b.String()}}}
}

// TestTooLong: text past model.MaxText and formulas past MaxFormula are
// left out with one aggregated note each, and never multiply into the
// output.
func TestTooLong(t *testing.T) {
	skipIfUnimplemented(t, probeCells)
	input := bombBook(model.MaxText+1, 200, 3).zip(t)
	res := readCells(t, input, CellsRequest{Sheet: "sheet:B", Limit: 0})
	got, _ := cellsJSON(t, res)
	for k, v := range got {
		if !strings.HasPrefix(k, "Z") || v != `0` {
			t.Errorf("%s = %.40s", k, v)
		}
	}
	if len(got) != 200 {
		t.Errorf("%d cells, want the 200 cached values of column Z", len(got))
	}
	k := noteKinds(res.Notes)
	if k[model.NoteValue] != 1 || k[model.NoteFormula] != 1 {
		t.Errorf("notes %+v", res.Notes)
	}
	// Exactly MaxText is shown.
	res = readCells(t, bombBook(model.MaxText, 1, 1).zip(t), CellsRequest{Range: "B!A1", Limit: 0})
	if res.Cells.Len() != 1 {
		t.Error("a text of exactly 32,767 characters is left out")
	}
}

// TestOutputBudget: a read stops at a row boundary at Options.MaxOutput
// as at --limit, a row alone past it is refused, and find stops too.
func TestOutputBudget(t *testing.T) {
	skipIfUnimplemented(t, probeAll)
	input := bombBook(30000, 100, 3).zip(t)
	opt := Options{MaxOutput: 1 << 20}
	res, err := Cells(input, CellsRequest{Sheet: "sheet:B", Limit: DefaultLimit}, opt)
	if err != nil {
		t.Fatal(err)
	}
	// 30,000 bytes a cell, 3 a row plus Z: 11 rows fit in 1 MiB.
	if res.Truncated == nil || res.Truncated.Next != "B!A12:Z100" || res.Cells.Len() != 44 {
		t.Errorf("truncated %+v after %d cells", res.Truncated, res.Cells.Len())
	}
	if b := encode(t, res); len(b) > 1<<20+4096 {
		t.Errorf("output %d bytes past the budget", len(b))
	}
	if n := noteKinds(res.Notes)[model.NoteTruncated]; n != 1 || !strings.Contains(res.Notes[len(res.Notes)-1].Reason+fmt.Sprint(res.Notes), "output limit") {
		t.Errorf("notes %+v", res.Notes)
	}
	_, err = Cells(bombBook(30000, 2, 40).zip(t), CellsRequest{Sheet: "sheet:B", Limit: DefaultLimit}, opt)
	if code, path := errCode(err); code != cli.CodeOperationFailed || path != "--sheet" {
		t.Errorf("a row past the budget: %q at %q (%v)", code, path, err)
	}
	fres, err := Find(input, FindRequest{Query: "x", View: model.ViewValues, Limit: DefaultLimit}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if fres.Truncated == nil || len(fres.Matches) < 30 || len(fres.Matches) > 40 {
		t.Errorf("find: %d matches, truncated %+v", len(fres.Matches), fres.Truncated)
	}
}
