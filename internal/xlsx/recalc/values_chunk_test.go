package recalc

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

func TestValuesChunk(t *testing.T) {
	b := xlsxtest.Book{SST: []string{"<r><t>one</t></r><r><t>&amp;two</t></r>"}, Sheets: []xlsxtest.Sheet{
		{Name: "First", Rows: `<row r="2"><c r="A2" t="b"><f>TRUE()</f><v>1</v></c><c r="B2"><f>1</f></c><c r="C2"><v>7</v></c></row>`},
		{Name: "Second", Rows: `<row r="1"><c r="A1" t="s"><f>"x"</f><v>0</v></c><c r="B1" t="e"><f>1/0</f><v>#DIV/0!</v></c></row>`},
	}}
	data, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Values(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []Value{{Sheet: "First", Row: 2, Col: 1, T: "b", V: "1"}, {Sheet: "Second", Row: 1, Col: 1, T: "str", V: "one&two"}, {Sheet: "Second", Row: 1, Col: 2, T: "e", V: "#DIV/0!"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %+v want %+v", got[i], want[i])
		}
	}
	if _, err := Values(nil); err == nil {
		t.Error("invalid workbook accepted")
	}
}
