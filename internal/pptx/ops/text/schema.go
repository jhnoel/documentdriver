package text

import (
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/pptx/ops/edit"
)

// Child order of the DrawingML types the builder writes into, slot by
// slot in schema order; names in one slot form a choice. They are read
// from the edit layer's generated schema table (edit.ChildOrder), the one
// table Handle.Ensure places children by.
var (
	pPrSlots = drawingMLOrder("a:CT_TextParagraphProperties")
	rPrSlots = drawingMLOrder("a:CT_TextCharacterProperties")
)

// drawingMLOrder returns the child order of a DrawingML complex type as
// local names. The types are fixed and tabulated, so a missing table or
// a child from another schema is a programming error, reported when the
// package loads.
func drawingMLOrder(typ string) [][]string {
	slots, ok := edit.ChildOrder(typ)
	if !ok {
		panic(fmt.Sprintf("text: %s is not in the edit schema table", typ))
	}
	out := make([][]string, len(slots))
	for k, s := range slots {
		for _, n := range s {
			local, ok := strings.CutPrefix(n, "a:")
			if !ok {
				panic(fmt.Sprintf("text: %s child %s is not DrawingML", typ, n))
			}
			out[k] = append(out[k], local)
		}
	}
	return out
}

// Attribute order of a:pPr and a:rPr in the schema (ECMA-376
// dml-main.xsd, CT_TextParagraphProperties and
// CT_TextCharacterProperties), used to place an attribute the element
// does not yet have. The edit layer's table holds children only.
var (
	pPrAttrs = []string{"marL", "marR", "lvl", "indent", "algn", "defTabSz", "rtl", "eaLnBrk",
		"fontAlgn", "latinLnBrk", "hangingPunct"}
	rPrAttrs = []string{"kumimoji", "lang", "altLang", "sz", "b", "i", "u", "strike", "kern",
		"cap", "spc", "normalizeH", "baseline", "noProof", "dirty", "err", "smtClean", "smtId", "bmk"}
)

// slotIndex returns the slot of local name in slots, -1 if absent.
func slotIndex(slots [][]string, local string) int {
	for k, s := range slots {
		for _, n := range s {
			if n == local {
				return k
			}
		}
	}
	return -1
}
