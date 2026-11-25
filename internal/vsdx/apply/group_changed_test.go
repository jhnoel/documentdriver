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
// group page0/s7 (300 x 300 pt) changes its children, so changed lists
// s7 and every descendant whose shown values changed (s3, s1, s2, s4,
// s5, s6), not the group alone. The children are bound to the group's
// size by Visio's formulas (s3: PinX Sheet.7!Width*0.2125, PinY
// Sheet.7!Height*0.5, Width Sheet.7!Width*0.425, Height
// Sheet.7!Height*1), which a resize now evaluates (docs/contract.md
// §3, Groups; design §19): s3 becomes 300*0.425 = 127.5 wide and 300
// high, its left at 300*0.2125 - 127.5/2 = 0 from the group's (x
// 56.69) and centred in its height (top 76.54), so [56.69, 76.54,
// 127.5, 300]. Before §19 the children kept their stored sizes and s3
// showed [56.69, 311.34, 96.38, 65.2]. It skips only when the corpus
// is absent.
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
	if b := boxes["page0/s3"]; len(b) != 4 || b[0] != 56.69 || b[1] != 76.54 || b[2] != 127.5 || b[3] != 300 {
		t.Errorf("s3 box = %v, want [56.69 76.54 127.5 300] (scaled by its Sheet.7 formulas)", b)
	}
	if b := boxes["page0/s7"]; len(b) != 4 || b[2] != 300 || b[3] != 300 {
		t.Errorf("s7 box = %v, want 300 x 300", b)
	}
}
