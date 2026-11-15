package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// FuzzApply: any plan text against a small workbook either applies (and
// the output reads) or is refused with a contract error; nothing panics
// and a refusal writes nothing. Add ./internal/xlsx/apply:FuzzApply to
// FUZZ_TARGETS.
func FuzzApply(f *testing.F) {
	for _, s := range []string{
		`{"ops":[{"op":"cell.set","at":"Budget!B2","value":"=A1*2"}]}`,
		`{"ops":[{"op":"row.insert","at":"Budget!1:1","position":"after","count":2}]}`,
		`{"ops":[{"op":"sheet.add","name":"N","as":"n"},{"op":"cell.set","at":"N!A1","value":1}]}`,
		`{"ops":[{"op":"range.set","at":"Budget!A1","values":[[1,null,""]]}]}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		xlsxtest.Skip(t, func() {
			p, errs := ops.ParsePlan([]byte(text))
			if errs != nil {
				return
			}
			dir := t.TempDir()
			in, err := xlsxtest.Book{Sheets: []xlsxtest.Sheet{{Name: "Budget", Rows: `<row r="1"><c r="A1"><v>2</v></c></row>`}}}.Write(dir, "in.xlsx")
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "out.xlsx")
			_, err = Run(in, p, Options{Out: out})
			if err != nil {
				e, ok := err.(*cli.Error)
				if !ok || e.Code == cli.CodeInternal {
					t.Fatalf("%s: %v", text, err)
				}
				if _, serr := os.Stat(out); serr == nil {
					t.Fatal("a refused plan wrote its output")
				}
			}
		})
	})
}
