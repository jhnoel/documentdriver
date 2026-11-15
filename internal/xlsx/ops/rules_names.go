package ops

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jhnoel/documentdriver/internal/xlsx/formula"
)

// name.add's and name.set's rules (designer-owned).
var (
	nameAddRules = []Rule{
		rule("a name starts with a letter, _ or \\, has letters, digits, _, . and \\ after, is not a cell reference (A1, R1C1) and is at most 255 characters", "name",
			func(o *NameAdd) bool { return !validDefinedName(o.Name) },
			`{"op":"name.add","name":"A1","refers_to":"=1"}`),
		rule("refers_to must be a formula: = and a valid formula", "refers_to",
			func(o *NameAdd) bool { return !validRefersTo(o.RefersTo) },
			`{"op":"name.add","name":"Tax","refers_to":"0.2"}`),
	}
	nameSetRules = []Rule{
		rule("refers_to must be a formula: = and a valid formula", "refers_to",
			func(o *NameSet) bool { return !validRefersTo(o.RefersTo) },
			`{"op":"name.set","at":"name:Tax","refers_to":"=(1"}`),
	}
)

// validDefinedName reports whether s is a name Excel accepts for a new
// defined name. Built-in names (_xlnm.) are not written by plans.
func validDefinedName(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > 255 || strings.HasPrefix(strings.ToLower(s), "_xlnm.") {
		return false
	}
	for i, r := range s {
		ok := unicode.IsLetter(r) || r == '_' || r == '\\' || (i > 0 && (unicode.IsDigit(r) || r == '.'))
		if !ok {
			return false
		}
	}
	if formula.LooksLikeCellRef(s) {
		return false
	}
	u := strings.ToUpper(s)
	if u == "R" || u == "C" {
		return false
	}
	return true
}

// validRefersTo reports whether s is "=" and a formula whose stored form
// validates (formula.Store, formula.Validate).
func validRefersTo(s string) bool {
	body, ok := strings.CutPrefix(s, "=")
	if !ok || body == "" {
		return false
	}
	stored, err := formula.Store(body)
	return err == nil && formula.Validate(stored) == nil
}
