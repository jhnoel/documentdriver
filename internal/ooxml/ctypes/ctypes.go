// Package ctypes is the one table of OPC content types and relationship
// types the engine writes and checks. Every entry was observed in
// Office-authored files (docProps/app.xml names Word, PowerPoint or
// Excel), never written from memory: provenance.json names, per entry, the
// file it was first seen in and how many Word, PowerPoint and Excel files
// use it. The
// constants and tables in table_gen.go are generated from it by
// tools/ctypesgen (go generate); a drift test fails when they differ.
//
// The package knows nothing about a document format; every format imports
// it. Code outside this package, its generator and tests must not
// spell a content type or relationship type literally (enforced by
// internal/archtest).
package ctypes

//go:generate go run ../../../tools/ctypesgen

import "strings"

// Entry is a content type or relationship type with its evidence.
type Entry struct {
	// Name is the Go constant's name.
	Name string
	// Value is the content type or relationship type URI.
	Value string
	// Files counts the Office-authored files using it; Word, PowerPoint and
	// Excel split the count by product.
	Files, Word, PowerPoint, Excel int
	// External counts files where a relationship of this type was
	// TargetMode="External" (relationship types only).
	External int
	// Source is the first file it was seen in, as corpus-label/path, and
	// SHA256 that file's digest.
	Source, SHA256 string
}

// Target says a relationship type pointed at parts of a content type.
type Target struct {
	Rel, ContentType string
	// Files counts the Office-authored files with such a relationship;
	// Word, PowerPoint and Excel split it by product.
	Files, Word, PowerPoint, Excel int
	Source                         string
}

// ContentTypes returns a copy of the content-type table.
func ContentTypes() []Entry { return append([]Entry(nil), contentTypes...) }

// RelTypes returns a copy of the relationship-type table.
func RelTypes() []Entry { return append([]Entry(nil), relTypes...) }

// Targets returns a copy of the relationship-target table.
func Targets() []Target { return append([]Target(nil), relTargets...) }

// ExpectedContentTypes returns the content types parts targeted by a
// relationship of type rel have in Word- and PowerPoint-authored files
// (pairs seen in at least minFiles of them), or nil when there is none.
// Pairs only Excel files show are left out, so the workbook corpus never
// widens what docx and pptx accept.
func ExpectedContentTypes(rel string) []string {
	var out []string
	for _, t := range relTargets {
		if t.Rel == rel && t.Word+t.PowerPoint >= minFiles {

			out = append(out, t.ContentType)
		}
	}
	return out
}

// Last returns the final path segment of a relationship type URI, which is
// the same in the transitional and strict vocabularies.
func Last(rel string) string {
	return rel[strings.LastIndex(rel, "/")+1:]
}

// Is reports whether actual is the relationship type want in either the
// transitional or the strict vocabulary: they differ only before the last
// segment. want is a Rel constant.
func Is(actual, want string) bool {
	return strings.HasSuffix(actual, "/"+Last(want))
}

// Rebase returns the type of a sibling relationship in the same
// vocabulary as actual (so a strict slideLayout yields the strict image
// type): actual's prefix up to the last segment plus the last segment of
// want. It reports false if actual has no prefix.
func Rebase(actual, want string) (string, bool) {
	i := strings.LastIndex(actual, "/")
	if i < 0 {
		return "", false
	}
	return actual[:i+1] + Last(want), true
}
