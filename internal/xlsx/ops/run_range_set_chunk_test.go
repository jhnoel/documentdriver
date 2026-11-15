package ops_test

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestRangeSetChunkRefusals(t *testing.T) {
	array := `<row r="1"><c r="B1"><f t="array" ref="B1:B2">1</f><v>1</v></c></row><row r="2"><c r="B2"><v>1</v></c></row>`
	t.Run("cut before writing", func(t *testing.T) {
		runOps(t, book(array), &ops.RangeSet{At: ops.AtRange("Budget!A2:B2"), Values: ops.Grid{{ops.Num(1), ops.Num(2)}}}).refused(0, cli.CodeOperationFailed, "at")
	})
	t.Run("missing sheet", func(t *testing.T) {
		runOps(t, book(""), &ops.RangeSet{At: ops.AtRange("Missing!A1"), Values: ops.Grid{{ops.Num(1)}}}).refused(0, cli.CodeNotFound, "at")
	})
}
