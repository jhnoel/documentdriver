package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// --recalc:G17, G18: --recalc: --recalc, only with DD_SOFFICE.
func TestRecalcSplicesValues(t *testing.T) {
	bin := os.Getenv("DD_SOFFICE")
	if bin == "" {
		t.Skip("DD_SOFFICE is not set")
	}
	xlsxtest.Skip(t, func() {
		in, _ := e2eBook(t)
		out := filepath.Join(t.TempDir(), "out.xlsx")
		_, err := Run(in, ops.NewPlan(&ops.CellSet{At: ops.AtCell("Budget!C1"), Value: ops.Fx("=B1+1")}), Options{Out: out, Recalc: &RecalcOptions{Soffice: bin}})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(out) //nolint:gosec // test output
		parts, _ := xlsxtest.Parts(data)
		if s := string(parts["xl/worksheets/sheet1.xml"]); !strings.Contains(s, `<c r="C1"><f>B1+1</f><v>5</v></c>`) {
			t.Errorf("value not spliced: %s", s)
		}
	})
}
