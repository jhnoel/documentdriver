package splice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// Writer builds replacement content for a Patch: elements, attributes
// and text, escaped as it goes, so new bytes come from one escaper.
// Names are checked as XML qualified names and values as XML
// characters; the first fault is kept and returned by String. Whether
// each prefix is bound where the content lands is for Check, which
// knows the scope.
type Writer struct {
	b    strings.Builder
	open []string // names of the open elements
	// inTag is set while the newest element's start tag is unclosed
	// (attributes may follow).
	inTag bool
	err   error
}

// Start opens element qname.
func (w *Writer) Start(qname string) {
	if w.err != nil {
		return
	}
	if !xmlscan.ValidQName(qname) {
		w.err = fmt.Errorf("splice: invalid element name %q", qname)
		return
	}
	w.closeTag()
	w.b.WriteByte('<')
	w.b.WriteString(qname)
	w.open = append(w.open, qname)
	w.inTag = true
}

// Attr adds attribute qname="value" to the element just opened; it must
// come before the element's content.
func (w *Writer) Attr(qname, value string) {
	if w.err != nil {
		return
	}
	switch {
	case !w.inTag:
		w.err = fmt.Errorf("splice: attribute %s outside a start tag", qname)
	case !xmlscan.ValidQName(qname):
		w.err = fmt.Errorf("splice: invalid attribute name %q", qname)
	case !xmlscan.ValidChars(value):
		w.err = fmt.Errorf("splice: attribute %s: value is not XML characters", qname)
	}
	if w.err != nil {
		return
	}
	w.b.WriteByte(' ')
	w.b.WriteString(qname)
	w.b.WriteString(`="`)
	w.b.WriteString(xmlscan.EscapeAttr(value, '"'))
	w.b.WriteByte('"')
}

// Text adds character data.
func (w *Writer) Text(s string) {
	if w.err != nil || s == "" {
		return
	}
	if !xmlscan.ValidChars(s) {
		w.err = errors.New("splice: text is not XML characters")
		return
	}
	w.closeTag()
	w.b.WriteString(xmlscan.EscapeText(s))
}

// End closes the innermost open element: as an empty-element tag when it
// has no content.
func (w *Writer) End() {
	if w.err != nil {
		return
	}
	if len(w.open) == 0 {
		w.err = errors.New("splice: End with no open element")
		return
	}
	name := w.open[len(w.open)-1]
	w.open = w.open[:len(w.open)-1]
	if w.inTag {
		w.b.WriteString("/>")
		w.inTag = false
		return
	}
	w.b.WriteString("</")
	w.b.WriteString(name)
	w.b.WriteByte('>')
}

// closeTag ends the pending start tag if attributes may still follow.
func (w *Writer) closeTag() {
	if w.inTag {
		w.b.WriteByte('>')
		w.inTag = false
	}
}

// String returns the content written, or the first fault: a bad name or
// character, or an element left open.
func (w *Writer) String() (string, error) {
	if w.err != nil {
		return "", w.err
	}
	if len(w.open) > 0 {
		return "", fmt.Errorf("splice: element %s not closed", w.open[len(w.open)-1])
	}
	return w.b.String(), nil
}

// Reset empties the writer for reuse.
func (w *Writer) Reset() {
	w.b.Reset()
	w.open = w.open[:0]
	w.inTag = false
	w.err = nil
}
