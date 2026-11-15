package edit

import (
	"github.com/jhnoel/documentdriver/internal/docx/model"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Namespace families WordprocessingML edits use.
var (
	// WML is the WordprocessingML main namespace (transitional, then
	// strict), tabulated as "w": the read model's family, so read and
	// edit agree on what is WordprocessingML.
	WML = model.WordML
	// W14 is the Office 2010 wordprocessing extension namespace of
	// w14:paraId and w14:textId.
	W14 = core.NS{URIs: []string{"http://schemas.microsoft.com/office/word/2010/wordml"}, Prefix: "w14"}
	// M is Office Math (transitional, then strict), tabulated as "m": the
	// WordprocessingML tables list m:oMath and m:oMathPara among a
	// paragraph's content, so an equation is placed like a run.
	M = core.NS{URIs: []string{"http://schemas.openxmlformats.org/officeDocument/2006/math", "http://purl.oclc.org/ooxml/officeDocument/math"}, Prefix: "m", Table: "m"}
	// MC is the markup-compatibility namespace of mc:Ignorable.
	MC = core.NS{URIs: []string{"http://schemas.openxmlformats.org/markup-compatibility/2006"}, Prefix: "mc"}
)

// W returns the WordprocessingML element name local.
func W(local string) core.Name { return WML.Name(local) }

// is reports whether e is the WordprocessingML element local.
func is(e *tree.Node, local string) bool { return WML.Is(e, local) }

// isRoot reports whether e is a part's root element.
func isRoot(e *tree.Node) bool {
	return e != nil && e.Kind() == tree.ElementNode && e.Parent() != nil && e.Parent().Kind() == tree.DocumentNode
}
