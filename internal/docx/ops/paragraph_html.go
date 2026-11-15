package ops

import (
	"errors"
	"regexp"
	"strings"

	"github.com/jhnoel/documentdriver/internal/docx/build"
	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/math"
)

// paragraphHTML is one html value of an operation that writes a whole
// paragraph (paragraph.insert; paragraph.replace when it is ported):
// either one block element, <p> or <h1>..<h6> as read shows it, whose
// storable attributes set the paragraph's properties, or bare inline
// content, which is the content alone.
type paragraphHTML struct {
	Atoms []html.Atom
	// Attrs are the block element's storable attributes (its tag, its
	// style and its paragraph CSS); nil for bare inline content.
	Attrs *html.ParaAttrs
}

// The refusals of a paragraph's html, each at the html field.
const (
	errSeveralParagraphs = "one paragraph per html value; use the list form for several"
	errMixedParagraph    = "html is one paragraph element (<p> or <h1>..<h6>) or inline content, not inline content with a paragraph element in it; " + errSeveralParagraphs
)

// blockStart matches a value that opens with a paragraph element.
var blockStart = regexp.MustCompile(`(?i)^\s*<(p|h[1-6])[\s/>]`)

// parseParagraphHTML reads one html value of a paragraph. The derived
// attributes read writes on a paragraph (its id, revision marks on the
// paragraph mark, list membership and marker, which other operations
// own) are ignored, so a paragraph's markup as read shows it can be
// sent back as it is.
func parseParagraphHTML(s string) (paragraphHTML, error) {
	return parseParagraph(s, build.InlineAtoms)
}

// parseReplacementHTML reads the html of paragraph.replace: a whole
// paragraph's content as read shows it, so linked and unlinked text and
// several links may mix (docxdriver's one-link rule is for inserted
// text), and placeholders and equations carry their addresses.
func parseReplacementHTML(s string) (paragraphHTML, error) {
	return parseParagraph(s, build.ParagraphAtoms)
}

// parseParagraph parses html s into a paragraph's atoms and attributes.
func parseParagraph(s string, inline func([]html.Item) ([]html.Atom, error)) (paragraphHTML, error) {
	if !blockStart.MatchString(s) {
		items, err := html.ParseFragment("<p>"+s+"</p>", math.LaTeXToOMML)
		if err != nil {
			if isStrayParagraphEnd(err) {
				return paragraphHTML{}, errors.New(errMixedParagraph)
			}
			return paragraphHTML{}, err
		}
		atoms, err := inline(items)
		return paragraphHTML{Atoms: atoms}, err
	}
	items, err := html.ParseFragment(s, math.LaTeXToOMML)
	if err != nil {
		if isStrayParagraphEnd(err) {
			return paragraphHTML{}, errors.New(errMixedParagraph)
		}
		return paragraphHTML{}, err
	}
	var attrs *html.ParaAttrs
	for _, it := range items {
		if it.Kind != html.ItemParaOpen {
			continue
		}
		if attrs != nil {
			return paragraphHTML{}, errors.New(errSeveralParagraphs)
		}
		a := *it.Para
		a.BreakIns, a.BreakDel, a.Num, a.List = html.WrapID{}, html.WrapID{}, html.Opt[string]{}, html.Opt[html.ListAttrs]{}
		attrs = &a
	}
	if attrs == nil {
		return paragraphHTML{}, errors.New(errMixedParagraph)
	}
	if attrs.Tag != "p" && attrs.Class.OK {
		// read shows a heading's style as data-style; it is the tag's.
		if attrs.Class.V != build.StyleOf(html.ParaAttrs{Tag: attrs.Tag}) {
			return paragraphHTML{}, errors.New("<" + attrs.Tag + "> takes its style from the tag; data-style or class is only for <p>")
		}
		attrs.Class = html.Opt[string]{}
	}
	atoms, err := inline(items)
	return paragraphHTML{Atoms: atoms, Attrs: attrs}, err
}

// isStrayParagraphEnd reports whether err is the HTML front end's
// refusal of a paragraph's end tag with nothing to close, which is what
// a paragraph element inside inline content (or after it) reads as.
func isStrayParagraphEnd(err error) bool {
	m := err.Error()
	if !strings.Contains(m, "has no open <") {
		return false
	}
	for _, t := range []string{"p", "h1", "h2", "h3", "h4", "h5", "h6"} {
		if strings.Contains(m, "</"+t+"> has no open <"+t+">") {
			return true
		}
	}
	return false
}
