package ops_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jhnoel/documentdriver/internal/vsdx/apply"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// TestRefusalsSubshapeV2S4 checks the CC6b row: object.delete of V2
// page0/s4, a master-inherited sub-shape (MasterShape 6 inside the
// master3 instance page0/s3), is refused with operation_failed at
// ops[0].at and the inherited-from-master message.
func TestRefusalsSubshapeV2S4(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	expectRefusal(t, path, `{"ops":[{"op":"object.delete","at":"page0/s4"}]}`, "at",
		"page0/s4 is inherited from master3 (master shape 6): delete the instance page0/s3, or edit the master in Visio")
	expectRefusal(t, path, `{"ops":[{"op":"object.delete","at":"page0/s5"}]}`, "at",
		"page0/s5 is inherited from master3 (master shape 7): delete the instance page0/s3, or edit the master in Visio")
}

// TestRefusalsSubshapeV2Instance checks the CC6b row: object.delete of
// V2 page0/s3, the master instance itself, is accepted and deletes
// page0/s3, page0/s4 and page0/s5 with it, in document order.
func TestRefusalsSubshapeV2Instance(t *testing.T) {
	path := vsdxtest.Fixture(t, "v2-master")
	out := filepath.Join(t.TempDir(), "out.vsdx")
	res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.delete","at":"page0/s3"}]}`), apply.Options{Out: out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"page0/s3", "page0/s4", "page0/s5"}
	if !slices.Equal(res.Deleted, want) {
		t.Errorf("deleted = %v, want %v", res.Deleted, want)
	}
	checkNoNewFindings(t, path, out)
}

// TestRefusalsSubshapeV4PlainChildren checks the CC6b row: object.delete
// of V4's plain group children (no MasterShape) is accepted as today,
// deleting only the targeted child.
func TestRefusalsSubshapeV4PlainChildren(t *testing.T) {
	path := vsdxtest.Fixture(t, "v4-groups")
	for _, tc := range []struct {
		at   string
		want []string
	}{
		{"page0/s11", []string{"page0/s11"}},
		{"page0/s13", []string{"page0/s13"}},
		{"page0/s21", []string{"page0/s21"}},
	} {
		out := filepath.Join(t.TempDir(), "out.vsdx")
		res, err := apply.Run(path, parsePlan(t, `{"ops":[{"op":"object.delete","at":"`+tc.at+`"}]}`), apply.Options{Out: out})
		if err != nil {
			t.Fatalf("%s: Run: %v", tc.at, err)
		}
		if !slices.Equal(res.Deleted, tc.want) {
			t.Errorf("%s: deleted = %v, want %v", tc.at, res.Deleted, tc.want)
		}
		checkNoNewFindings(t, path, out)
	}
}
