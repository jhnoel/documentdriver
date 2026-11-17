package main

import (
	"io"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/read"
)

var xmlCommand = &cli.Command{
	Name:    "xml",
	Args:    []string{"drawing.vsdx", "address"},
	Summary: "print the source XML of one shape",
	Help: `Prints the exact source bytes of one shape, addressed as vsdx read shows
it (page0/s17, master2/s5), its nested shapes included. The output is
XML, not JSON.`,
}

// runXML is the xml handler (chunk C1): read.XML, bytes and a newline.
// An address that is not a shape address (no slash, as pptx xml
// refuses a bare slide name) is usage; a well-formed shape address
// that names nothing is read.XML's not_found.
func runXML(p *cli.Parsed, stdout io.Writer) error {
	_, id, ok := strings.Cut(p.Args[1], "/")
	if !ok || id == "" {
		return cli.Errorf(cli.CodeUsage, "", "%q is not a shape address; give one as vsdx read shows it, such as page0/s5", p.Args[1])
	}
	input, err := readInput(p.Args[0])
	if err != nil {
		return err
	}
	out, err := read.XML(input, p.Args[1])
	if err != nil {
		return cli.Wrap(cli.CodeInvalidDocument, "", err)
	}
	if _, err := stdout.Write(append(out, '\n')); err != nil {
		return cli.Errorf(cli.CodeIO, "", "write the result: %v", err)
	}
	return nil
}

// htmlHelp is `vsdx help html` (chunk C1 writes it from the grammar of
// text.Render; docs/vsdx-contract-draft.md, section 7).
func htmlHelp() string {
	return `HTML in vsdx results and plans (docs/vsdx-contract-draft.md, section 7).

vsdx read writes a shape's text as HTML in html; a plan writes it in
html (text.set, object.add). The input is HTML5: end tags HTML makes
optional may be left out, <br> needs no slash, attribute values need
no quotes, named character references work. Anything the text cannot
store is refused, never dropped: an element or attribute not listed
here, an end tag with nothing to close, an element left open, a
comment. The error's path names the attribute or element.

Elements:
  <p>                      a paragraph
  <ul>, <li>               bulleted paragraphs
  <b> <i> <u> <s>          bold, italic, underline, strike
  <span style>             run formatting
  <br/>                    a line break
  <opaque data-ref data-kind data-text>
                           a field placeholder

Attributes: style on <p>, <li> and <span> (the CSS properties below),
data-char on <li> (the bullet character), and data-ref, data-kind and
data-text on <opaque> (data-kind is "field", data-text the cached
text). Send them back as read; an attribute or style property the
grammar does not have is refused by name.

CSS properties in a <span> style: font-weight, font-style,
text-decoration (the normal/none forms), font-size, color,
font-family.  CSS properties in a paragraph style: text-align,
line-height, margin-top, margin-bottom, margin-left, margin-right,
text-indent.

The HTML shows only the text's own formatting, never what it inherits
from style sheets or its master. A trailing line feed of the stored
text adds no empty paragraph. What the HTML does not show (tab runs,
subscript and superscript position, case, small caps, double
underline, letter spacing, Asian fonts, force-justify) is kept from
the run or paragraph replaced.
`
}
