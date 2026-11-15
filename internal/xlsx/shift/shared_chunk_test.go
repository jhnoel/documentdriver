package shift

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

func TestPlanSharedChunkRefErrors(t *testing.T) {
	c := func(r int) formula.Cell { return formula.Cell{Row: r, Col: 2} }
	g := SharedGroup{SI: 3, Master: c(1), Text: "$A$2", Ref: formula.Range{Kind: formula.Cells, From: c(1), To: c(3)}, Members: []formula.Cell{c(1), c(2), c(3)}}
	got, stats, err := PlanShared([]SharedGroup{g}, 4, formula.Sheets{Own: "S"}, formula.Edit{Sheet: "S", Kind: formula.DeleteRows, At: 2, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || stats.Adjusted != 1 || len(stats.RefErrors) != 2 {
		t.Fatalf("got %v stats %+v", got, stats)
	}
}
