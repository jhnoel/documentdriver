package model

import "github.com/jhnoel/documentdriver/internal/ooxml/tree"

// PropertyBag returns n's property container name (w:pPr, w:rPr, ...).
// With original, a tracked property change's snapshot (w:pPrChange/w:pPr)
// stands in for it, and a change without a snapshot gives nil.
func PropertyBag(n *tree.Node, name string, original bool) *tree.Node {
	p := ChildW(n, name)
	if p == nil {
		return nil
	}
	if original {
		if change := ChildW(p, name+"Change"); change != nil {
			return ChildW(change, name)
		}
	}
	return p
}

// StylesPartName is the styles part beside the main document part.
func (p *Package) StylesPartName() string { return p.SiblingPart("styles.xml") }

// SettingsPartName is the settings part beside the main document part
// ("word/settings.xml" for "word/document.xml").
func (p *Package) SettingsPartName() string { return p.SiblingPart("settings.xml") }
