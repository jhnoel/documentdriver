package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestRowFormatChunk(t *testing.T) {
	r := runOps(t, book(`<row r="2" hidden="1"><c r="A2"><v>1</v></c></row>`),
		&ops.RowFormat{At: ops.AtRows("Budget!2:3"), Height: ops.Ptr(30.5), Hidden: ops.Ptr(false)},
		&ops.RowFormat{At: ops.AtRows("Budget!3:3"), Hidden: ops.Ptr(true)}).ok()
	x := r.sheet(1)
	for _, want := range []string{`<row r="2" ht="30.5" customHeight="1"><c r="A2"><v>1</v></c></row>`, `<row r="3" ht="30.5" customHeight="1" hidden="1"/>`} {
		if !strings.Contains(x, want) {
			t.Errorf("missing %s in %s", want, x)
		}
	}
	for _, env := range r.env {
		if len(env.Changed) != 0 {
			t.Errorf("row formatting reported changed units: %v", env.Changed)
		}
	}
}

func TestRowFormatChunkMissingSheet(t *testing.T) {
	runOps(t, book(""), &ops.RowFormat{At: ops.AtRows("Missing!1:1"), Hidden: ops.Ptr(true)}).refused(0, cli.CodeNotFound, "at")
}
