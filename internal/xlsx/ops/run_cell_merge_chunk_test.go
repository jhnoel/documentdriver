package ops_test

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestCellMergeChunkRefusals(t *testing.T) {
	array := book(`<row r="1"><c r="A1"><f t="array" ref="A1:A3">B1:B3</f></c></row>`)
	table := book("")
	table.Sheets[0].Tables = []string{`<table id="1" name="T" displayName="T" ref="A1:B3"><tableColumns count="1"><tableColumn id="1" name="H"/></tableColumns></table>`}
	t.Run("array member", func(t *testing.T) {
		runOps(t, array, &ops.CellMerge{At: ops.AtRange("Budget!A2:B2")}).refused(0, cli.CodeOperationFailed, "at")
	})
	t.Run("table body", func(t *testing.T) {
		runOps(t, table, &ops.CellMerge{At: ops.AtRange("Budget!B2:C2")}).refused(0, cli.CodeOperationFailed, "at")
	})
	t.Run("missing sheet", func(t *testing.T) {
		runOps(t, book(""), &ops.CellMerge{At: ops.AtRange("Missing!A1:B1")}).refused(0, cli.CodeNotFound, "at")
	})
}
