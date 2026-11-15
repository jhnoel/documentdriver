package edit

import (
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// Vocab is the namespace families the docx side writes: WordprocessingML
// (tabulated), relationships, the w14 extension, and Office Math (whose
// equations the WordprocessingML tables place).
var Vocab = core.Vocab{WML, core.REL, W14, M}

// Format is what the edit core needs from WordprocessingML: its
// namespaces, the units the verifier compares, the complex type of an
// element (where Ensure and the placed inserts start their walk) and the
// units a path must not step into (another unit: a handle on a paragraph
// cannot reach into a table nested in a text box, or a section).
var Format = core.Format{
	Vocab: Vocab,
	Units: units{},
	Type:  Type,
	Bound: func(e *tree.Node) bool { return IsUnit(e) },
	// Run properties: EG_RPrBase is a repeated choice in both the
	// transitional and the strict schema (ECMA-376 5th edition), so
	// neither fixes their order; new ones go in declaration order.
	PropertyChoices: map[string]bool{
		"w:CT_RPr": true, "w:CT_ParaRPr": true, "w:CT_RPrOriginal": true, "w:CT_ParaRPrOriginal": true,
	},
}
