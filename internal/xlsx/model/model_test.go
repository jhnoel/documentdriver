package model

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// skipIfUnimplemented runs probe and skips t when it panics with a stub's
// "unimplemented: chunk N" message (a1-chunks.md).
func skipIfUnimplemented(t *testing.T, probe func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok && strings.HasPrefix(s, "unimplemented: ") {
				t.Skip(s)
			}
			panic(r)
		}
	}()
	probe()
}

func TestParseRef(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = ParseRef("A!A1") })
	for _, tc := range []struct {
		in   string
		want string // kind|sheet|range|name|column
	}{
		{"Budget!B2", "range|Budget|B2||"},
		{"Budget!A1:D10", "range|Budget|A1:D10||"},
		{"Budget!D10:A1", "range|Budget|A1:D10||"},
		{"Budget!B1:A2", "range|Budget|A1:B2||"},
		{"Budget!5:7", "range|Budget|5:7||"},
		{"Budget!7:5", "range|Budget|5:7||"},
		{"Budget!C:E", "range|Budget|C:E||"},
		{"Budget!c3", "range|Budget|C3||"},
		{"Budget!XFD1048576", "range|Budget|XFD1048576||"},
		{"'Q1 Budget'!A1", "range|Q1 Budget|A1||"},
		{"'Bob''s'!A1", "range|Bob's|A1||"},
		{"''''!A1", "range|'|A1||"},
		{"'Budget'!A1", "range|Budget|A1||"},
		{"'2024'!A1", "range|2024|A1||"},
		{"'A1'!A1", "range|A1|A1||"},
		{"'a!b'!A1", "range|a!b|A1||"},
		{"Ünïcode!A1", "range|Ünïcode|A1||"},
		{"sheet:Budget", "sheet|Budget|||"},
		{"sheet:Q1 Budget", "sheet|Q1 Budget|||"},
		{"name:Tax_Rate", "name|||Tax_Rate|"},
		{"name:Budget!Local", "name|Budget||Local|"},
		{"name:'Q1 Budget'!Local", "name|Q1 Budget||Local|"},
		{"table:Sales", "table|||Sales|"},
		{"table:Sales/column:Amount", "column|||Sales|Amount"},
		{"table:Sales/column:a/column:b", "column|||Sales|a/column:b"},
	} {
		r, err := ParseRef(tc.in)
		if err != nil {
			t.Errorf("ParseRef(%q): %v", tc.in, err)
			continue
		}
		if got := describe(r); got != tc.want {
			t.Errorf("ParseRef(%q) = %s, want %s", tc.in, got, tc.want)
		}
		if r.Kind == RefRange {
			if r.Range.From.RowAbs || r.Range.From.ColAbs || r.Range.To.RowAbs || r.Range.To.ColAbs {
				t.Errorf("%q kept $ flags", tc.in)
			}
			// Round trip: what read prints, ParseRef reads back.
			back, err := ParseRef(Qualified(r.Sheet, r.Range))
			if err != nil || describe(back) != tc.want {
				t.Errorf("round trip of %q via %q: %v %v", tc.in, Qualified(r.Sheet, r.Range), describe(back), err)
			}
		}
	}
}

func describe(r Ref) string {
	kind := map[RefKind]string{RefSheet: "sheet", RefRange: "range", RefName: "name", RefTable: "table", RefTableColumn: "column"}[r.Kind]
	rng := ""
	if r.Kind == RefRange {
		rng = r.Range.String()
	}
	return kind + "|" + r.Sheet + "|" + rng + "|" + r.Name + "|" + r.Column
}

func TestParseRefRefusals(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = ParseRef("A!A1") })
	for _, in := range []string{
		"", "A1", "A1:B2", "5:7", "!A1", "Budget!", "Budget!$A$1", "Budget!A$1", "Budget!$A1:B2", "Budget!$5:$7",
		"Budget!R1C1", "Budget!R[1]C", "Budget!RC", "Budget!A1:C", "Budget!A:1", "Budget!XFE1", "Budget!A0", "Budget!A1048577",
		"Q1 Budget!A1", "'Q1 Budget!A1", "Q1 Budget'!A1", "'Bob's'!A1", "''!A1", "[1]Budget!A1", "'[1]Budget'!A1x",
		"Jan:Mar!A1", "'Jan:Mar'!A1:", " Budget!A1", "Budget!A1 ", "Budget!A1,Budget!B1", "Budget!A1 B1",
		"sheet:", "name:", "name:Budget!", "name:!x", "name:a!b!c", "table:", "table:/column:x", "table:T/column:",
		"cell:A1", "Budget!#REF!",
	} {
		_, err := ParseRef(in)
		var ae *AddressError
		if !errors.As(err, &ae) || ae.Input != in || ae.Reason == "" {
			t.Errorf("ParseRef(%q) = %v, want an *AddressError", in, err)
		}
	}
	// $ gets a reason that says what to write.
	_, err := ParseRef("Budget!$A$1")
	if err == nil || !strings.Contains(err.Error(), "$") {
		t.Errorf("$ refusal: %v", err)
	}
}

func TestParseSheet(t *testing.T) {
	skipIfUnimplemented(t, func() { _, _ = ParseSheet("sheet:x") })
	for in, want := range map[string]string{"sheet:Budget": "Budget", "sheet:Q1 Budget": "Q1 Budget", "sheet:Bob's": "Bob's", "sheet:A1": "A1"} {
		if got, err := ParseSheet(in); err != nil || got != want {
			t.Errorf("ParseSheet(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"Budget", "sheet:", "", "Sheet:Budget", "Budget!A1", "name:x"} {
		if _, err := ParseSheet(in); err == nil {
			t.Errorf("ParseSheet(%q) accepted", in)
		}
	}
	_, err := ParseSheet("Budget")
	if err == nil || !strings.Contains(err.Error(), "sheet:Budget") {
		t.Errorf("the refusal shows the form: %v", err)
	}
}

func TestFormatAddresses(t *testing.T) {
	skipIfUnimplemented(t, func() { _ = Cell("a", 1, 1) })
	for _, tc := range []struct{ got, want string }{
		{SheetID("Q1 Budget"), "sheet:Q1 Budget"},
		{CellKey(2, 2), "B2"},
		{CellKey(formula.MaxRows, formula.MaxCols), "XFD1048576"},
		{string(AppendCellKey([]byte("x"), 10, 27)), "xAA10"},
		{Cell("Budget", 1, 1), "Budget!A1"},
		{Cell("Q1 Budget", 1, 1), "'Q1 Budget'!A1"},
		{Cell("Bob's", 3, 2), "'Bob''s'!B3"},
		{Cell("2024", 1, 1), "'2024'!A1"},
		{Cell("A1", 1, 1), "'A1'!A1"},
		{Cell("R1C1", 1, 1), "'R1C1'!A1"},
		{Area("Budget", 1, 1, 10, 4), "Budget!A1:D10"},
		{Area("Budget", 2, 2, 2, 2), "Budget!B2"},
		{Rows("Budget", 5, 7), "Budget!5:7"},
		{Rows("Budget", 5, 5), "Budget!5:5"},
		{Cols("Budget", 3, 5), "Budget!C:E"},
		{Cols("Q1 Budget", 3, 3), "'Q1 Budget'!C:C"},
		{Qualified("Budget", formula.Range{Kind: formula.Cells, From: formula.Cell{Row: 1, Col: 1, RowAbs: true, ColAbs: true}, To: formula.Cell{Row: 2, Col: 2}}), "Budget!A1:B2"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestAppendNumber(t *testing.T) {
	for _, f := range []float64{0, 1, -1, 1200, 0.1, 0.5, 1e-7, 1e-6, 123456789012345678, 1e21, 1e20, -2.5e-9, math.MaxFloat64, math.SmallestNonzeroFloat64, 45352.5625, 0.1 + 0.2} {
		got := FormatNumber(f)
		want, err := json.Marshal(f)
		if err != nil || got != string(want) {
			t.Errorf("FormatNumber(%v) = %s, encoding/json %s", f, got, want)
		}
		back, ok := ParseNumber(got)
		if !ok || back != f {
			t.Errorf("ParseNumber(FormatNumber(%v)) = %v %v", f, back, ok)
		}
	}
	if FormatNumber(math.NaN()) != "null" || FormatNumber(math.Inf(1)) != "null" {
		t.Error("non-finite")
	}
}

func TestParseNumber(t *testing.T) {
	for in, want := range map[string]float64{"1200": 1200, "0.1": 0.1, "1E-3": 0.001, "-0": math.Copysign(0, -1), "1e+21": 1e21, ".5": 0.5, "5.": 5} {
		if got, ok := ParseNumber(in); !ok || got != want {
			t.Errorf("ParseNumber(%q) = %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "NaN", "nan", "Inf", "-Infinity", "1e400", "0x10", "1_000", " 1", "1 ", "abc", "1,5", "+"} {
		if got, ok := ParseNumber(in); ok {
			t.Errorf("ParseNumber(%q) = %v, accepted", in, got)
		}
	}
}

func TestValueJSON(t *testing.T) {
	skipIfUnimplemented(t, func() { _ = Value{}.AppendJSON(nil) })
	for _, tc := range []struct {
		v     Value
		json  string
		match string
	}{
		{Value{Kind: Number, Num: 1200}, `1200`, "1200"},
		{Value{Kind: Number, Num: 1e-7}, `1e-7`, "1e-7"},
		{Value{Kind: String, Text: "Item"}, `"Item"`, "Item"},
		{Value{Kind: String, Text: "=1+1"}, `{"text":"=1+1"}`, "=1+1"},
		{Value{Kind: String, Text: "a\"b\\c\n<&>\x01"}, `"a\"b\\c\n<&>\u0001"`, "a\"b\\c\n<&>\x01"},
		{Value{Kind: String, Text: "bad\xffutf8"}, `"bad` + "�" + `utf8"`, "bad\xffutf8"},
		{Value{Kind: String, Text: ""}, `""`, ""},
		{Value{Kind: Bool, Bool: true}, `true`, "true"},
		{Value{Kind: Bool}, `false`, "false"},
		{Value{Kind: Error, Text: "#N/A"}, `{"error":"#N/A"}`, "#N/A"},
		{Value{Kind: Date, Text: "2024-03-01"}, `{"date":"2024-03-01"}`, "2024-03-01"},
		{Value{Kind: Date, Text: "2024-03-01T13:30:00"}, `{"date":"2024-03-01T13:30:00"}`, "2024-03-01T13:30:00"},
		{Value{Kind: Time, Text: "13:30:00"}, `{"time":"13:30:00"}`, "13:30:00"},
		{Value{}, ``, ""},
	} {
		if got := string(tc.v.AppendJSON([]byte{})); got != tc.json {
			t.Errorf("%+v: JSON %s, want %s", tc.v, got, tc.json)
		}
		if tc.json != "" && !json.Valid([]byte(tc.json)) {
			t.Errorf("%s is not JSON", tc.json)
		}
		if got := tc.v.MatchText(); got != tc.match {
			t.Errorf("%+v: MatchText %q, want %q", tc.v, got, tc.match)
		}
	}
}

func TestEntryJSON(t *testing.T) {
	skipIfUnimplemented(t, func() { _ = Entry{}.AppendJSON(nil, ViewCells) })
	num := func(f float64) Value { return Value{Kind: Number, Num: f} }
	for _, tc := range []struct {
		name                    string
		e                       Entry
		cells, values, formulas string // "" = not shown
	}{
		{"plain", Entry{Value: num(1)}, `1`, `1`, ``},
		{"text", Entry{Value: Value{Kind: String, Text: "x"}}, `"x"`, `"x"`, ``},
		{"formula", Entry{Value: num(2400), Formula: "=B2+C2"}, `{"f":"=B2+C2","v":2400}`, `2400`, `"=B2+C2"`},
		{"formula no cache", Entry{Formula: "=B2+C2"}, `{"f":"=B2+C2"}`, ``, `"=B2+C2"`},
		{"formula text value", Entry{Value: Value{Kind: String, Text: "=x"}, Formula: "=A1"}, `{"f":"=A1","v":{"text":"=x"}}`, `{"text":"=x"}`, `"=A1"`},
		{"dynamic anchor", Entry{Value: num(1), Formula: "=SEQUENCE(3)", Array: Dynamic}, `{"f":"=SEQUENCE(3)","v":1}`, `1`, `"=SEQUENCE(3)"`},
		{"dynamic member", Entry{Value: num(2), Spill: "Budget!A1", Array: Dynamic}, `{"v":2,"spill":"Budget!A1"}`, `2`, `{"spill":"Budget!A1"}`},
		{"member no value", Entry{Spill: "Budget!A1", Array: Dynamic}, `{"spill":"Budget!A1"}`, ``, `{"spill":"Budget!A1"}`},
		{"cse anchor", Entry{Value: num(2), Formula: "=A1:B2*2", Array: CSE}, `{"f":"=A1:B2*2","v":2,"array":"cse"}`, `2`, `{"f":"=A1:B2*2","array":"cse"}`},
		{"cse member", Entry{Value: num(4), Spill: "'Q 1'!C1", Array: CSE}, `{"v":4,"spill":"'Q 1'!C1","array":"cse"}`, `4`, `{"spill":"'Q 1'!C1","array":"cse"}`},
		{"data table anchor", Entry{Value: num(5), Array: DataTable}, `{"v":5,"array":"data_table"}`, `5`, `{"array":"data_table"}`},
		{"data table anchor f", Entry{Value: num(5), Formula: "=TABLE(A1,B1)", Array: DataTable}, `{"f":"=TABLE(A1,B1)","v":5,"array":"data_table"}`, `5`, `{"f":"=TABLE(A1,B1)","array":"data_table"}`},
		{"data table member", Entry{Value: num(6), Spill: "S!H2", Array: DataTable}, `{"v":6,"spill":"S!H2","array":"data_table"}`, `6`, `{"spill":"S!H2","array":"data_table"}`},
		{"unknown member", Entry{Value: num(2), Spill: "S!K1", Array: UnknownArray}, `{"v":2,"spill":"S!K1"}`, `2`, `{"spill":"S!K1"}`},
		{"anchor formula lost", Entry{Value: num(1), Array: CSE}, `{"v":1,"array":"cse"}`, `1`, `{"array":"cse"}`},
		{"anchor formula lost dynamic", Entry{Value: num(1), Array: Dynamic}, `1`, `1`, ``},
		{"empty", Entry{}, ``, ``, ``},
	} {
		for view, want := range map[View]string{ViewCells: tc.cells, ViewValues: tc.values, ViewFormulas: tc.formulas} {
			got := string(tc.e.AppendJSON(nil, view))
			if got != want {
				t.Errorf("%s, %s: %s, want %s", tc.name, view, got, want)
			}
			if tc.e.Shown(view) != (want != "") {
				t.Errorf("%s, %s: Shown %v", tc.name, view, tc.e.Shown(view))
			}
		}
	}
}

func TestNotes(t *testing.T) {
	skipIfUnimplemented(t, func() { _ = Cell("a", 1, 1) })
	var n Notes
	if l := n.List(); l == nil || len(l) != 0 {
		t.Fatalf("empty list %v", l)
	}
	n.Add(NotePart, "/xl/styles.xml", "styles cannot be read")
	n.Add(NotePart, "/xl/styles.xml", "styles cannot be read")
	for r := 1; r <= 1000; r++ {
		n.AddCell(NoteDuration, "Budget", r, 2, "duration format [h]:mm shown as a number")
	}
	n.AddCell(NoteDuration, "Other", 3, 3, "duration format [h]:mm shown as a number")
	l := n.List()
	if len(l) != 3 || l[0].At != "/xl/styles.xml" || l[1].At != "Budget!B1" || !strings.Contains(l[1].Reason, "1000") || l[2].At != "Other!C3" || strings.Contains(l[2].Reason, "cells in all") {
		t.Errorf("notes %+v", l)
	}
	var many Notes
	for i := 0; i < MaxNotes+5; i++ {
		many.Add(NotePart, "p"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+FormatNumber(float64(i)), "r")
	}
	l = many.List()
	if len(l) != MaxNotes+1 || l[MaxNotes].Kind != NoteMore || !strings.Contains(l[MaxNotes].Reason, "5") {
		t.Errorf("past MaxNotes: %d notes, last %+v", len(l), l[len(l)-1])
	}
	b, err := json.Marshal(n.List()[0])
	if err != nil || string(b) != `{"at":"/xl/styles.xml","reason":"styles cannot be read"}` {
		t.Errorf("note JSON %s %v (Kind must not be printed)", b, err)
	}
}
