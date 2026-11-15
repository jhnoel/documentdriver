package recalc

import (
	"errors"
	"os"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K10: Values (pure), and G17: Compute (LibreOffice, only when
// DD_SOFFICE names the binary:
// DD_SOFFICE=/Applications/LibreOffice.app/Contents/MacOS/soffice)
// (designer rows; frozen).

func TestValues(t *testing.T) {
	xlsxtest.Skip(t, func() {
		b := xlsxtest.Book{SST: []string{"<t>s</t>"}, Sheets: []xlsxtest.Sheet{{Name: "A",
			Rows: `<row r="1"><c r="A1"><v>2</v></c><c r="B1"><f>A1*2</f><v>4</v></c><c r="C1" t="s"><f>"s"</f><v>0</v></c></row>`}}}
		data, err := b.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		vs, err := Values(data)
		if err != nil {
			t.Fatal(err)
		}
		want := []Value{{Sheet: "A", Row: 1, Col: 2, V: "4"}, {Sheet: "A", Row: 1, Col: 3, T: "str", V: "s"}}
		if len(vs) != 2 || vs[0] != want[0] || vs[1] != want[1] {
			t.Errorf("Values = %+v", vs)
		}
	})
}

func TestCompute(t *testing.T) {
	bin := os.Getenv("DD_SOFFICE")
	if bin == "" {
		t.Skip("DD_SOFFICE is not set")
	}
	xlsxtest.Skip(t, func() {
		// E9: a stale cached value is recomputed (OOXMLRecalcMode=0).
		b := xlsxtest.Book{CalcPr: `<calcPr fullCalcOnLoad="1"/>`, Sheets: []xlsxtest.Sheet{{Name: "A",
			Rows: `<row r="1"><c r="A1"><v>7</v></c></row><row r="2"><c r="A2"><v>3</v></c></row><row r="3"><c r="A3"><f>A1+A2</f><v>5</v></c></row>`}}}
		data, err := b.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		vs, err := Compute(data, Options{Soffice: bin})
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 1 || vs[0].V != "10" {
			t.Errorf("Compute = %+v, want A3 10", vs)
		}
	})
}

func TestComputeUnavailable(t *testing.T) {
	xlsxtest.Skip(t, func() {
		_, err := Compute(nil, Options{Soffice: "/nonexistent/soffice"})
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("missing binary: %v", err)
		}
	})
}
