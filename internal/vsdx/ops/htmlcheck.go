package ops

import (
	"regexp"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
)

// CheckHTML checks every html field of p (text.set's html, and
// object.add's when it is set) against the vsdx text grammar, with no
// document open: it is the plan's document-independent html pass, so a
// plan whose html is bad is refused before the drawing is read. It
// returns every fault, in op order, each an invalid_operation at
// ops[n].html with the element or attribute at fault on the path:
// ops[n].html<a> for an element, ops[n].html@bogus for an attribute,
// ops[n].html@style with the CSS property in the message for a style
// property, and ops[n].html@data-color for a data-color. It returns
// none when every html field is in the grammar. Chunk CC5.
func CheckHTML(p *Plan) cli.Errors {
	if p == nil {
		return nil
	}
	var errs cli.Errors
	for i, s := range p.Ops {
		html, ok := htmlOf(s)
		if !ok {
			continue
		}
		if msg, field, bad := htmlFault(html); bad {
			errs = append(errs, cli.Errorf(cli.CodeInvalidOperation, cli.Join(cli.Index("ops", i), field), "%s", msg))
		}
	}
	return errs
}

// htmlOf returns the html field of s and whether s carries one:
// text.set's html (required) and object.add's html when set.
func htmlOf(s Spec) (string, bool) {
	switch v := s.(type) {
	case *TextSetSpec:
		return v.HTML, true
	case *ObjectAddSpec:
		if v.HTML == nil {
			return "", false
		}
		return *v.HTML, true
	}
	return "", false
}

// htmlFault checks html against the vsdx text grammar and reports the
// first fault: its message (text.ParseHTML's error without the "text: "
// prefix) and its path field, "html" with the element or attribute at
// fault after it, or false when html is in the grammar.
func htmlFault(html string) (string, string, bool) {
	if _, _, err := text.ParseHTML(html); err != nil {
		msg := strings.TrimPrefix(err.Error(), "text: ")
		return msg, "html" + htmlFieldOf(msg), true
	}
	return "", "", false
}

// htmlFieldOf returns the path suffix naming what a grammar error is
// about, from its message: "<a>" for an element, "@bogus" for an
// attribute, "@data-color" for a data attribute the grammar reads by
// name (a data-color, data-ref, data-kind, data-char), and "@style"
// for a CSS style property, whose name stays in the message. Anything
// else has no suffix: bare "html".
func htmlFieldOf(msg string) string {
	if m := htmlAttr.FindStringSubmatch(msg); m != nil {
		return "@" + m[1]
	}
	if m := htmlElement.FindStringSubmatch(msg); m != nil {
		return "<" + m[1] + ">"
	}
	if m := htmlDataAttr.FindStringSubmatch(msg); m != nil {
		return "@" + m[0]
	}
	if strings.Contains(msg, "style property") || strings.Contains(msg, " style: ") || hasCSSPropertyLead(msg) {
		return "@style"
	}
	return ""
}

// hasCSSPropertyLead reports whether msg opens with a CSS property the
// grammar reads, as a style value error words it ("color:red: ...",
// "font-weight:normal inside <b>").
func hasCSSPropertyLead(msg string) bool {
	for _, prop := range cssPropertyLeads {
		if rest, ok := strings.CutPrefix(msg, prop); ok {
			if strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, " ") {
				return true
			}
		}
	}
	return false
}

// cssPropertyLeads are the CSS properties of the text grammar, as the
// style value errors name them.
var cssPropertyLeads = []string{
	"color", "font-size", "font-weight", "font-style", "font-family", "text-decoration",
	"text-align", "line-height", "margin-top", "margin-bottom", "margin-left", "margin-right", "text-indent",
}

// htmlAttr, htmlElement and htmlDataAttr find what a grammar error is
// about in its message, as internal/vsdx/text words it.
var (
	htmlAttr     = regexp.MustCompile(`attribute ([a-z][a-z0-9-]*)`)
	htmlElement  = regexp.MustCompile(`element <([a-z][a-z0-9-]*)>`)
	htmlDataAttr = regexp.MustCompile(`data-[a-z0-9-]+`)
)
