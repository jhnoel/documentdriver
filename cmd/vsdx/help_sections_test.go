package main

import (
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/ops"
)

// TestHelpOpSections checks the CC7 rows in the printed help: for
// every implemented op, `help <op>` has the summary, fields, rules
// and refuses sections and is what the CLI prints.
func TestHelpOpSections(t *testing.T) {
	for i := range ops.Ops {
		d := &ops.Ops[i]
		if !d.Implemented() {
			continue
		}
		text, ok := opHelp(d.Name)
		if !ok {
			t.Errorf("opHelp(%s) reports no help", d.Name)
			continue
		}
		for _, section := range []string{"fields:\n", "rules:\n", "refuses:\n"} {
			if !strings.Contains(text, section) {
				t.Errorf("help %s lacks section %q:\n%s", d.Name, section, text)
			}
		}
		if code, got, _ := runCLI(t, "help", d.Name); code != cli.ExitOK || got != text {
			t.Errorf("help %s exit %d, stdout differs from opHelp", d.Name, code)
		}
	}
}

// TestHelpObjectSetRows checks the object.set CC7 row: a note for
// every field, the refuses list, and (CC10 having removed it) no
// from/from_glue exclusivity rule.
func TestHelpObjectSetRows(t *testing.T) {
	text, ok := opHelp("object.set")
	if !ok {
		t.Fatalf("opHelp(object.set) reports no help")
	}
	// Every field line under fields: carries a note after ". ".
	fields := text[strings.Index(text, "fields:\n")+len("fields:\n"):]
	fields = fields[:strings.Index(fields, "\nrules:\n")]
	for _, line := range strings.Split(strings.TrimSpace(fields), "\n") {
		if !strings.Contains(line, ". ") {
			t.Errorf("object.set field line has no note: %q", line)
		}
	}
	for _, want := range []string{
		"formula-bound cells",
		"containers/lists/callouts",
		"scaled-page geometry",
		"masters and stencils",
		"both-ends-glued move",
		"whole-shape glue on a connector without the dynamic glue cells",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help object.set lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "connector takes from or from_glue, not both") {
		t.Errorf("help object.set still carries the rule CC10 removed:\n%s", text)
	}
}

// TestHelpOtherRows checks the remaining CC7 rows: object.delete's
// inherited sub-shape refusal, text.replace's page-wide skip, and
// prop.add's ASCII name rule.
func TestHelpOtherRows(t *testing.T) {
	cases := []struct {
		op, section, want string
	}{
		{"object.delete", "refuses:\n", "a sub-shape a master instance inherits"},
		{"text.replace", "refuses:\n", "containers, lists and callouts are skipped by a page-wide replace"},
		{"prop.add", "rules:\n", "name must be ASCII letters, digits and _, not starting with a digit"},
	}
	for _, tc := range cases {
		text, ok := opHelp(tc.op)
		if !ok {
			t.Fatalf("opHelp(%s) reports no help", tc.op)
		}
		section := text[strings.Index(text, tc.section):]
		if !strings.Contains(section, tc.want) {
			t.Errorf("help %s %s lacks %q:\n%s", tc.op, tc.section, tc.want, text)
		}
	}
}
