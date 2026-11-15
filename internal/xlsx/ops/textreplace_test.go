package ops_test

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// G4: text.replace (needs K2) (designer rows; frozen).

func TestTextReplace(t *testing.T) {
	rows := `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="inlineStr"><is><t>a cat</t></is></c><c r="C1" t="str"><f>"cat"</f><v>cat</v></c></row>` +
		`<row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2"><v>7</v></c></row>`
	b := book(rows)
	b.SST = []string{"<t>cat cat</t>", "<r><rPr><b/></rPr><t>ca</t></r><r><t>t</t></r>"}
	for _, tc := range []struct {
		name string
		spec *ops.TextReplace
		code string // "" for success
		path string
		want map[string]string
	}{
		{"one in a cell", &ops.TextReplace{At: ops.AtText("Budget!B1"), Select: "cat", With: "dog"}, "", "", map[string]string{"B1": `"a dog"`}},
		{"ambiguous", &ops.TextReplace{At: ops.AtText("Budget!A1:B1"), Select: "cat", With: "dog"}, cli.CodeAmbiguous, "select", nil},
		{"none", &ops.TextReplace{At: ops.AtText("sheet:Budget"), Select: "cow", With: "x"}, cli.CodeNotFound, "select", nil},
		{"occurrence 2", &ops.TextReplace{At: ops.AtText("Budget!A1:B1"), Select: "cat", With: "dog", Occurrence: ops.Ptr(uint32(2))}, "", "", map[string]string{"A1": `"cat dog"`, "B1": `"a cat"`}},
		{"occurrence past", &ops.TextReplace{At: ops.AtText("Budget!A1"), Select: "cat", With: "dog", Occurrence: ops.Ptr(uint32(3))}, cli.CodeNotFound, "occurrence", nil},
		{"all, formulas untouched", &ops.TextReplace{At: ops.AtText("Budget!A1:C1"), Select: "cat", With: "dog", All: true}, "", "", map[string]string{"A1": `"dog dog"`, "B1": `"a dog"`, "C1": `{"f":"=\"cat\"","v":"cat"}`}},
		{"rich text", &ops.TextReplace{At: ops.AtText("Budget!A2"), Select: "cat", With: "dog"}, cli.CodeOperationFailed, "at", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xlsxtest.Skip(t, func() {
				r := runOps(t, b, tc.spec)
				if tc.code != "" {
					r.refused(0, tc.code, tc.path)
					return
				}
				r.ok()
				got := r.cells("Budget!A1:C2")
				for k, v := range tc.want {
					if got[k] != v {
						t.Errorf("%s = %s, want %s", k, got[k], v)
					}
				}
				if strings.Contains(r.sheet(1), `<c r="A1" t="s">`) && tc.want["A1"] != "" && tc.want["A1"] != `"cat cat"` {
					t.Error("a changed shared-string cell still points at the sst (decision 11: written inline)")
				}
				if pt, _ := r.pkg.Part("/xl/sharedStrings.xml"); pt.Patch() != nil {
					t.Error("the shared strings were touched")
				}
			})
		})
	}
}
