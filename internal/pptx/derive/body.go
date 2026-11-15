package derive

// bodyDefaults are the values ECMA-376 (21.1.2.1.1 bodyPr) states are
// implied when the attribute is omitted everywhere. Booleans are written
// "0" (false).
var bodyDefaults = map[string]string{
	"lIns": "91440", "tIns": "45720", "rIns": "91440", "bIns": "45720",
	"anchor": "t", "wrap": "square",
	"vert": "horz", "rot": "0", "numCol": "1", "spcCol": "0",
	"rtlCol": "0", "anchorCtr": "0", "upright": "0",
	"vertOverflow": "overflow", "horzOverflow": "overflow",
	"compatLnSpc": "0", "spcFirstLastPara": "0", "fromWordArt": "0", "forceAA": "0",
}

// BodyAttrs are the a:bodyPr attributes the views show and the body codec
// writes, in the order the views show them: the insets, anchor and wrap,
// then the other attributes with weight in the corpus.
var BodyAttrs = []string{
	"anchor", "wrap", "lIns", "tIns", "rIns", "bIns",
	"vert", "rot", "numCol", "spcCol", "rtlCol", "anchorCtr", "upright",
	"vertOverflow", "horzOverflow", "compatLnSpc", "spcFirstLastPara", "fromWordArt", "forceAA",
}

// Autofit modes as the views show them, by element.
var autofitModes = map[string]string{"noAutofit": "none", "spAutoFit": "shape", "normAutofit": "shrink"}

// AutofitElems maps the views' autofit modes to their elements.
var AutofitElems = map[string]string{"none": "noAutofit", "shape": "spAutoFit", "shrink": "normAutofit"}

// ReadBodyAttr reads attribute name of a text body's a:bodyPr through an
// object's placeholder chain (each link an object element, the object
// first): the first link whose p:txBody/a:bodyPr has it, else the value
// the specification implies (origin spec) for the attributes in
// BodyAttrs. It is the read half the read model and the body codec share.
func ReadBodyAttr(chain []Link, name string) (string, Origin, bool) {
	for _, l := range chain {
		bp := Child(Child(l.Elem, PML, "txBody"), DML, "bodyPr")
		if bp == nil {
			continue
		}
		if v, ok := bp.Attr(name); ok {
			return v, l.Origin, true
		}
	}
	if v, ok := bodyDefaults[name]; ok {
		return v, FromSpecDflt, true
	}
	return "", "", false
}

// Autofit is a text body's autofit choice: the mode ("none" for
// a:noAutofit, "shape" for a:spAutoFit, "shrink" for a:normAutofit) and,
// for shrink, the stored @fontScale and @lnSpcReduction ("" when not
// written), with the origin of the link that holds the choice.
type Autofit struct {
	Mode                      string
	FontScale, LnSpcReduction string
	Origin                    Origin
}

// ReadAutofit reads a text body's autofit choice through the placeholder
// chain: the first link whose a:bodyPr holds one of the three elements,
// else a:noAutofit, which ECMA-376 states is implied when none is given
// (origin spec). The scale PowerPoint stored with a:normAutofit is
// reported as written; it is never computed.
func ReadAutofit(chain []Link) Autofit {
	for _, l := range chain {
		bp := Child(Child(l.Elem, PML, "txBody"), DML, "bodyPr")
		if bp == nil {
			continue
		}
		for c := range bp.ElementsSeq() {
			mode, ok := autofitModes[c.Local()]
			if !ok || !In(c, DML) {
				continue
			}
			a := Autofit{Mode: mode, Origin: l.Origin}
			if mode == "shrink" {
				a.FontScale, _ = c.Attr("fontScale")
				a.LnSpcReduction, _ = c.Attr("lnSpcReduction")
			}
			return a
		}
	}
	return Autofit{Mode: "none", Origin: FromSpecDflt}
}
