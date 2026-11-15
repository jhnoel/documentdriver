package create

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/integrity"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K14 (Template) and G16 (Run) (designer rows; frozen).

func TestTemplate(t *testing.T) {
	xlsxtest.Skip(t, func() {
		a, err := Template()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := Template()
		if !bytes.Equal(a, b) {
			t.Error("the template is not reproducible")
		}
		if f := integrity.Check(a); !f.Empty() {
			t.Errorf("template findings: %v", f.Kinds())
		}
		wb, err := read.Workbook(a, read.Options{})
		if err != nil || len(wb.Sheets) != 1 || wb.Sheets[0].Name != "Sheet1" {
			t.Errorf("read: %+v %v", wb, err)
		}
	})
}

func TestRun(t *testing.T) {
	xlsxtest.Skip(t, func() {
		dir := t.TempDir()
		blank := filepath.Join(dir, "blank.xlsx")
		res, err := Run(nil, Options{Out: blank})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(blank) //nolint:gosec // test output
		tmpl, _ := Template()
		if !bytes.Equal(data, tmpl) || res.NewBase != cli.Hash(data) || res.Saved != blank {
			t.Errorf("blank create: %+v", res)
		}
		out := filepath.Join(dir, "made.xlsx")
		p := ops.NewPlan(&ops.CellSet{At: ops.AtCell("Sheet1!A1"), Value: ops.Str("Item")})
		if _, err := Run(p, Options{Out: out}); err != nil {
			t.Fatal(err)
		}
		made, _ := os.ReadFile(out) //nolint:gosec // test output
		c, err := read.Cells(made, read.CellsRequest{Range: "Sheet1!A1"}, read.Options{})
		if err != nil || c.Cells.Len() != 1 {
			t.Errorf("read of the created workbook: %v", err)
		}
		if _, err := Run(&ops.Plan{Base: cli.Hash(made), Ops: p.Ops}, Options{Out: out}); err == nil {
			t.Error("a create plan with base was accepted")
		}
		dry := filepath.Join(dir, "dry.xlsx")
		if _, err := Run(p, Options{Out: dry, DryRun: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dry); !errors.Is(err, os.ErrNotExist) {
			t.Error("a dry run wrote a file")
		}
	})
}
