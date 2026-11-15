package integrity

import (
	"slices"
	"strings"
)

// A difference is an intended difference from docxdriver 0.5.2's findings
// (docs/docx-integrity.md gives the evidence for each). rust matches the
// findings docxdriver reports that the Go checks do not, goSide the
// reverse; nil matches nothing. The parity tests accept a difference
// between the two only when every finding on either side is matched by
// one of these.
type difference struct {
	name         string
	rust, goSide func(Finding) bool
}

func message(ms ...string) func(Finding) bool {
	return func(f Finding) bool { return slices.Contains(ms, f.Message) }
}

func contains(s string) func(Finding) bool {
	return func(f Finding) bool { return strings.Contains(f.Message, s) }
}

func expectedTypeMessage(f Finding) bool {
	for _, rel := range []string{"officeDocument", "comments"} {
		if strings.Contains(f.Message, " is not what Word writes for "+rel+" parts (") {
			return true
		}
	}
	return false
}

var intendedDifferences = []difference{
	{"expected content types from the ctypes table", expectedTypeMessage, expectedTypeMessage},
	{"attribute syntax errors named, not \"position \"",
		message("xml: not well-formed: position "),
		message("xml: not well-formed: duplicate attribute", "xml: not well-formed: malformed attribute", "xml: not well-formed: invalid element name")},
	{"non-well-formed XML docxdriver accepted", nil, message(
		"xml: not well-formed: XML declaration not at the start", "xml: not well-formed: malformed XML declaration",
		"xml: not well-formed: invalid processing instruction target", "xml: not well-formed: missing white space between attributes",
		"xml: not well-formed: < in an attribute value", "xml: not well-formed: CDATA section outside the root element",
		"xml: reserved namespace bound to a prefix")},
	{"CDATA outside the root named", message("xml: not well-formed: text outside the root element"), message("xml: not well-formed: CDATA section outside the root element")},
	{"reserved namespace binding reported at the binding", message("xml: reserved namespace used for an attribute"), message("xml: reserved namespace bound to a prefix")},
	{"xsd:dateTime days checked against the month", nil, contains("is not a valid ST_DateTime")},
	{"xml:space's anonymous type named by the attribute, not anonymous1", contains("is not a valid anonymous1"), contains("/@xml:space is not a valid xml:space")},
	{"NCName by the XML Name rules", message("integrity: relationship Id is not a valid xsd:ID"), message("integrity: relationship Id is not a valid xsd:ID")},
	{"entries neither stored nor deflated named", message("package: entry cannot be read"), message("package: entry is neither stored nor deflated")},
	{"entries checked against their declared size", nil, message("package: entry cannot be read")},
	{"reading limits", nil, contains("exceeds a safety limit")},
}

// explained returns the names of the intended differences that account for
// the findings only docxdriver reported (rustOnly) and only the Go checks
// reported (goOnly), and whether they account for all of them.
func explained(rustOnly, goOnly []Finding) ([]string, bool) {
	var names []string
	match := func(fs []Finding, side func(difference) func(Finding) bool) bool {
		for _, f := range fs {
			i := slices.IndexFunc(intendedDifferences, func(d difference) bool { return side(d) != nil && side(d)(f) })
			if i < 0 {
				return false
			}
			if n := intendedDifferences[i].name; !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		return true
	}
	ok := match(rustOnly, func(d difference) func(Finding) bool { return d.rust })
	ok = match(goOnly, func(d difference) func(Finding) bool { return d.goSide }) && ok
	return names, ok
}

// compareFindings returns the kinds (with counts folded in) only want and
// only got have.
func diffFindings(want, got map[Finding]int) (wantOnly, gotOnly []Finding) {
	for k, n := range want {
		if got[k] != n {
			wantOnly = append(wantOnly, k)
		}
	}
	for k, n := range got {
		if want[k] != n {
			gotOnly = append(gotOnly, k)
		}
	}
	slices.SortFunc(wantOnly, compareFindings)
	slices.SortFunc(gotOnly, compareFindings)
	return wantOnly, gotOnly
}
