package ops_test

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestTextReplaceChunk(t *testing.T) {
	b := book(`<row r="1"><c r="A1" t="inlineStr" s="1"><is><t>_x0063_at cat</t></is></c><c r="B1" t="inlineStr"><is><r><t>cat</t></r></is></c></row>`)
	t.Run("occurrence and style", func(t *testing.T) {
		r := runOps(t, b, &ops.TextReplace{At: ops.AtText("Budget!A1:B1"), Select: "cat", With: "dog", Occurrence: ops.Ptr(uint32(2))}).ok()
		if got := r.cells("Budget!A1")["A1"]; got != `"cat dog"` {
			t.Fatal(got)
		}
		if len(r.env[0].Changed) != 1 || r.env[0].Changed[0].ID != "Budget!A1" {
			t.Fatal(r.env[0].Changed)
		}
	})
	t.Run("ambiguity count", func(t *testing.T) {
		r := runOps(t, b, &ops.TextReplace{At: ops.AtText("sheet:Budget"), Select: "cat", With: "dog"})
		r.refused(0, cli.CodeAmbiguous, "select")
		var e *ops.Rejection
		if !errors.As(r.err, &e) || e.Matches != 3 {
			t.Fatal(r.err)
		}
	})
	t.Run("inline rich", func(t *testing.T) {
		r := runOps(t, b, &ops.TextReplace{At: ops.AtText("Budget!B1"), Select: "cat", With: "dog"})
		r.refused(0, cli.CodeOperationFailed, "at")
	})
	t.Run("identity", func(t *testing.T) {
		r := runOps(t, b, &ops.TextReplace{At: ops.AtText("Budget!B1"), Select: "cat", With: "cat"}).ok()
		if len(r.env[0].Changed) != 0 {
			t.Fatal(r.env[0].Changed)
		}
	})
}
