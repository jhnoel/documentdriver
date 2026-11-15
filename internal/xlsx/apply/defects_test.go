package apply

import (
	"errors"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// Documents with defects (contract §10): a defect elsewhere never
// blocks; only the op's own target is unreadable_target (designer rows;
// frozen).

func TestDefectsElsewhereNeverBlock(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := xlsxtest.Book{Sheets: []xlsxtest.Sheet{
			{Name: "Good", Rows: `<row r="1"><c r="A1"><v>1</v></c></row>`},
			{Name: "Bad", Rows: `<row r="2"/><row r="1"/>`}, // rows out of order
		}}
		in, err := b.Write(t.TempDir(), "in.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Run(in, ops.NewPlan(&ops.CellSet{At: ops.AtCell("Good!B1"), Value: ops.Num(2)}), Options{DryRun: true}); err != nil {
			t.Errorf("an edit of a good sheet was blocked: %v", err)
		}
		_, err = Run(in, ops.NewPlan(&ops.CellSet{At: ops.AtCell("Bad!B1"), Value: ops.Num(2)}), Options{DryRun: true})
		var e *cli.Error
		if !errors.As(err, &e) || e.Code != cli.CodeUnreadableTarget || e.Path != "ops[0].at" || e.Detail == "" {
			t.Errorf("an edit of the bad sheet: %v", err)
		}
	})
}
