package build

import (
	"strconv"

	"github.com/jhnoel/documentdriver/internal/docx/html"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// DirectFormat is the formatting the HTML can carry that run properties
// rpr set directly (the inverse of runPropertiesOf): bold, italic,
// underline, strike, vertical alignment, highlight, colour, size and the
// ascii font. A property switched off explicitly (w:b w:val="0") is a
// reset. Properties the HTML cannot carry are not part of it.
func DirectFormat(rpr *tree.Node) html.Fmt {
	var f html.Fmt
	if rpr == nil {
		return f
	}
	toggle := func(local string, bit uint8, on *bool) {
		c := model.ChildW(rpr, local)
		if c == nil {
			return
		}
		switch v, _ := model.Attr(c, "val"); v {
		case "0", "false", "off":
			f.Resets |= bit
		default:
			*on = true
		}
	}
	toggle("b", 1, &f.Bold)
	toggle("i", 2, &f.Italic)
	toggle("strike", 8, &f.Strike)
	if u := model.ChildW(rpr, "u"); u != nil {
		if v := model.AttrOr(u, "val"); v == "none" {
			f.Resets |= 4
		} else if v == "single" || v == "" {
			f.Underline = true
		}
	}
	if v := model.ChildW(rpr, "vertAlign"); v != nil {
		switch model.AttrOr(v, "val") {
		case "superscript":
			f.VertAlign = html.Some(html.Superscript)
		case "subscript":
			f.VertAlign = html.Some(html.Subscript)
		case "baseline":
			f.Resets |= 16
		}
	}
	if h := model.ChildW(rpr, "highlight"); h != nil {
		if c, err := html.ParseHighlightColor(model.AttrOr(h, "val")); err == nil && model.AttrOr(h, "val") != "none" {
			f.Highlight = html.Some(c)
		}
	}
	if c := model.ChildW(rpr, "color"); c != nil {
		if v := model.AttrOr(c, "val"); len(v) == 6 {
			if n, err := strconv.ParseUint(v, 16, 32); err == nil {
				f.Color = html.Some(uint32(n))
			}
		}
	}
	if s := model.ChildW(rpr, "sz"); s != nil {
		if n, ok := model.ParseU32(model.AttrOr(s, "val")); ok {
			f.FontSize = html.Some(n)
		}
	}
	if r := model.ChildW(rpr, "rFonts"); r != nil {
		if v, ok := model.Attr(r, "ascii"); ok {
			if ff, err := html.ParseFontFamily(v); err == nil {
				f.FontFamily = html.Some(ff)
			}
		}
	}
	return f
}

// SharedFormat is the direct formatting every text run of paragraph p
// shares (docxdriver's with_shared_run_format, from the runs' own
// properties rather than through the projection): read shows it on the
// paragraph and leaves the text bare, so bare text in a replacement
// keeps it. Runs inside nested stories do not count; a paragraph without
// text runs shares nothing.
func SharedFormat(p *tree.Node) html.Fmt {
	var common html.Fmt
	first := true
	p.Walk(func(n *tree.Node) bool {
		if n != p && model.IsW(n, "p") {
			return false
		}
		if !model.IsW(n, "r") {
			return true
		}
		if model.ChildW(n, "t") == nil {
			return false
		}
		f := DirectFormat(model.ChildW(n, "rPr"))
		if first {
			common, first = f, false
			return false
		}
		common = intersect(common, f)
		return false
	})
	return common
}

// intersect returns the format common to both a and b.
func intersect(a, b html.Fmt) html.Fmt {
	var f html.Fmt
	f.Resets = a.Resets & b.Resets
	f.Bold = a.Bold && b.Bold
	f.Italic = a.Italic && b.Italic
	f.Underline = a.Underline && b.Underline
	f.Strike = a.Strike && b.Strike
	if a.VertAlign == b.VertAlign {
		f.VertAlign = a.VertAlign
	}
	if a.Highlight == b.Highlight {
		f.Highlight = a.Highlight
	}
	if a.Color == b.Color {
		f.Color = a.Color
	}
	if a.FontSize == b.FontSize {
		f.FontSize = a.FontSize
	}
	if a.FontFamily == b.FontFamily {
		f.FontFamily = a.FontFamily
	}
	return f
}

// WithBase gives every character atom the formatting of base where its
// own leaves a property unset (docxdriver's with_shared_run_format: a
// piece's own formatting, an explicit reset included, wins).
func WithBase(atoms []html.Atom, base html.Fmt) []html.Atom {
	if base == (html.Fmt{}) {
		return atoms
	}
	out := make([]html.Atom, len(atoms))
	for i, a := range atoms {
		if a.Kind == html.AtomChar {
			f := a.Fmt
			if !f.Color.OK {
				f.Color = base.Color
			}
			if !f.FontSize.OK {
				f.FontSize = base.FontSize
			}
			if !f.FontFamily.OK {
				f.FontFamily = base.FontFamily
			}
			if !f.Highlight.OK {
				f.Highlight = base.Highlight
			}
			if !f.VertAlign.OK && f.Resets&16 == 0 {
				f.VertAlign = base.VertAlign
			}
			for _, t := range []struct {
				bit       uint8
				own       *bool
				inherited bool
			}{{1, &f.Bold, base.Bold}, {2, &f.Italic, base.Italic}, {4, &f.Underline, base.Underline}, {8, &f.Strike, base.Strike}} {
				if !*t.own && f.Resets&t.bit == 0 {
					*t.own = t.inherited
				}
			}
			a.Fmt = f
		}
		out[i] = a
	}
	return out
}
