package apply

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

// The rebase of earlier units and aliases (designer-owned; must pass).
func TestRebase(t *testing.T) {
	unit := func(r1, c1, r2, c2 int) ops.Unit {
		e := &ops.Env{}
		e.ChangedRange("Budget", r1, c1, r2, c2)
		return e.Changed[0]
	}
	res := &Result{Aliases: map[string]string{"b": "sheet:Budget"}, Units: []ops.Unit{
		unit(3, 1, 4, 2), unit(1, 1, 1, 1), unit(5, 1, 5, 1), unit(4, 1, 7, 1),
		{Kind: ops.UnitSheet, ID: "sheet:Budget", Sheet: "Budget"}, {Kind: ops.UnitName, ID: "name:Budget!Local"},
	}}
	rebase(res, []formula.Edit{
		{Sheet: "Budget", Kind: formula.InsertRows, At: 2, Count: 2}, // A3:B4 -> A5:B6
		{Sheet: "budget", Kind: formula.DeleteRows, At: 7, Count: 1}, // A7 (was A5) goes; A6:A9 -> A6:A8
	}, [][2]string{{"Budget", "Q1 Budget"}})
	var got []string
	for _, u := range res.Units {
		got = append(got, u.ID)
	}
	want := []string{"'Q1 Budget'!A5:B6", "'Q1 Budget'!A1", "'Q1 Budget'!A6:A8", "sheet:Q1 Budget", "name:'Q1 Budget'!Local"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unit %d: %s, want %s", i, got[i], want[i])
		}
	}
	if res.Aliases["b"] != "sheet:Q1 Budget" {
		t.Errorf("alias %s", res.Aliases["b"])
	}
}
