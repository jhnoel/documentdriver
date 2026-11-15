package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestColumnFormatMissingSheet(t *testing.T) {
	runOps(t, book(""), &ops.ColumnFormat{At: ops.AtCols("Missing!C:E"), Hidden: ops.Ptr(true)}).refused(0, cli.CodeNotFound, "at")
}

func TestColumnFormatCreatesCols(t *testing.T) {
	xlsxtest.Skip(t, func() {
		r := runOps(t, book(""), &ops.ColumnFormat{At: ops.AtCols("Budget!C:E"), Hidden: ops.Ptr(true)}).ok()
		if x := r.sheet(1); !strings.Contains(x, `<col min="3" max="5" hidden="1"`) {
			t.Errorf("missing hidden columns in %s", x)
		}
		if len(r.env[0].Changed) != 0 {
			t.Errorf("column formatting reported changed units: %v", r.env[0].Changed)
		}
	})
}
