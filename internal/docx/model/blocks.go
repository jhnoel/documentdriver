package model

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// FindSectPr returns the w:sectPr a body block (w:p or w:tbl) carries:
// a paragraph's w:pPr/w:sectPr, else the first w:sectPr anywhere below
// the block, else nil.
func FindSectPr(block *tree.Node) *tree.Node {
	if IsW(block, "p") {
		if ppr := ChildW(block, "pPr"); ppr != nil {
			if s := ChildW(ppr, "sectPr"); s != nil {
				return s
			}
		}
	}
	var found *tree.Node
	block.Walk(func(n *tree.Node) bool {
		if found != nil {
			return false
		}
		if n != block && IsW(n, "sectPr") {
			found = n
			return false
		}
		return true
	})
	return found
}

// InsidePPr reports whether n sits in a w:pPr below paragraph p (a
// paragraph-mark or property marker, not a content wrapper); false once
// the walk up from n reaches p or the root.
func InsidePPr(n, p *tree.Node) bool {
	for a := n.Parent(); a != nil; a = a.Parent() {
		if a == p {
			return false
		}
		if IsW(a, "pPr") {
			return true
		}
	}
	return false
}

// AssetURL is the stable URL of an image part's bytes:
// "assets/<sha256 hex of b>.<ImageExt(part)>".
func AssetURL(part string, b []byte) string {
	s := sha256.Sum256(b)
	return "assets/" + hex.EncodeToString(s[:]) + "." + ImageExt(part)
}
