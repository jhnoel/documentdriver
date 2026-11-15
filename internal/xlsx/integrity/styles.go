package integrity

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// styleCounts is what styles.xml says the sheets may index.
type styleCounts struct {
	cellXfs int // -1 unknown
}

// nonEmpty are the style lists Excel needs at least one entry in (dxfs may
// be empty: Excel writes it so).
var nonEmpty = map[string]bool{"fonts": true, "fills": true, "borders": true, "cellStyleXfs": true, "cellXfs": true, "numFmts": true}

// checkStyles reads styles.xml (a small part, as a tree) and checks the
// lists the gate cares about: the cellXfs count, the ids each xf holds
// against the lists they index, and the counts and emptiness of lists.
func (c *checker) checkStyles(name string) styleCounts {
	out := styleCounts{cellXfs: -1}
	doc := c.tree(name)
	if doc == nil {
		return out
	}
	root := doc.Root()
	if root == nil || !root.Is(sml.NSMain, "styleSheet") {
		c.f.add(StylesMissing, name, "root", "root is not styleSheet")
		return out
	}
	list := func(local string) (*tree.Node, int) {
		n := root.Child(sml.NSMain, local)
		if n == nil {
			return nil, -1
		}
		k := 0
		for range n.ElementsSeq() {
			k++
		}
		if v, ok := n.Attr("count"); ok {
			if cnt, ok := sml.ParseIndex(v); !ok || cnt != k {
				c.f.add(StylesCount, name, local, local+" count "+v+", "+strconv.Itoa(k)+" children")
			}
		}
		if k == 0 && nonEmpty[local] {
			c.f.add(StylesEmpty, name, local, local+" has no children")
		}
		return n, k
	}
	_, nFonts := list("fonts")
	_, nFills := list("fills")
	_, nBorders := list("borders")
	_, nStyleXfs := list("cellStyleXfs")
	xfs, nXfs := list("cellXfs")
	numFmts, _ := list("numFmts")
	list("cellStyles")
	list("dxfs")
	if xfs == nil {
		c.f.add(StylesMissing, name, "cellXfs", "no cellXfs")
		return out
	}
	out.cellXfs = nXfs
	custom := map[int]bool{}
	if numFmts != nil {
		for e := range numFmts.ElementsSeq() {
			if id, ok := sml.ParseIndex(attr(e, "numFmtId")); ok {
				custom[id] = true
			}
		}
	}
	for xf := range xfs.ElementsSeq() {
		ref := func(attrName string, limit int, key string) {
			v, ok := xf.Attr(attrName)
			if !ok {
				return
			}
			id, ok := sml.ParseIndex(v)
			if !ok || limit >= 0 && id >= limit {
				c.f.add(StylesRange, name, key, "cellXfs xf has "+attrName+"="+v+", the list has "+strconv.Itoa(limit))
			}
		}
		ref("fontId", nFonts, "fontId")
		ref("fillId", nFills, "fillId")
		ref("borderId", nBorders, "borderId")
		ref("xfId", nStyleXfs, "xfId")
		if v, ok := xf.Attr("numFmtId"); ok {
			id, ok := sml.ParseIndex(v)
			if !ok || id >= 164 && !custom[id] {
				c.f.add(StylesRange, name, "numFmtId", "cellXfs xf has numFmtId="+v+" with no numFmt")
			}
		}
	}
	return out
}
