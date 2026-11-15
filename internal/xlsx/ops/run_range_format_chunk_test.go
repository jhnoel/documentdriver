package ops_test

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestRangeFormatChunkCap(t *testing.T) {
	runOps(t, book(""), &ops.RangeFormat{At: ops.AtRange("Budget!A1:A100001"), Bold: ops.Ptr(true)}).refused(0, cli.CodeOperationFailed, "at")
}

func TestRangeFormatChunkMissingSheet(t *testing.T) {
	runOps(t, book(""), &ops.RangeFormat{At: ops.AtRange("Missing!A1"), Bold: ops.Ptr(true)}).refused(0, cli.CodeNotFound, "at")
}
