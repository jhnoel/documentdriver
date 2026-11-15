package view

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// runProps is a run's w:rPr cascade as typed values: what the style
// chains and direct properties set, the last setter winning, before it is
// resolved to the formatting the projection renders (fmtState). It holds
// what collectRunCSS writes as CSS for the style catalog.
type runProps struct {
	highlight                  string // a palette name, "" for unset
	weightSet, weightNormal    bool
	styleSet, styleNormal      bool
	decoSet, underline, strike bool
	vertSet                    bool
	vert                       vertAlign
	colorSet                   bool
	color                      string // six hex digits
	sizeSet                    bool
	size                       uint32 // half points
	fontSet                    bool
	font                       string // as written, valid by parseFontFamily
}

// collectRunProps adds the properties of the w:rPr p to r, as
// collectRunCSS does to its CSS map.
func collectRunProps(p *tree.Node, r *runProps) {
	for n := range p.ChildrenSeq() {
		name := localName(n)
		val := model.AttrOr(n, "val")
		switch name {
		case "highlight":
			if isHighlight(val) {
				r.highlight = val
			}
		case "b":
			r.weightSet, r.weightNormal = true, offValues[val]
		case "i":
			r.styleSet, r.styleNormal = true, offValues[val]
		case "u", "strike":
			on := val != "0" && val != "none" && val != "false" && val != "off"
			r.decoSet = true
			if name == "u" {
				r.underline = on
			} else {
				r.strike = on
			}
		case "vertAlign":
			r.vertSet = true
			switch val {
			case "superscript":
				r.vert = superscript
			case "subscript":
				r.vert = subscript
			default:
				r.vert = baseline
			}
		case "color":
			if len(val) == 6 && allHex(val) {
				r.colorSet, r.color = true, val
			}
		case "sz":
			if v, ok := model.ParseU32(val); ok {
				r.sizeSet, r.size = true, v
			}
		case "rFonts":
			v, ok := model.Attr(n, "ascii")
			if !ok {
				v, ok = model.Attr(n, "hAnsi")
			}
			if ok {
				if _, valid := parseFontFamily(v); valid {
					r.fontSet, r.font = true, v
				}
			}
		}
	}
}

// cssValue is what declarations makes of a declaration's value v: the
// trimmed value, whether it is kept ("unset" is dropped) and whether it
// is unsafe, which fails the whole declaration list.
func cssValue(v string) (value string, keep, unsafe bool) {
	v = rustTrim(v)
	if v == "" || strings.ContainsFunc(v, func(r rune) bool {
		return isControl(r) || r == '<' || r == '>' || r == '{' || r == '}'
	}) || strings.Contains(ascii.Lower(v), "url(") || strings.Contains(v, "!") {
		return "", false, true
	}
	return v, !strings.EqualFold(v, "unset"), false
}

// format resolves the cascade to the formatting the projection renders.
// It is the CSS round trip the projection is defined by (the cascade
// written as CSS, read back by applyInlineCSS) on typed values: a
// property it cannot carry gives no formatting at all.
func (r *runProps) format() fmtState {
	var f fmtState
	f.highlight = r.highlight
	if r.weightSet {
		f.resets |= resetBold
		f.bold = !r.weightNormal
	}
	if r.styleSet {
		f.resets |= resetItalic
		f.italic = !r.styleNormal
	}
	if r.decoSet {
		f.resets |= resetUnderline | resetStrike
		f.underline, f.strike = r.underline, r.strike
	}
	if r.vertSet {
		f.resets |= resetBaseline
		f.vert = r.vert
	}
	if r.colorSet {
		v, err := strconv.ParseUint(r.color, 16, 32)
		if err != nil {
			return fmtState{}
		}
		f.color, f.hasColor = uint32(v), true
	}
	if r.sizeSet {
		// font-size:<size/2>pt reads back only between 0.5pt and 100000pt.
		if r.size < 1 || r.size > 200000 {
			return fmtState{}
		}
		f.size, f.hasSize = r.size, true
	}
	if r.fontSet {
		v, keep, unsafe := cssValue(r.font)
		if unsafe {
			return fmtState{}
		}
		if keep {
			font, ok := parseFontFamily(v)
			if !ok {
				return fmtState{}
			}
			f.font, f.hasFont = font, true
		}
	}
	return f
}

// hasInlineCSS reports whether inlineCSS(f) is not empty.
func hasInlineCSS(f fmtState) bool {
	return f.resets&resetBold != 0 && !f.bold ||
		f.resets&resetItalic != 0 && !f.italic ||
		f.resets&(resetUnderline|resetStrike) != 0 && !f.underline && !f.strike ||
		f.resets&resetBaseline != 0 && f.vert == baseline ||
		f.highlight != "" || f.hasColor || f.hasSize || f.hasFont
}

// inheritFmt is the run formatting a paragraph carries for its bare text:
// the inheritable properties (color, font family, size, style and
// weight) of its runs' inline CSS, typed.
type inheritFmt struct {
	weightNormal, styleNormal  bool
	hasColor, hasSize, hasFont bool
	color, size                uint32
	font                       string // as declarations reads it back
}

// inheritable is the inheritable part of the inline CSS of a run with
// formatting f, without color when the paragraph has a hyperlink: what
// parsing inlineCSS(f) with declarations and keeping those properties
// gives. A font name that does not read back fails the whole list, as
// in declarations.
func inheritable(f fmtState, hasLink bool) inheritFmt {
	var in inheritFmt
	if f.hasFont {
		v, keep, unsafe := cssValue(f.font)
		if unsafe {
			return inheritFmt{}
		}
		in.font, in.hasFont = v, keep
	}
	in.weightNormal = f.resets&resetBold != 0 && !f.bold
	in.styleNormal = f.resets&resetItalic != 0 && !f.italic
	in.color, in.hasColor = f.color, f.hasColor && !hasLink
	in.size, in.hasSize = f.size, f.hasSize
	return in
}

// intersect keeps the properties in and o share with equal values.
func (in *inheritFmt) intersect(o inheritFmt) {
	in.weightNormal = in.weightNormal && o.weightNormal
	in.styleNormal = in.styleNormal && o.styleNormal
	in.hasColor = in.hasColor && o.hasColor && in.color == o.color
	in.hasSize = in.hasSize && o.hasSize && in.size == o.size
	in.hasFont = in.hasFont && o.hasFont && in.font == o.font
}

// declare writes the properties into the declaration map m.
func (in inheritFmt) declare(m map[string]string) {
	if in.hasColor {
		m["color"] = "#" + hex6(in.color)
	}
	if in.hasFont {
		m["font-family"] = in.font
	}
	if in.hasSize {
		m["font-size"] = formatFloat(float64(in.size)/2) + "pt"
	}
	if in.styleNormal {
		m["font-style"] = "normal"
	}
	if in.weightNormal {
		m["font-weight"] = "normal"
	}
}
