package main

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/xlsx/ops"
)

func TestHelpFromOperationTable(t *testing.T) {
	listing := opsHelp()
	for i := range ops.Ops {
		d := &ops.Ops[i]
		if !strings.Contains(listing, d.Name) {
			t.Errorf("listing lacks %s", d.Name)
		}
		text, ok := opHelp(d.Name)
		if !ok || !strings.Contains(text, d.Summary) {
			t.Errorf("help for %s missing summary", d.Name)
		}
		for _, r := range d.Rules {
			if !strings.Contains(text, r.Msg) {
				t.Errorf("help for %s missing rule %q", d.Name, r.Msg)
			}
		}
		for _, s := range append(append([]string{}, d.Adjusts...), d.Refuses...) {
			if !strings.Contains(text, s) {
				t.Errorf("help for %s missing %q", d.Name, s)
			}
		}
	}
	if text, ok := opHelp("not.an.op"); ok || text != "" {
		t.Errorf("unknown op: %q, %v", text, ok)
	}
}

func TestHelpPlanTypes(t *testing.T) {
	for _, tc := range []struct{ op, want string }{
		{"cell.set", "value, required"},
		{"range.set", "rows of values, required"},
		{"row.insert", "before|after, required"},
		{"name.add", "workbook|sheet:Name, optional"},
		{"text.replace", "address, required"},
	} {
		text, ok := opHelp(tc.op)
		if !ok || !strings.Contains(text, tc.want) {
			t.Errorf("%s lacks %q: %s", tc.op, tc.want, text)
		}
	}
}
