package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestRangeClearChunk(t *testing.T) {
	rows := `<row r="1"><c r="A1" s="1"><v>9</v></c><c r="B1"><v>8</v></c></row>`
	r := runOps(t, book(rows), &ops.RangeClear{At: ops.AtRange("Budget!A:B")}).ok()
	if x := r.sheet(1); !strings.Contains(x, `<c r="A1" s="1"/>`) || strings.Contains(x, `r="B1"`) {
		t.Fatalf("clear: %s", x)
	}
	if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "Budget!A1:B1" {
		t.Fatalf("changed: %+v", r.env[0].Changed)
	}
}

func TestRangeClearChunkRefusals(t *testing.T) {
	array := `<row r="1"><c r="A1"><f t="array" ref="A1:A2">B1:B2</f><v>1</v></c></row><row r="2"><c r="A2"><v>2</v></c></row>`
	runOps(t, book(array), &ops.RangeClear{At: ops.AtRange("Budget!A2")}).refused(0, cli.CodeOperationFailed, "at")
	b := book(`<row r="1"><c r="A1"><v>1</v></c></row>`)
	b.Sheets[0].Tables = []string{`<table id="1" name="T" displayName="T" ref="A1:B2"><tableColumns count="2"><tableColumn id="1" name="H1"/><tableColumn id="2" name="H2"/></tableColumns></table>`}
	runOps(t, b, &ops.RangeClear{At: ops.AtRange("Budget!B1")}).refused(0, cli.CodeOperationFailed, "at")
	runOps(t, book(""), &ops.RangeClear{At: ops.AtRange("Missing!A1")}).refused(0, cli.CodeNotFound, "at")
}
