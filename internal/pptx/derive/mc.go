package derive

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// SupportedNamespaces are the namespaces whose mc:Choice branches are
// projected: the set taken to be understood by PowerPoint 2016 (16.0) as
// shipped. It is a decision recorded in docs/design.md, not a verified
// fact: PowerPoint is not available to test it. Namespaces introduced
// after 2016 (for example 3D models, 2017) are not listed, so their
// Fallback is projected.
var SupportedNamespaces = map[string]string{
	"http://schemas.microsoft.com/office/drawing/2010/main":        "a14",
	"http://schemas.microsoft.com/office/powerpoint/2010/main":     "p14",
	"http://schemas.microsoft.com/office/drawing/2012/main":        "a15",
	"http://schemas.microsoft.com/office/powerpoint/2012/main":     "p15",
	"http://schemas.microsoft.com/office/drawing/2014/main":        "a16",
	"http://schemas.microsoft.com/office/drawing/2007/8/2/chart":   "c14",
	"http://schemas.microsoft.com/office/drawing/2014/chartex":     "cx",
	"http://schemas.microsoft.com/office/drawing/2015/9/8/chartex": "cx1",
	"urn:schemas-microsoft-com:vml":                                "v",
}

// Branch is the projected branch of an mc:AlternateContent element.
type Branch struct {
	// Node is the chosen mc:Choice or mc:Fallback, nil if neither applies.
	Node *tree.Node
	// Fallback is true when the Fallback was chosen.
	Fallback bool
	// Requires is the chosen Choice's Requires attribute.
	Requires string
	// Unsupported lists the namespaces of skipped Choices.
	Unsupported []string
}

// SelectBranch picks the branch of mc:AlternateContent ac that a consumer
// supporting SupportedNamespaces reads: the first Choice all of whose
// Requires prefixes resolve to supported namespaces, else the Fallback.
func SelectBranch(ac *tree.Node) Branch {
	var b Branch
	for c := range ac.ElementsSeq() {
		switch {
		case Is(c, MC, "Choice"):
			req, _ := c.Attr("Requires")
			ok := req != ""
			for _, p := range strings.Fields(req) {
				uri, found := c.LookupPrefix(p)
				if !found {
					ok = false
					b.Unsupported = append(b.Unsupported, p+"=(undeclared)")
					continue
				}
				if _, sup := SupportedNamespaces[uri]; !sup {
					ok = false
					b.Unsupported = append(b.Unsupported, uri)
				}
			}
			if ok {
				b.Node, b.Requires = c, req
				return b
			}
		case Is(c, MC, "Fallback"):
			b.Node, b.Fallback = c, true
			return b
		}
	}
	return b
}
