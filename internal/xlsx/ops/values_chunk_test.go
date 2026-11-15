package ops

import (
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
)

func TestChunkValueAndGrid(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  CellValue
		bad   bool
	}{
		{`42`, Num(42), false}, {`{"text":"=x"}`, Str("=x"), false},
		{`{"date":"1900-03-01"}`, DateOf("1900-03-01"), false},
		{`{"date":"1900-02-28"}`, CellValue{}, true}, {`{"time":"01:02:03.1"}`, CellValue{}, true},
		{`1e9999`, CellValue{}, true},
	} {
		v, err := cli.ParseJSON([]byte(tc.input))
		if err != nil {
			t.Fatal(err)
		}
		d := cli.NewDecoder([]byte(tc.input))
		var got CellValue
		got.DecodePlan(v, "value", d)
		if (d.Err() != nil) != tc.bad || !tc.bad && got != tc.want {
			t.Errorf("%s: %+v, %v", tc.input, got, d.Err())
		}
	}
	input := `[[null,""],[1,true]]`
	v, err := cli.ParseJSON([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	d := cli.NewDecoder([]byte(input))
	var g Grid
	g.DecodePlan(v, "values", d)
	if d.Err() != nil || len(g) != 2 || g[0][0] != Null() || g[0][1] != Str("") {
		t.Fatalf("grid %+v: %v", g, d.Err())
	}
}
