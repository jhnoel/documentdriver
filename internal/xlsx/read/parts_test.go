package read

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/numfmt"
)

func parse(t *testing.T, s string) *tree.Document {
	t.Helper()
	d, err := tree.ParseString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseStyles(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = parseStyles(parse(t, `<styleSheet xmlns="`+nsMainT+`"/>`)) })
	f := valueStyles(fx{})
	f.numFmts[14] = "yyyy-mm-dd" // a custom format overriding a built-in id
	f.xfs = append(f.xfs, 14, 9999, 0)
	s, err := parseStyles(parse(t, f.styles()))
	if err != nil {
		t.Fatal(err)
	}
	for xf, want := range map[int]numfmt.Kind{
		xfGeneral: numfmt.General, xfDate: numfmt.Date, xfDateTime: numfmt.DateTime, xfTime: numfmt.Time,
		xfDuration: numfmt.Duration, xfCustomDT: numfmt.DateTime, xfPercent: numfmt.Number, xfText: numfmt.Text,
		8: numfmt.Date, 9: numfmt.General, 10: numfmt.General,
	} {
		if k, _, ok := s.Kind(xf); !ok || k != want {
			t.Errorf("xf %d: %v %v, want %v", xf, k, ok, want)
		}
	}
	if _, code, _ := s.Kind(xfDuration); code != "[h]:mm" {
		t.Errorf("duration code %q", code)
	}
	if _, _, ok := s.Kind(11); ok {
		t.Error("xf 11 is out of range")
	}
	var none *Styles
	if k, _, ok := none.Kind(0); ok || k != numfmt.General {
		t.Error("nil Styles")
	}
	if _, err := parseStyles(parse(t, `<other/>`)); err == nil {
		t.Error("wrong root accepted")
	}
}

func TestParseMetadata(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = parseMetadata(parse(t, dynamicMetadata)) })
	m, err := parseMetadata(parse(t, dynamicMetadata))
	if err != nil {
		t.Fatal(err)
	}
	for cm, want := range map[int]model.ArrayKind{0: model.CSE, 1: model.Dynamic, 2: model.CSE, 3: model.UnknownArray, -1: model.UnknownArray} {
		if got := m.ArrayKind(cm); got != want {
			t.Errorf("cm %d: %v, want %v", cm, got, want)
		}
	}
	var none *Metadata
	if none.ArrayKind(0) != model.CSE || none.ArrayKind(1) != model.UnknownArray {
		t.Error("nil Metadata")
	}
	broken := `<metadata xmlns="` + nsMainT + `"><metadataTypes><metadataType name="XLDAPR"/></metadataTypes><cellMetadata><bk><rc t="1" v="5"/></bk><bk><rc t="7" v="0"/></bk></cellMetadata></metadata>`
	m, err = parseMetadata(parse(t, broken))
	if err != nil {
		t.Fatal(err)
	}
	if m.ArrayKind(1) != model.UnknownArray || m.ArrayKind(2) != model.UnknownArray {
		t.Error("a broken chain must be unknown, not guessed")
	}
}

func TestParseTable(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = parseTable(parse(t, table("T", "A1:B2", "x", "y")), "S") })
	tb, err := parseTable(parse(t, table("Sales", "B2:D10", "Region", "Q1", "Q 2")), "S")
	if err != nil {
		t.Fatal(err)
	}
	if tb.Name != "Sales" || tb.Sheet != "S" || tb.Ref.String() != "B2:D10" || tb.HeaderRows != 1 || tb.TotalsRows != 0 || len(tb.Columns) != 3 {
		t.Errorf("table %+v", tb)
	}
	for col, want := range map[string]string{"Region": "B3:B10", "q 2": "D3:D10"} {
		if r, found, has := tb.ColumnRange(col); !found || !has || r.String() != want {
			t.Errorf("column %s: %v %v %v", col, r, found, has)
		}
	}
	if _, found, _ := tb.ColumnRange("Nope"); found {
		t.Error("found Nope")
	}
	noHdr, err := parseTable(parse(t, `<table xmlns="`+nsMainT+`" name="n" displayName="Disp" ref="A1:A1" headerRowCount="1"><tableColumns><tableColumn name="c"/></tableColumns></table>`), "S")
	if err != nil {
		t.Fatal(err)
	}
	if noHdr.Name != "Disp" {
		t.Errorf("displayName: %q", noHdr.Name)
	}
	if _, found, has := noHdr.ColumnRange("c"); !found || has {
		t.Error("a header-only table has no data rows")
	}
	if _, err := parseTable(parse(t, `<table xmlns="`+nsMainT+`" name="n" ref="bad"/>`), "S"); err == nil {
		t.Error("bad ref accepted")
	}
}

func TestNameRange(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _, _ = nameRange("") })
	for text, want := range map[string]string{
		"Budget!$B$2":            "Budget|B2",
		"Budget!$B$2:$C$3":       "Budget|B2:C3",
		" Budget!B2 ":            "Budget|B2",
		"'Q1 Budget'!$A:$A":      "Q1 Budget|A:A",
		"Budget!$2:$3":           "Budget|2:3",
		"'Bob''s'!A1":            "Bob's|A1",
		"0.5":                    "",
		"Budget!$A$1,Budget!B":   "",
		"Budget!#REF!":           "",
		"[1]Budget!A1":           "",
		"Jan:Mar!A1":             "",
		"A1":                     "",
		"SUM(Budget!A1)":         "",
		"OFFSET(Budget!A1,1,1)":  "",
		"Budget!A1 Budget!A1:B2": "",
	} {
		sheet, r, ok := nameRange(text)
		got := ""
		if ok {
			got = sheet + "|" + r.String()
		}
		if got != want {
			t.Errorf("nameRange(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestISODate(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = isoDate("", numfmt.General) })
	for _, tc := range []struct {
		text string
		kind numfmt.Kind
		want string // AppendJSON, "" for !ok
	}{
		{"2024-03-01T00:00:00", numfmt.General, `{"date":"2024-03-01"}`},
		{"2024-03-01", numfmt.General, `{"date":"2024-03-01"}`},
		{"2024-03-01T13:30:00", numfmt.General, `{"date":"2024-03-01T13:30:00"}`},
		{"2024-03-01T13:30:00Z", numfmt.Date, `{"date":"2024-03-01"}`},
		{"2024-03-01T13:30:00.600", numfmt.DateTime, `{"date":"2024-03-01T13:30:01"}`},
		{"2024-03-01T23:59:59.6", numfmt.DateTime, `{"date":"2024-03-02T00:00:00"}`},
		{"2024-03-01", numfmt.DateTime, `{"date":"2024-03-01T00:00:00"}`},
		{"1899-12-31T13:30:00", numfmt.Time, `{"time":"13:30:00"}`},
		{"13:30:00", numfmt.Time, `{"time":"13:30:00"}`},
		{"T13:30:00", numfmt.General, `{"time":"13:30:00"}`},
		{"garbage", numfmt.General, ""},
		{"2024-02-30", numfmt.General, ""},
		{"2024-03-01T25:00:00", numfmt.General, ""},
	} {
		v, ok := isoDate(tc.text, tc.kind)
		got := ""
		if ok {
			got = string(v.AppendJSON(nil))
		}
		if got != tc.want {
			t.Errorf("isoDate(%q, %v) = %s, want %s", tc.text, tc.kind, got, tc.want)
		}
	}
}
