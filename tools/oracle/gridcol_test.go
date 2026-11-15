package main

import (
	"strings"
	"testing"
)

// TestGridColumns: a cell.merge or cell.split address is the cell's
// position in its row for the Go CLI and the grid column it starts at for
// docxdriver; mixed.docx's table has a two-column cell first in row 2 and
// a vertical merge in its third grid column.
func TestGridColumns(t *testing.T) {
	src := writeFixtures()["mixed.docx"]
	for _, c := range []struct{ at, want string }{
		{"table:1/row:1/cell:3", "table:1/row:1/cell:3"},
		{"table:1/row:2/cell:2", "table:1/row:2/cell:3"},
		{"table:1/row:2/cell:9", "table:1/row:2/cell:9"},
		{"table:7/row:1/cell:1", "table:7/row:1/cell:1"},
	} {
		plan := `{"ops":[{"op":"cell.split","at":"` + c.at + `"},{"op":"row.delete","at":"` + c.at + `","count":1}]}`
		got, err := gridColumns(plan, src)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, `{"op":"cell.split","at":"`+c.want+`"}`) || !strings.Contains(got, `"at":"`+c.at+`","count"`) {
			t.Errorf("%s: %s", c.at, got)
		}
	}
}
