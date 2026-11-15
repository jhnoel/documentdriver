package main

// A Case is one CLI invocation run on every input file, by the pinned
// docxdriver 0.5.2 CLI and by the Go docx CLI alike. Adding a subcommand
// to the comparison is one entry here.
type Case struct {
	// Name identifies the case in manifests and reports.
	Name string
	// Args builds the arguments for an input file, given as its base name
	// (both CLIs run with the file's directory as working directory,
	// because docxdriver refuses paths outside its working directory).
	Args func(file string) []string
	// Ported reports whether the Go docx CLI implements the case; compare
	// runs ported cases unless -cases names others.
	Ported bool
}

// cases is every recorded case, in report order. The read-document cases
// are the HTML projection: the agent markup (kind document, the default)
// and the UI profile (kind document_ui: full HTML, source map, paragraph
// entries), each in the three views.
var cases = []Case{
	{Name: "read-document", Args: read(), Ported: true},
	{Name: "read-document-final", Args: read("--view", "final"), Ported: true},
	{Name: "read-document-original", Args: read("--view", "original"), Ported: true},
	// document_ui is not in the agent contract (docs/contract.md,
	// section 12): recorded, never compared.
	{Name: "read-document-ui", Args: read("--kind", "document_ui")},
	{Name: "read-document-ui-final", Args: read("--kind", "document_ui", "--view", "final")},
	{Name: "read-document-ui-original", Args: read("--kind", "document_ui", "--view", "original")},
	{Name: "read-styles", Args: read("--kind", "styles"), Ported: true},
	{Name: "read-comments", Args: read("--kind", "comments"), Ported: true},
	{Name: "read-revisions", Args: read("--kind", "revisions"), Ported: true},
	{Name: "read-bookmarks", Args: read("--kind", "bookmarks"), Ported: true},
	{Name: "read-assets", Args: read("--kind", "assets"), Ported: true},
	{Name: "find", Args: find(""), Ported: true},
	{Name: "find-final", Args: find("", "--view", "final"), Ported: true},
	{Name: "find-original", Args: find("", "--view", "original"), Ported: true},
	{Name: "find-ignore-case", Args: find("THE", "--ignore-case"), Ported: true},
}

// read returns a case argument builder for the read command with flags.
func read(flags ...string) func(string) []string {
	return func(file string) []string {
		return append([]string{"read", file}, flags...)
	}
}

// find matches query; the empty query matches every paragraph, so the
// find cases cover every paragraph entry of a document.
func find(query string, flags ...string) func(string) []string {
	return func(file string) []string {
		return append([]string{"find", file, query}, flags...)
	}
}

// caseByName returns the case called name.
func caseByName(name string) (Case, bool) {
	for _, c := range cases {
		if c.Name == name {
			return c, true
		}
	}
	return Case{}, false
}
