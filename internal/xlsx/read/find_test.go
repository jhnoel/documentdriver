package read

import (
	"reflect"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
)

func probeFind() { _, _ = Find(nil, FindRequest{}, Options{}) }

// findBook has two sheets of values and formulas, one hidden.
func findBook() fx {
	f := valuesBook()
	f.sheets = append(f.sheets,
		fxSheet{name: "Calc", rows: formulasBook().sheets[0].rows},
		fxSheet{name: "Hidden", state: "hidden", rows: `<row r="1"><c r="A1" t="inlineStr"><is><t>ΣΑΣ Item</t></is></c><c r="B1" t="s"><v>0</v></c></row>`},
	)
	return f
}

// matches returns the matches as "cell=value" strings.
func matches(t *testing.T, res *FindResult) []string {
	t.Helper()
	out := make([]string, len(res.Matches))
	for i, m := range res.Matches {
		out[i] = m.Cell + "=" + string(m.Value)
	}
	return out
}

func TestFind(t *testing.T) {
	skipIfUnimplemented(t, probeFind)
	input := findBook().zip(t)
	for _, tc := range []struct {
		name string
		req  FindRequest
		want []string
	}{
		{"shared string", FindRequest{Query: "Item"}, []string{`Values!A1="Item"`, `Hidden!A1="ΣΑΣ Item"`, `Hidden!B1="Item"`}},
		{"rich text and inline", FindRequest{Query: "alue"}, []string{`Values!B1="Value "`}},
		{"case", FindRequest{Query: "item"}, nil},
		{"ignore case", FindRequest{Query: "iTEM", IgnoreCase: true}, []string{`Values!A1="Item"`, `Hidden!A1="ΣΑΣ Item"`, `Hidden!B1="Item"`}},
		{"simple folding", FindRequest{Query: "σας", IgnoreCase: true}, []string{`Hidden!A1="ΣΑΣ Item"`}},
		{"number text", FindRequest{Query: "120"}, []string{`Values!A2=1200`}},
		{"exponent", FindRequest{Query: "1e-7"}, []string{`Values!C2=1e-7`}},
		{"date text", FindRequest{Query: "2024-03-01T13"}, []string{`Values!B4={"date":"2024-03-01T13:30:00"}`, `Values!B5={"date":"2024-03-01T13:30:00"}`}},
		{"time", FindRequest{Query: "13:30"}, []string{`Values!B4={"date":"2024-03-01T13:30:00"}`, `Values!C4={"time":"13:30:00"}`, `Values!B5={"date":"2024-03-01T13:30:00"}`, `Values!C5={"time":"13:30:00"}`}},
		{"bool", FindRequest{Query: "true"}, []string{`Values!A3=true`, `Calc!B4=true`}},
		{"error", FindRequest{Query: "#DIV"}, []string{`Values!D3={"error":"#DIV/0!"}`, `Calc!D1={"error":"#DIV/0!"}`}},
		{"text form", FindRequest{Query: "=not"}, []string{`Values!C1={"text":"=not formula"}`}},
		{"one sheet", FindRequest{Query: "Item", Sheet: "sheet:hidden"}, []string{`Hidden!A1="ΣΑΣ Item"`, `Hidden!B1="Item"`}},
		{"limit", FindRequest{Query: "Item", Limit: 2}, []string{`Values!A1="Item"`, `Hidden!A1="ΣΑΣ Item"`}},
		{"formulas", FindRequest{Query: "B3", View: model.ViewFormulas}, []string{`Calc!A3="=B3*2"`}},
		{"formula display form", FindRequest{Query: "E5#", View: model.ViewFormulas}, []string{`Calc!D2="=SUM(E5#)"`}},
		{"formula equals sign", FindRequest{Query: "=@", View: model.ViewFormulas}, []string{`Calc!C2="=@A1:A3"`}},
		{"stored prefix not matched", FindRequest{Query: "_xlfn", View: model.ViewFormulas}, nil},
		{"formula ignore case", FindRequest{Query: "xlookup", View: model.ViewFormulas, IgnoreCase: true}, []string{`Calc!B1="=XLOOKUP(1,A:A,B:B)"`}},
	} {
		if tc.req.View == 0 {
			tc.req.View = model.ViewValues
		}
		if tc.req.Limit == 0 {
			tc.req.Limit = DefaultLimit
		}
		res, err := Find(input, tc.req, Options{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := matches(t, res); !reflect.DeepEqual(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
			t.Errorf("%s:\n got %v\nwant %v", tc.name, got, tc.want)
		}
		cut := res.Truncated != nil
		if cut != (tc.name == "limit") {
			t.Errorf("%s: truncated %v", tc.name, res.Truncated)
		}
		if cut && res.Truncated.After != "Hidden!A1" {
			t.Errorf("%s: after %q", tc.name, res.Truncated.After)
		}
		if res.Matches == nil {
			t.Errorf("%s: matches is nil (must encode as [])", tc.name)
		}
	}
}

// TestFindEmptyQuery: the empty query matches every cell with a value.
func TestFindEmptyQuery(t *testing.T) {
	skipIfUnimplemented(t, probeFind)
	res, err := Find(bigBook(3, 2).zip(t), FindRequest{View: model.ViewValues, Limit: DefaultLimit}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 6 {
		t.Errorf("%d matches", len(res.Matches))
	}
}

// TestFindExactLimit: exactly --limit matches is not a cut; one more is.
func TestFindExactLimit(t *testing.T) {
	skipIfUnimplemented(t, probeFind)
	input := bigBook(3, 2).zip(t)
	res, err := Find(input, FindRequest{View: model.ViewValues, Limit: 6}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 6 || res.Truncated != nil {
		t.Errorf("limit 6 of 6: %d matches, truncated %v", len(res.Matches), res.Truncated)
	}
	res, err = Find(input, FindRequest{View: model.ViewValues, Limit: 5}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 5 || res.Truncated == nil || res.Truncated.After != res.Matches[4].Cell {
		t.Errorf("limit 5 of 6: %d matches, truncated %v", len(res.Matches), res.Truncated)
	}
}

// TestFindSkipsSST: a search that meets no t="s" cell never reads the
// shared strings (survey R12): a broken sst gives no note.
func TestFindSkipsSST(t *testing.T) {
	skipIfUnimplemented(t, probeFind)
	f := bigBook(3, 2)
	f.sstRaw = `<sst`
	res, err := Find(f.zip(t), FindRequest{Query: "1", View: model.ViewValues, Limit: DefaultLimit}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes %+v", res.Notes)
	}
	res, err = Find(f.zip(t), FindRequest{Query: "1", View: model.ViewFormulas, Limit: DefaultLimit}, Options{})
	if err != nil || len(res.Notes) != 0 {
		t.Errorf("formulas view: %v %+v", err, res.Notes)
	}
}

// TestFindSheets: unreadable and non-worksheet sheets without --sheet are
// skipped (a note for the unreadable one); with --sheet they are the
// cell read's errors.
func TestFindSheets(t *testing.T) {
	skipIfUnimplemented(t, probeFind)
	input := addressBook().zip(t)
	res, err := Find(input, FindRequest{Query: "Total", View: model.ViewValues, Limit: DefaultLimit}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := matches(t, res); !reflect.DeepEqual(got, []string{`Budget!A4="Total"`}) {
		t.Errorf("matches %v", got)
	}
	if n := noteKinds(res.Notes)[model.NotePart]; n != 2 {
		t.Errorf("notes for Gone and Broken: %+v", res.Notes)
	}
	for sheet, code := range map[string]string{
		"sheet:Gone": cli.CodeUnreadableTarget, "sheet:Broken": cli.CodeUnreadableTarget,
		"sheet:Nope": cli.CodeNotFound, "Budget": cli.CodeInvalidOperation,
	} {
		_, err := Find(input, FindRequest{Query: "x", Sheet: sheet, View: model.ViewValues, Limit: DefaultLimit}, Options{})
		if c, p := errCode(err); c != code || p != "--sheet" {
			t.Errorf("--sheet %s: %q at %q, want %q", sheet, c, p, code)
		}
	}
	res, err = Find(input, FindRequest{Query: "x", Sheet: "sheet:Chart1", View: model.ViewValues, Limit: DefaultLimit}, Options{})
	if err != nil || len(res.Matches) != 0 || noteKinds(res.Notes)[model.NoteSheetKind] != 1 {
		t.Errorf("chartsheet: %v %+v", err, res)
	}
}
