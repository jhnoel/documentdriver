package ops

// init registers the page.add at/position rule with the
// document-independent rules (chunk CC13).
func init() {
	extraRules = append(extraRules, checkPageAddTogether)
}

// checkPageAddTogether is the extra rule that at and position go
// together on page.add: it applies to page.add, and reports the pair
// as one fault at the operation when exactly one of them is set.
func checkPageAddTogether(s Spec) (string, string, bool) {
	v, ok := s.(*PageAddSpec)
	if !ok {
		return "", "", false
	}
	if (v.At == "") != (v.Position == "") {
		return "at and position go together: the page to add next to, and before or after", "", true
	}
	return "", "", false
}
