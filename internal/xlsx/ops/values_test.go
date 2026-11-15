package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// K1: value decoding (needs K3 for dates and K4 for formulas)
// (designer rows; frozen).

func TestDecodeValue(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct {
			json string
			want ops.CellValue
		}{
			{`1200`, ops.Num(1200)},
			{`-3e-7`, ops.Num(-3e-7)},
			{`"Item"`, ops.Str("Item")},
			{`""`, ops.Str("")}, // owner decision 2026-11-14: "" writes an empty string
			{`{"text":"=literal"}`, ops.Str("=literal")},
			{`true`, ops.Boolean(true)},
			{`{"error":"#N/A"}`, ops.Err("#N/A")},
			{`{"date":"2024-03-01"}`, ops.DateOf("2024-03-01")},
			{`{"date":"2024-03-01T13:30:00"}`, ops.DateOf("2024-03-01T13:30:00")},
			{`{"time":"13:30:00"}`, ops.TimeOf("13:30:00")},
			{`"=B2+C2"`, ops.Fx("B2+C2")},
		} {
			p := parse(t, `{"ops":[{"op":"cell.set","at":"Budget!A1","value":`+tc.json+`}]}`)
			if got := p.Ops[0].Spec.(*ops.CellSet).Value; got != tc.want {
				t.Errorf("%s: got %+v, want %+v", tc.json, got, tc.want)
			}
		}
	})
}

func TestDecodeValueRefusals(t *testing.T) {
	xlsxtest.Skip(t, func() {
		for _, tc := range []struct{ json, code string }{
			{`[1]`, cli.CodeInvalidPlan},
			{`{"text":"a","error":"#N/A"}`, cli.CodeInvalidPlan},
			{`{"colour":"red"}`, cli.CodeInvalidPlan},
			{`null`, cli.CodeInvalidPlan}, // cell.set takes no null
			{`{"error":"#OOPS"}`, cli.CodeInvalidOperation},
			{`{"date":"2024-02-30"}`, cli.CodeInvalidOperation},
			{`{"date":"1900-02-28"}`, cli.CodeInvalidOperation},
			{`{"time":"24:00:00"}`, cli.CodeInvalidOperation},
			{`"="`, cli.CodeInvalidOperation},
			{`"=SUM(1"`, cli.CodeInvalidOperation},
			{`"=SEQUENCE(3)"`, cli.CodeInvalidOperation}, // decision 8: a new spill formula
			{`"=A1#"`, cli.CodeInvalidOperation},
		} {
			e := parseErr(`{"ops":[{"op":"cell.set","at":"Budget!A1","value":` + tc.json + `}]}`)
			if e == nil || e.Code != tc.code || e.Path != "ops[0].value" {
				t.Errorf("%s: got %+v, want %s at ops[0].value", tc.json, e, tc.code)
			}
		}
	})
}

func TestDecodeGrid(t *testing.T) {
	xlsxtest.Skip(t, func() {
		p := parse(t, `{"ops":[{"op":"range.set","at":"Budget!A1:C1","values":[[1,null,""]]}]}`)
		g := p.Ops[0].Spec.(*ops.RangeSet).Values
		if len(g) != 1 || g[0][1] != ops.Null() || g[0][2] != ops.Str("") {
			t.Errorf("grid %+v", g)
		}
		for _, tc := range []struct{ json, path string }{
			{`[[1,2],[3]]`, "ops[0].values[1]"},
			{`[]`, "ops[0].values"},
			{`[[]]`, "ops[0].values[0]"},
		} {
			e := parseErr(`{"ops":[{"op":"range.set","at":"Budget!A1","values":` + tc.json + `}]}`)
			if e == nil || e.Code != cli.CodeInvalidOperation || e.Path != tc.path {
				t.Errorf("%s: got %+v, want invalid_operation at %s", tc.json, e, tc.path)
			}
		}
		if e := parseErr(`{"ops":[{"op":"range.set","at":"Budget!A1","values":[[{"x":1}]]}]}`); e == nil || e.Path != "ops[0].values[0][0]" {
			t.Errorf("element path: %+v", e)
		}
	})
}

func FuzzDecodeValue(f *testing.F) {
	for _, s := range []string{`1`, `"x"`, `{"date":"2024-03-01"}`, `"=A1"`, `{"error":"#N/A"}`, `null`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		xlsxtest.Skip(t, func() {
			_, _ = ops.ParsePlan([]byte(`{"ops":[{"op":"range.set","at":"Budget!A1","values":[[` + v + `]]}]}`)) //nolint:errcheck // must not panic
		})
	})
}

// TestDecodeValueEdges: limits, every error literal, date edges.
func TestDecodeValueEdges(t *testing.T) {
	long := strings.Repeat("a", 32767)
	astral := strings.Repeat("\U0001F600", 16384)   // 32,768 UTF-16 units
	longF := "=" + strings.Repeat("1+", 4095) + "1" // 8,191 characters after "="
	for _, tc := range []struct {
		json string
		ok   bool
	}{
		{`"` + long + `"`, true},
		{`"` + long + `a"`, false},
		{`"` + astral + `"`, false},
		{`"` + longF + `"`, true},
		{`"` + longF + `+1"`, false},
		{`{"error":"#NULL!"}`, true}, {`{"error":"#DIV/0!"}`, true}, {`{"error":"#VALUE!"}`, true},
		{`{"error":"#REF!"}`, true}, {`{"error":"#NAME?"}`, true}, {`{"error":"#NUM!"}`, true},
		{`{"error":"#N/A"}`, true}, {`{"error":"#GETTING_DATA"}`, true}, {`{"error":"#n/a"}`, false},
		{`{"error":"#SPILL!"}`, false},
		{`{"date":"1900-03-01"}`, true}, {`{"date":"9999-12-31"}`, true}, {`{"date":"9999-12-31T23:59:59"}`, true},
		{`{"date":"2024-02-29"}`, true}, {`{"date":"2023-02-29"}`, false}, {`{"date":"2024-13-01"}`, false},
		{`{"date":"2024-03-01T24:00:00"}`, false}, {`{"date":"2024-03-01 13:30:00"}`, false}, {`{"date":"2024-03-01T13:30"}`, false},
		{`{"date":"2024-03-01T13:30:00Z"}`, false}, {`{"time":"00:00:00"}`, true}, {`{"time":"23:59:59"}`, true},
		{`{"time":"13:30"}`, false}, {`{"time":"13:60:00"}`, false}, {`{"text":"=x","date":"2024-03-01"}`, false},
		{`{"text":5}`, false},
	} {
		xlsxtest.Skip(t, func() {
			e := parseErr(`{"ops":[{"op":"cell.set","at":"Budget!A1","value":` + tc.json + `}]}`)
			if (e == nil) != tc.ok {
				short := tc.json
				if len(short) > 40 {
					short = short[:40] + "..."
				}
				t.Errorf("%s: error %+v, want ok %v", short, e, tc.ok)
			}
		})
	}
}
