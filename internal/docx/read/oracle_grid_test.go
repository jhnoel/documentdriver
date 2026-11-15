package read

import (
	"fmt"
	"testing"
)

// TestFromOracleGridColumn checks that the oracle projection counts a
// row's w:gridBefore in each cell's grid column, as read does.
func TestFromOracleGridColumn(t *testing.T) {
	out := `{"outcome":"completed","result":{"tables":[{"at":"table:1","rows":[` +
		`{"properties":{"gridBefore":{"val":"2"}},"cells":[{"grid_span":2},{"grid_span":1}]},` +
		`{"properties":{},"cells":[{"grid_span":1},{"grid_span":1}]}]}]}}`
	got, err := FromOracle("document", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for _, r := range got["tables"].([]any)[0].(Object)["rows"].([]any) {
		for _, c := range r.(Object)["cells"].([]any) {
			cols = append(cols, fmt.Sprint(c.(Object)["grid_column"]))
		}
	}
	if want := "[3 5 1 2]"; fmt.Sprint(cols) != want {
		t.Errorf("grid columns %v, want %s", cols, want)
	}
}
