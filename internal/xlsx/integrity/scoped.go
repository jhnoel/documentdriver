package integrity

import "github.com/jhnoel/documentdriver/internal/opc"

// Scope is what a scoped check reads: the parts a plan changed, by part
// name. The package (content types, relationships), the workbook part,
// the styles and the shared strings' item count are always checked:
// they are small, and an added or removed part, a renamed sheet or an
// interned style is checked through them. A worksheet, a table or the
// calcChain is checked only when it is in Parts; a table in Parts puts
// its owning sheet in scope (a table is checked against its sheet's
// header cells).
type Scope struct {
	Parts []string
}

// CheckScoped is Check restricted to s: the same findings
// Check makes, for the parts the scope reads; findings in a worksheet
// outside the scope are not made, and the sheet is not read. A part in
// Parts that the output no longer has (deleted) is skipped.
// TestGateMatchesFullCheck holds it to Check: on every fixture and
// corpus workbook, CheckScoped with every part in scope has exactly
// Check's findings, and for any scope its findings in the scoped parts
// are Check's in those parts.
func CheckScoped(data []byte, s Scope) *Findings {
	pkg, f, ok := openChecked(data)
	if !ok {
		return f
	}
	defer pkg.Close() //nolint:errcheck // an in-memory package holds nothing to release
	scope := map[string]bool{}
	for _, p := range s.Parts {
		scope[opc.PartKey(p)] = true
	}
	(&checker{f: f, pkg: pkg, scope: scope}).run()
	return f
}

// GateScoped is the commit gate of an xlsx plan: input and
// output are checked with the same scope, and an output with a finding
// the input lacks (Introduced) is refused with a *GateFailure. It
// returns the output's findings either way (the caller reports those on
// the changed parts as pre-existing hazards).
//
// The output is checked first; when it has no finding in the scope,
// nothing can be introduced and the input is not checked, so a clean
// output costs one check. (A narrower input scope, the parts with
// findings only, would not be sound: a table's duplicate-id and
// duplicate-name findings depend on which other tables the scope
// reads.) Cost: the scoped parts of the output, and of the input when
// the output has findings, streamed (worksheets and shared strings
// never as trees).
func GateScoped(input, output []byte, s Scope) (*Findings, error) {
	after := CheckScoped(output, s)
	if after.Empty() {
		return after, nil
	}
	return after, GateFindings(CheckScoped(input, s), after)
}
