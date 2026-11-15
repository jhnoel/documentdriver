package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/integrity"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/read"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The plan.Begin and FinishSealed wiring, with test-only operations that
// edit through the edit layer directly and a full-check gate (the scoped
// gate and the recalculation policy are chunks 6 and 5). These tests
// must pass on the skeleton.

// styleOp sets a cell's style: a change that marks no value, so the
// recalculation policy has nothing to do.
type styleOp struct {
	Sheet    string
	Row, Col int
	Style    int
	Fail     bool
}

func (o *styleOp) Run(env *ops.Env) error {
	if o.Fail {
		return ops.Refuse(cli.CodeOperationFailed, "at", "refused on purpose")
	}
	s, err := env.Sheet(o.Sheet, "at")
	if err != nil {
		return err
	}
	if err := s.SetCellStyle(o.Row, o.Col, o.Style); err != nil {
		return err
	}
	env.ChangedRange(o.Sheet, o.Row, o.Col, o.Row, o.Col)
	return nil
}

func init() {
	ops.Ops = append(ops.Ops, ops.OpDef{Name: "test.style", New: func() ops.Spec { return &styleOp{} }})
}

func fullGate(in, out []byte, _ integrity.Scope) (*integrity.Findings, error) {
	after := integrity.Check(out)
	return after, integrity.GateFindings(integrity.Check(in), after)
}

func wiringBook(t *testing.T) string {
	t.Helper()
	b := xlsxtest.Book{XFs: []int{0, 14}, SST: []string{"<t>kept</t>"}, Sheets: []xlsxtest.Sheet{
		{Name: "Budget", Dimension: "A1:B2", Rows: `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><v>2</v></c></row><row r="2"><c r="A2"><v>3</v></c></row>`},
		{Name: "Other", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`},
	}}
	p, err := b.Write(t.TempDir(), "in.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWiringCommit(t *testing.T) {
	in := wiringBook(t)
	out := filepath.Join(t.TempDir(), "out.xlsx")
	before, _ := os.ReadFile(in)
	p := ops.NewPlan(&styleOp{Sheet: "Budget", Row: 2, Col: 2, Style: 1})
	res, err := Run(in, p, Options{Out: out, gate: fullGate})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(in); !bytes.Equal(got, before) {
		t.Error("the input changed")
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != out || res.NewBase != cli.Hash(written) || res.Base != cli.Hash(before) {
		t.Errorf("result %+v", res)
	}
	if strings.Join(res.Parts, ",") != "/xl/worksheets/sheet1.xml" {
		t.Errorf("parts %v", res.Parts)
	}
	inParts, _ := xlsxtest.Parts(before)
	outParts, _ := xlsxtest.Parts(written)
	for name, b := range inParts {
		if name != "xl/worksheets/sheet1.xml" && !bytes.Equal(b, outParts[name]) {
			t.Errorf("untouched part %s changed", name)
		}
	}
	if !strings.Contains(string(outParts["xl/worksheets/sheet1.xml"]), `<row r="2"><c r="A2"><v>3</v></c><c r="B2" s="1"/></row>`) {
		t.Errorf("sheet: %s", outParts["xl/worksheets/sheet1.xml"])
	}
	if len(res.Changed) != 1 || !strings.Contains(string(res.Changed[0]), `"id":"Budget!B2"`) {
		t.Errorf("changed %s", res.Changed)
	}
	// Round trip: read sees the workbook as the plan left it.
	if _, err := read.Cells(written, read.CellsRequest{Sheet: "sheet:Budget"}, read.Options{}); err != nil {
		t.Errorf("read of the output: %v", err)
	}
}

func TestWiringDryRunAndNoOp(t *testing.T) {
	in := wiringBook(t)
	before, _ := os.ReadFile(in)
	res, err := Run(in, ops.NewPlan(&styleOp{Sheet: "Budget", Row: 2, Col: 2, Style: 1}), Options{DryRun: true, gate: fullGate})
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != "" || res.NewBase != "" || !res.DryRun || len(res.Parts) != 1 {
		t.Errorf("dry run %+v", res)
	}
	out := filepath.Join(t.TempDir(), "noop.xlsx")
	res, err = Run(in, ops.NewPlan(&styleOp{Sheet: "Budget", Row: 1, Col: 2, Style: 0}), Options{Out: out, gate: fullGate})
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != "" || res.NewBase != res.Base || len(res.Parts) != 0 || len(res.Changed) != 0 {
		t.Errorf("no-op %+v", res)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("a no-op plan wrote a file")
	}
	if got, _ := os.ReadFile(in); !bytes.Equal(got, before) {
		t.Error("the input changed")
	}
}

func TestWiringRefusals(t *testing.T) {
	in := wiringBook(t)
	out := filepath.Join(t.TempDir(), "out.xlsx")
	for _, tc := range []struct {
		plan *ops.Plan
		code string
		path string
	}{
		{ops.NewPlan(&styleOp{Sheet: "Budget", Row: 1, Col: 1, Style: 1}, &styleOp{Fail: true}), cli.CodeOperationFailed, "ops[1].at"},
		{ops.NewPlan(&styleOp{Sheet: "Nope", Row: 1, Col: 1, Style: 1}), cli.CodeNotFound, "ops[0].at"},
		// An xf index past cellXfs: the full gate refuses what it
		// introduces, naming the op.
		{ops.NewPlan(&styleOp{Sheet: "Other", Row: 1, Col: 1, Style: 1}, &styleOp{Sheet: "Budget", Row: 1, Col: 1, Style: 9}), cli.CodeInvalidOutput, "ops[1]"},
		{&ops.Plan{Base: "sha256:" + strings.Repeat("0", 64), Ops: ops.NewPlan(&styleOp{Sheet: "Budget", Row: 1, Col: 1, Style: 1}).Ops}, cli.CodeStaleBase, "base"},
	} {
		_, err := Run(in, tc.plan, Options{Out: out, gate: fullGate})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != tc.code || e.Path != tc.path {
			t.Errorf("got %v, want %s at %s", err, tc.code, tc.path)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a refused plan wrote %s", out)
		}
	}
}
