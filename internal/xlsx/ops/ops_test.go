package ops_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
	"github.com/jhnoel/documentdriver/internal/xlsx/xlsxtest"
)

// TestOpTable: one entry per op, each in its own run_<name>.go with a
// summary and its own Spec type; names are noun.verb.
func TestOpTable(t *testing.T) {
	seen := map[reflect.Type]string{}
	for _, d := range ops.Ops {
		if !strings.Contains(d.Name, ".") || d.Summary == "" {
			t.Errorf("%q: a noun.verb name and a summary", d.Name)
		}
		typ := reflect.TypeOf(d.New())
		if other, dup := seen[typ]; dup {
			t.Errorf("%s and %s share a Spec type", d.Name, other)
		}
		seen[typ] = d.Name
		file := "run_" + strings.ReplaceAll(d.Name, ".", "_") + ".go"
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%s: %s: %v", d.Name, file, err)
		}
	}
	if len(ops.Ops) != 18 {
		t.Errorf("%d ops, the draft contract has 18", len(ops.Ops))
	}
}

// TestRuleExamples: each rule's example breaks that rule: it is refused
// with invalid_operation at the rule's path.
func TestRuleExamples(t *testing.T) {
	for _, d := range ops.Ops {
		for _, r := range d.Rules {
			t.Run(d.Name+"/"+r.Msg, func(t *testing.T) {
				if r.Example == "" {
					t.Fatal("no example")
				}
				xlsxtest.Skip(t, func() {
					e := parseErr(`{"ops":[` + r.Example + `]}`)
					want := "ops[0]"
					if r.Path != "" {
						want += "." + r.Path
					}
					if e == nil || e.Code != cli.CodeInvalidOperation || e.Path != want || e.Message != r.Msg {
						t.Errorf("%s: got %+v, want invalid_operation at %s: %s", r.Example, e, want, r.Msg)
					}
				})
			})
		}
	}
}

// TestParsePlanShape: the envelope and field faults, all reported
// together in plan order.
func TestParsePlanShape(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string // code@path
	}{
		{`{"ops":[]}`, []string{"invalid_plan@ops"}},
		{`{"ops":[{"op":"cell.nope"}]}`, []string{"invalid_plan@ops[0].op"}},
		{`{"ops":[{"op":"sheet.add"}]}`, []string{"invalid_plan@ops[0].name"}},
		{`{"ops":[{"op":"sheet.add","name":"A","extra":1}]}`, []string{"invalid_plan@ops[0].extra"}},
		{`{"ops":[{"op":"row.delete","at":"A1"},{"op":"row.delete","at":"Budget!A1"}]}`,
			[]string{"invalid_operation@ops[0].at", "invalid_operation@ops[1].at"}},
		{`{"ops":[{"op":"row.delete","at":"Budget!$5:$7"}]}`, []string{"invalid_operation@ops[0].at"}},
		{`{"ops":[{"op":"sheet.rename","at":"sheet:A","name":"B"}],"author":"x"}`, []string{"invalid_plan@author"}},
		{`{"ops":[{"op":"row.insert","at":"Budget!5:5","position":"above"}]}`, []string{"invalid_plan@ops[0].position"}},
		{`{"ops":[{"op":"sheet.rename","at":"$1","name":"B"}]}`, []string{"invalid_operation@ops[0].at"}},
	} {
		_, errs := ops.ParsePlan([]byte(tc.text))
		var got []string
		for _, e := range errs {
			got = append(got, e.Code+"@"+e.Path)
		}
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%s: got %v, want %v", tc.text, got, tc.want)
		}
	}
}

// TestParsePlanAddresses: what each address field decodes to.
func TestParsePlanAddresses(t *testing.T) {
	p := parse(t, `{"ops":[{"op":"row.delete","at":"'Q1 Budget'!7:5"},{"op":"column.delete","at":"Budget!E:C"},`+
		`{"op":"sheet.rename","at":"sheet:Bob's","name":"B"},{"op":"name.delete","at":"name:Budget!Local"},`+
		`{"op":"cell.merge","at":"Budget!B2:A1"}]}`)
	rows := p.Ops[0].Spec.(*ops.RowDelete).At
	cols := p.Ops[1].Spec.(*ops.ColumnDelete).At
	sh := p.Ops[2].Spec.(*ops.SheetRename).At
	nm := p.Ops[3].Spec.(*ops.NameDelete).At
	mg := p.Ops[4].Spec.(*ops.CellMerge).At
	if rows.Sheet != "Q1 Budget" || rows.From != 5 || rows.To != 7 {
		t.Errorf("rows %+v", rows)
	}
	if cols.From != 3 || cols.To != 5 {
		t.Errorf("cols %+v", cols)
	}
	if sh.Name != "Bob's" || nm.Name != "Local" || nm.Sheet != "Budget" {
		t.Errorf("sheet %+v name %+v", sh, nm)
	}
	if mg.R1 != 1 || mg.C1 != 1 || mg.R2 != 2 || mg.C2 != 2 {
		t.Errorf("merge %+v", mg)
	}
}

// TestAliases: sheet.add's as names the sheet for later ops; an unknown
// alias is not_found at the field.
func TestAliases(t *testing.T) {
	r := ops.NewPlan(&ops.SheetRename{At: ops.AtSheet("$x"), Name: "B"})
	err := ops.ResolveAliases(r.Ops[0].Spec, map[string]string{})
	if rej, ok := err.(*ops.Rejection); !ok || rej.Code != cli.CodeNotFound || rej.Field != "at" {
		t.Errorf("unknown alias: %v", err)
	}
	if err := ops.ResolveAliases(r.Ops[0].Spec, map[string]string{"x": "sheet:New"}); err != nil {
		t.Fatal(err)
	}
	if got := r.Ops[0].Spec.(*ops.SheetRename).At.Name; got != "New" {
		t.Errorf("resolved to %q", got)
	}
	if got := ops.AliasOf(&ops.SheetAdd{Name: "N", As: ops.Ptr("n")}); got != "n" {
		t.Errorf("AliasOf %q", got)
	}
}

// FuzzParsePlan: any text either decodes or is refused with errors that
// carry a contract code; nothing panics. The stubs' panics skip.
func FuzzParsePlan(f *testing.F) {
	for _, s := range []string{
		`{"ops":[{"op":"cell.set","at":"Budget!A1","value":1}]}`,
		`{"ops":[{"op":"range.set","at":"Budget!A1","values":[[1,null,""],[{"date":"2024-03-01"},"=A1+1",true]]}]}`,
		`{"ops":[{"op":"text.replace","at":"sheet:Budget","select":"a","with":"b","all":true}]}`,
		`{"ops":[{"op":"name.add","name":"Tax","refers_to":"=0.2","scope":"sheet:Budget"}]}`,
		`{"ops":[{"op":"range.format","at":"Budget!A1:B2","bold":true,"fill":"FFEE00"}]}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		xlsxtest.Skip(t, func() {
			p, errs := ops.ParsePlan([]byte(s))
			if (p == nil) == (len(errs) == 0) {
				t.Fatalf("plan %v and errors %v", p, errs)
			}
			for _, e := range errs {
				if e.Code == "" || e.Code == cli.CodeInternal {
					t.Fatalf("error %+v", e)
				}
			}
		})
	})
}
