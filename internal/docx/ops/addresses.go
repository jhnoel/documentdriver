package ops

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/model"
)

// The addresses of the contract (docs/contract.md, section 6) that plan
// fields hold besides a paragraph address. Each is a string in the form
// read shows; a rule of the operation checks the form, so that a
// malformed address is an invalid_operation naming the field.

// SectionAddress is a section: "section:<paraId>" (eight hex digits, the
// w14:paraId of the paragraph that ends it, which carries its w:sectPr),
// the stable form read shows; "section:final" for the body's last
// section, which no paragraph ends; "section:N" (N from 1, positional)
// as a shorthand; or "section:$name" for a section an earlier operation
// named. A value of eight hex digits (with "~N" for a duplicated id) is
// always a paragraph id, any other a number.
type SectionAddress string

// rest is the address without its "section:" prefix.
func (a SectionAddress) rest() (string, bool) { return strings.CutPrefix(string(a), "section:") }

// ParaID returns the boundary paragraph's id of a "section:<paraId>"
// address, upper-cased: eight hex digits, with "~N" (N from 2) for the
// Nth paragraph sharing a duplicated id, as read shows it.
func (a SectionAddress) ParaID() (string, bool) {
	rest, ok := a.rest()
	if !ok || !isParaIDForm(rest) {
		return "", false
	}
	return strings.ToUpper(rest), true
}

// isParaIDForm reports whether s is eight hex digits, optionally followed
// by "~N" (N from 2, no leading zero).
func isParaIDForm(s string) bool {
	id, dup, hasDup := strings.Cut(s, "~")
	if hasDup && !validDuplicate(dup) {
		return false
	}
	if len(id) != 8 {
		return false
	}
	for _, c := range id {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// Number returns the section's 1-based number, or final for
// "section:final"; ok is false for a paragraph id, an alias or a
// malformed address.
func (a SectionAddress) Number() (n uint32, final, ok bool) {
	rest, isSection := a.rest()
	if !isSection || isParaIDForm(rest) {
		return 0, false, false
	}
	if rest == "final" {
		return 0, true, true
	}
	v, isNumber := model.ParseU32(rest)
	return v, false, isNumber && v > 0
}

// Alias returns the name of a section alias ("section:$name").
func (a SectionAddress) Alias() (string, bool) {
	name, ok := strings.CutPrefix(string(a), "section:$")
	return name, ok && cli.ValidAlias(name)
}

// Valid reports whether the address has one of the four forms.
func (a SectionAddress) Valid() bool {
	_, _, ok := a.Number()
	_, alias := a.Alias()
	_, id := a.ParaID()
	return ok || alias || id
}

// CommentAddress is "comment:N", N the comment's id in the file.
type CommentAddress string

// ID returns the comment's id; ok is false for a malformed address.
func (a CommentAddress) ID() (string, bool) {
	id, ok := strings.CutPrefix(string(a), "comment:")
	return id, ok && ascii.Digits(id)
}

// BookmarkAddress is "bookmark:Name".
type BookmarkAddress string

// Name returns the bookmark's name; ok is false for a malformed address
// or an empty name.
func (a BookmarkAddress) Name() (string, bool) {
	name, ok := strings.CutPrefix(string(a), "bookmark:")
	return name, ok && name != ""
}

// EquationAddress is an equation of a paragraph: "0A1B2C3D/equation:2"
// (N from 1, counted in the paragraph), or the paragraph's address alone
// for its only equation.
type EquationAddress struct {
	// Paragraph is the paragraph's address.
	Paragraph ParagraphAddress
	// N is the equation's 1-based number, nil when the address names the
	// paragraph alone.
	N *uint32
}

const equationInfix = "/equation:"

// encodeJSON returns the equation address as a string value.
func (a EquationAddress) encodeJSON() *value {
	s := string(a.Paragraph)
	if a.N != nil {
		s += equationInfix + strconv.FormatUint(uint64(*a.N), 10)
	}
	return &value{kind: kString, s: s}
}

// RevisionScope is what a revision operation's `at` names: one revision
// ("revision:7"), or the paragraphs whose revisions are meant.
type RevisionScope struct {
	// ID is the revision's id in the file, "" when paragraphs are given.
	ID string
	// Paragraphs are the paragraphs whose revisions are meant.
	Paragraphs []ParagraphAddress
}

// RevisionAuthor is the author whose revisions are meant; not empty.
type RevisionAuthor string

// encodeJSON returns the scope as a paragraph list or revision address.
func (r RevisionScope) encodeJSON() *value {
	if r.Paragraphs != nil {
		out := &value{kind: kArray, a: []*value{}}
		for _, p := range r.Paragraphs {
			out.a = append(out.a, &value{kind: kString, s: string(p)})
		}
		return out
	}
	return &value{kind: kString, s: "revision:" + r.ID}
}
