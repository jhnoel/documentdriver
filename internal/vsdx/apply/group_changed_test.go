package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// TestGroupResizeListsDescendants is the groups row on
// dave-howard-vsdx/tests/test10_nested_shapes.vsdx: object.set box on
// group page0/s7 (300 x 300 pt) moves the shown y of its children,
// whose stored local positions are measured from the group's
// lower-left, so changed lists s7 and every descendant whose shown
// values changed (s3, s1, s2, s4, s5, s6), not the group alone. s3's y
// goes from 76.54 to 311.34. It skips only when the corpus is absent.
func TestGroupResizeListsDescendants(t *testing.T) {
	dir := corpusDir()
	if dir == "" {
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	src := filepath.Join(dir, "repos", "dave-howard-vsdx", "tests", "test10_nested_shapes.vsdx")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("corpus file: %v", err)
	}
	p, errs := ops.ParsePlan([]byte(`{"ops":[{"op":"object.set","at":"page0/s7","box":[56.69,76.54,300,300]}]}`))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	res, err := Run(src, p, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	boxes := map[string][]float64{}
	for _, raw := range res.Changed {
		var o struct {
			ID  string    `json:"id"`
			Box []float64 `json:"box"`
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatal(err)
		}
		got = append(got, o.ID)
		boxes[o.ID] = o.Box
	}
	want := []string{"page0/s7", "page0/s3", "page0/s1", "page0/s2", "page0/s4", "page0/s5", "page0/s6"}
	if len(got) != len(want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("changed = %v, want %v", got, want)
		}
	}
	if b := boxes["page0/s3"]; len(b) != 4 || b[1] != 311.34 || b[2] != 96.38 || b[3] != 65.2 {
		t.Errorf("s3 box = %v, want [56.69 311.34 96.38 65.2]", b)
	}
	if b := boxes["page0/s7"]; len(b) != 4 || b[2] != 300 || b[3] != 300 {
		t.Errorf("s7 box = %v, want 300 x 300", b)
	}
}
