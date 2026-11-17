package model

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// formats builds the Character and Paragraph formats the HTML shows
// for the shape's text, by the rule of text.Formats: only the cells of
// owner (the sheet whose Text is shown), Style bits split into Bold,
// Italic and Underline, Size inches to points (geom.FormatNum), Color
// through doc.Document.Color, Font the stored face name ("Themed"
// omitted), HorzAlign 0 to 3, Bullet, BulletStr, IndLeft, IndRight,
// IndFirst, SpBefore, SpAfter, SpLine. Chunk M5.
func formats(r *inherit.Resolver, owner *inherit.Shape) text.Formats {
	if r == nil || owner == nil {
		return text.Formats{}
	}
	below := *owner
	below.Sheet = nil
	var out text.Formats
	charRows := r.Rows(owner, vsd.SectionCharacter)
	for _, row := range charRows {
		if row.Origin != inherit.Local || row.Key.RowIX < 0 {
			continue
		}
		if out.Char == nil {
			out.Char = make(map[int]text.CharFormat, len(charRows))
		}
		out.Char[row.Key.RowIX] = formatsChar(r, owner, &below, row.Key)
	}
	paraRows := r.Rows(owner, vsd.SectionParagraph)
	for _, row := range paraRows {
		if row.Origin != inherit.Local || row.Key.RowIX < 0 {
			continue
		}
		if out.Para == nil {
			out.Para = make(map[int]text.ParaFormat, len(paraRows))
		}
		out.Para[row.Key.RowIX] = formatsPara(r, owner, &below, row.Key)
	}
	return out
}

// formatsChar builds the character format of one owner row: Style bits
// against the inherited Style, Strikethru, Size, Color and Font, each
// shown only when the owner stores it and it differs from the
// inherited value. A stored value that does not parse, a Themed
// colour or font, and a font name holding ';' show nothing.
func formatsChar(r *inherit.Resolver, owner, below *inherit.Shape, row sheet.Key) text.CharFormat {
	var f text.CharFormat
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("Style")); ok {
		if ownNum, ok := vsd.Number(own.V); ok {
			inhNum := 0.0
			if inhOK {
				inhNum, _ = vsd.Number(inh.V)
			}
			f.Bold = formatsOnOff(int(ownNum)&1 != 0, int(inhNum)&1 != 0)
			f.Italic = formatsOnOff(int(ownNum)&2 != 0, int(inhNum)&2 != 0)
			f.Underline = formatsOnOff(int(ownNum)&4 != 0, int(inhNum)&4 != 0)
		}
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("Strikethru")); ok {
		if ownOn, ok := formatsBool(own.V); ok {
			f.Strike = formatsOnOff(ownOn, inhOK && formatsIsOn(inh.V))
		}
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("Size")); ok {
		f.Size = formatsShown(own, inh, inhOK, formatsPoints)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("Color")); ok {
		f.Color = formatsColor(r, own, inh, inhOK)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("Font")); ok {
		f.Font = formatsFont(own, inh, inhOK)
	}
	return f
}

// formatsPara builds the paragraph format of one owner row: HorzAlign,
// Bullet and BulletStr, the indents and spacings in points, and
// SpLine, shown only when the owner stores the cell and it differs
// from the inherited value.
func formatsPara(r *inherit.Resolver, owner, below *inherit.Shape, row sheet.Key) text.ParaFormat {
	var f text.ParaFormat
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("HorzAlign")); ok {
		f.Align = formatsAlign(own, inh, inhOK)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("Bullet")); ok {
		f.Bullet = formatsIsOn(own.V) && !(inhOK && formatsIsOn(inh.V) == formatsIsOn(own.V))
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("BulletStr")); ok {
		if !inhOK || own.V != inh.V {
			f.BulletChar = own.V
		}
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("IndLeft")); ok {
		f.MarginLeft = formatsShown(own, inh, inhOK, formatsLength)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("IndRight")); ok {
		f.MarginRight = formatsShown(own, inh, inhOK, formatsLength)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("IndFirst")); ok {
		f.Indent = formatsShown(own, inh, inhOK, formatsLength)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("SpBefore")); ok {
		f.SpaceBefore = formatsShown(own, inh, inhOK, formatsLength)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("SpAfter")); ok {
		f.SpaceAfter = formatsShown(own, inh, inhOK, formatsLength)
	}
	if own, inh, ok, inhOK := formatsCells(r, owner, below, row.WithCell("SpLine")); ok {
		f.LineHeight = formatsShown(own, inh, inhOK, formatsLineHeight)
	}
	return f
}

// formatsCells returns the cell owner stores at key and the inherited
// cell below owner (the master shape's or the style chain's),
// reporting whether each is present. A cell the owner does not store,
// or stores only as inherited (F="Inh"), or holds only through the
// master or a style, is not present: only a local cell is the owner's
// formatting.
func formatsCells(r *inherit.Resolver, owner, below *inherit.Shape, key sheet.Key) (sheet.Cell, sheet.Cell, bool, bool) {
	own, origin, ok := r.Cell(owner, key)
	if !ok || origin != inherit.Local {
		return sheet.Cell{}, sheet.Cell{}, false, false
	}
	inh, _, inhOK := r.Cell(below, key)
	return own, inh, true, inhOK
}

// formatsOnOff returns "on" when own is set and inherited is not,
// "off" when own is clear and inherited is set, and "" when they
// agree.
func formatsOnOff(own, inh bool) string {
	switch {
	case own && !inh:
		return "on"
	case !own && inh:
		return "off"
	}
	return ""
}

// formatsIsOn reports whether a stored boolean value is set: a number
// other than zero, or a boolean spelling vsd.Bool accepts.
func formatsIsOn(v string) bool {
	on, ok := formatsBool(v)
	return ok && on
}

// formatsBool returns a stored boolean value: a number other than
// zero, or a boolean spelling vsd.Bool accepts, reporting false when
// v is neither.
func formatsBool(v string) (bool, bool) {
	if n, ok := vsd.Number(v); ok {
		return n != 0, true
	}
	return vsd.Bool(v)
}

// formatsInches returns own's value in inches, reporting false when
// own is not a number or stores the same number as the inherited
// cell.
func formatsInches(own, inh sheet.Cell, inhOK bool) (float64, bool) {
	ownNum, ok := vsd.Number(own.V)
	if !ok {
		return 0, false
	}
	if inhOK {
		if inhNum, ok := vsd.Number(inh.V); ok && inhNum == ownNum {
			return 0, false
		}
	}
	return ownNum, true
}

// formatsShown returns own's value as show spells it, or "" when own
// is not a number or show spells the inherited value the same way (so
// Visio's two spellings of 12 pt, 0.16666666666666666 and
// 0.1666666666666667, count as the same stored value).
func formatsShown(own, inh sheet.Cell, inhOK bool, show func(float64) string) string {
	ownNum, ok := vsd.Number(own.V)
	if !ok {
		return ""
	}
	shown := show(ownNum)
	if inhOK {
		if inhNum, ok := vsd.Number(inh.V); ok && show(inhNum) == shown {
			return ""
		}
	}
	return shown
}

// formatsPoints returns n inches in points as the character Size
// shows it ("10").
func formatsPoints(n float64) string {
	return geom.FormatNum(n * vsd.PointsPerInch)
}

// formatsLength returns n inches in points with the unit ("18pt"),
// as the paragraph indents and spacings show it.
func formatsLength(n float64) string {
	return geom.FormatNum(n*vsd.PointsPerInch) + "pt"
}

// formatsLineHeight returns n, a SpLine value, as the HTML shows it:
// a negative value as a percentage ("120%"), otherwise in points
// ("14.4pt").
func formatsLineHeight(n float64) string {
	if n < 0 {
		return geom.FormatNum(-n*100) + "%"
	}
	return geom.FormatNum(n*vsd.PointsPerInch) + "pt"
}

// formatsColor returns own's colour resolved through the document,
// or "" when it does not resolve or resolves to the inherited
// colour.
func formatsColor(r *inherit.Resolver, own, inh sheet.Cell, inhOK bool) string {
	hex, ok := r.Doc().Color(own.V)
	if !ok {
		return ""
	}
	if inhOK {
		if inhHex, ok := r.Doc().Color(inh.V); ok && inhHex == hex {
			return ""
		}
	}
	return hex
}

// formatsFont returns own's face name, or "" when it is empty,
// Themed, holds ';' (htmlx.StyleDecls would split it), or stores the
// same name as the inherited cell. A name holding ';' is left out of
// the format, with an omission on the object ("font name with ';' not
// shown") recorded by the caller that owns the object; it is kept on
// write as every unshown cell.
func formatsFont(own, inh sheet.Cell, inhOK bool) string {
	if own.V == "" || own.V == vsd.ValueThemed || strings.Contains(own.V, ";") {
		return ""
	}
	if inhOK && own.V == inh.V {
		return ""
	}
	return own.V
}

// formatsAlign returns own's HorzAlign as the HTML shows it ("left",
// "center", "right", "justify"), or "" when it is not a number,
// stores the same number as the inherited cell, or is outside 0 to 3
// (Visio's force-justify 4 is kept on write, not shown).
func formatsAlign(own, inh sheet.Cell, inhOK bool) string {
	ownNum, ok := formatsInches(own, inh, inhOK)
	if !ok || ownNum != float64(int(ownNum)) {
		return ""
	}
	switch int(ownNum) {
	case 0:
		return "left"
	case 1:
		return "center"
	case 2:
		return "right"
	case 3:
		return "justify"
	}
	return ""
}
