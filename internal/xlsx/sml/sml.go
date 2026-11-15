// Package sml is the SpreadsheetML vocabulary every xlsx package shares:
// the namespaces, the small attribute grammars (indexes, booleans), the
// _xHHHH_ string escape, and readers of the workbook's sheet and defined
// name entries and of a table part, over trees their callers parsed.
//
// It is a leaf: it imports the tree, opc, ctypes and formula packages
// and no other xlsx package, so the integrity check can use it without
// depending on the sheet scanner it checks. It never parses a part (no
// Part.Tree or tree.Parse*): callers in archtest's xlsxTreeFiles do, and
// pass nodes here.
package sml

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// The namespaces of SpreadsheetML parts, Transitional and Strict.
const (
	// NSMain is the Transitional SpreadsheetML main namespace.
	NSMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	// NSStrict is the Strict SpreadsheetML main namespace.
	NSStrict = "http://purl.oclc.org/ooxml/spreadsheetml/main"
	// NSRel is the Transitional relationships namespace (r:id).
	NSRel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	// NSRelStrict is the Strict relationships namespace.
	NSRelStrict = "http://purl.oclc.org/ooxml/officeDocument/relationships"
	// NSPackageRel is the namespace of a .rels part.
	NSPackageRel = "http://schemas.openxmlformats.org/package/2006/relationships"
)

// IsMain reports whether uri is a SpreadsheetML main namespace
// (Transitional or Strict).
func IsMain(uri string) bool { return uri == NSMain || uri == NSStrict }

// IsRel reports whether uri is a relationships namespace (Transitional or
// Strict), the namespace of r:id.
func IsRel(uri string) bool { return uri == NSRel || uri == NSRelStrict }

// MainChild returns the first element child of n whose name is local in
// a SpreadsheetML main namespace, or nil.
func MainChild(n *tree.Node, local string) *tree.Node {
	for e := range n.ElementsSeq() {
		if e.Local() == local && IsMain(e.Space()) {
			return e
		}
	}
	return nil
}

// IsTrue reports whether a boolean attribute value is true: "1" or
// "true", as Excel writes them. Anything else, absent included, is
// false.
func IsTrue(s string) bool { return s == "1" || s == "true" }

// ParseIndex parses an index or count attribute: an unsigned decimal of
// one to nine digits, no sign, no spaces (so the value fits any int). It
// reports false for anything else.
func ParseIndex(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
