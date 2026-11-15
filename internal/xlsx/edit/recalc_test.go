package edit

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// The recalculation policy (designer-owned; these pass).

func recalcBook(calcPr, chain string) xlsxtest.Book {
	b := oneSheet(`<row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>A1*2</f><v>4</v></c></row>`)
	b.CalcPr, b.CalcChain = calcPr, chain
	return b
}

func finished(t *testing.T, b xlsxtest.Book, edit func(*Sheet) error) *Workspace {
	t.Helper()
	w := openBook(t, b)
	s, err := w.Sheet("S")
	if err != nil {
		t.Fatal(err)
	}
	if err := edit(s); err != nil {
		t.Fatal(err)
	}
	if err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	return w
}

func workbookText(t *testing.T, w *Workspace) string {
	t.Helper()
	doc, err := w.WorkbookTree()
	if err != nil {
		t.Fatal(err)
	}
	return string(doc.Bytes())
}

func TestRecalcPolicy(t *testing.T) {
	xlsxtest.Skip(t, func() {
		// A value edit: fullCalcOnLoad, the calcChain kept.
		w := finished(t, recalcBook(`<calcPr calcId="191029"/>`, `<c r="B1" i="1"/>`), func(s *Sheet) error {
			return s.SetCell(1, 1, `<c r="A1"><v>7</v></c>`)
		})
		if wb := workbookText(t, w); !strings.Contains(wb, `<calcPr calcId="191029" fullCalcOnLoad="1"/>`) {
			t.Errorf("calcPr: %s", wb)
		}
		if !w.Package().Has("/xl/calcChain.xml") {
			t.Error("a value edit removed the calcChain")
		}
		// A formula edit: the calcChain, its relationship and override go;
		// a missing calcPr is made in CT_Workbook order.
		w = finished(t, recalcBook("", `<c r="B1" i="1"/>`), func(s *Sheet) error {
			return s.SetCell(1, 3, `<c r="C1"><f>A1+1</f></c>`)
		})
		if w.Package().Has("/xl/calcChain.xml") {
			t.Error("the calcChain survived a formula edit")
		}
		rels, err := w.Package().Rels(w.WorkbookPart())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rels.All() {
			if strings.HasSuffix(r.Type, "/calcChain") {
				t.Error("the calcChain relationship survived")
			}
		}
		ct, _ := w.Package().ContentTypes()
		if _, ok := ct.Override("/xl/calcChain.xml"); ok {
			t.Error("the calcChain override survived")
		}
		if wb := workbookText(t, w); !strings.Contains(wb, `<calcPr fullCalcOnLoad="1"/></workbook>`) {
			t.Errorf("calcPr not made: %s", wb)
		}
	})
}

func TestRecalcPolicyNoOp(t *testing.T) {
	xlsxtest.Skip(t, func() {
		// A style-only edit marks nothing: the workbook is untouched.
		w := finished(t, recalcBook(`<calcPr calcId="191029"/>`, `<c r="B1" i="1"/>`), func(s *Sheet) error {
			return s.SetCellStyle(1, 1, 0)
		})
		if ch, _ := w.Package().ChangedParts(); len(ch) != 0 {
			t.Errorf("changed %v", ch)
		}
		// fullCalcOnLoad already set: nothing written to the workbook.
		w = finished(t, recalcBook(`<calcPr fullCalcOnLoad="1"/>`, ""), func(s *Sheet) error {
			return s.SetCell(1, 1, `<c r="A1"><v>7</v></c>`)
		})
		if ch, _ := w.Package().ChangedParts(); len(ch) != 1 {
			t.Errorf("changed %v, want the sheet only", ch)
		}
	})
}
