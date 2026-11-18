package ops

// init registers the html check with the document-independent rules:
// every html field is parsed against the vsdx text grammar in the
// document-independent pass, before the file is read (chunk CC5).
func init() {
	extraRules = append(extraRules, checkHTML)
}

// checkHTML is the extra rule that parses an op's html field: it
// applies to text.set and to object.add, the ops that carry html, and
// reports the grammar fault's message and path field.
func checkHTML(s Spec) (string, string, bool) {
	html, ok := htmlOf(s)
	if !ok {
		return "", "", false
	}
	return htmlFault(html)
}
