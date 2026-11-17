package emit

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TextXML returns the Character and Paragraph Section elements for the
// formats (cells only for what a format sets: Style bits, Size in
// inches, Color, Font, Strikethru; HorzAlign, Bullet, BulletStr,
// IndLeft, IndRight, IndFirst, SpBefore, SpAfter, SpLine) and the Text
// element: cp and pp markers where the row changes, a line feed
// between paragraphs and after the last when Terminated, U+2028 for a
// break, fld elements for field runs (their Field rows are kept by
// the caller). Chunk K5b.
func TextXML(b text.Body, f text.Formats) (sections, textEl []byte, err error) {
	for ix := range f.Char {
		if ix < 0 {
			return nil, nil, fmt.Errorf("vsdx emit: TextXML: character row %d is negative", ix)
		}
	}
	for ix := range f.Para {
		if ix < 0 {
			return nil, nil, fmt.Errorf("vsdx emit: TextXML: paragraph row %d is negative", ix)
		}
	}
	charSec, err := charSection(f)
	if err != nil {
		return nil, nil, err
	}
	paraSec, err := paraSection(f)
	if err != nil {
		return nil, nil, err
	}
	var sec strings.Builder
	sec.Grow(len(charSec) + len(paraSec))
	sec.Write(charSec)
	sec.Write(paraSec)
	var out strings.Builder
	out.Grow(128 + 16*len(b.Paras))
	out.WriteString("<" + vsd.ElemText + ">")
	curCP, curPP := -1, -1
	for i, para := range b.Paras {
		if para.PP < -1 {
			return nil, nil, fmt.Errorf("vsdx emit: TextXML: paragraph %d pp %d is below -1", i, para.PP)
		}
		if i > 0 {
			out.WriteByte('\n')
		}
		if paraSec != nil && para.PP >= 0 && para.PP != curPP {
			out.WriteString("<" + vsd.ElemParaMark + " IX='" + strconv.Itoa(para.PP) + "'/>")
			curPP = para.PP
		}
		for j, run := range para.Runs {
			if run.CP < -1 {
				return nil, nil, fmt.Errorf("vsdx emit: TextXML: paragraph %d run %d cp %d is below -1", i, j, run.CP)
			}
			if run.Field < -1 {
				return nil, nil, fmt.Errorf("vsdx emit: TextXML: paragraph %d run %d field %d is below -1", i, j, run.Field)
			}
			if charSec != nil && run.CP >= 0 && run.CP != curCP {
				out.WriteString("<" + vsd.ElemCharMark + " IX='" + strconv.Itoa(run.CP) + "'/>")
				curCP = run.CP
			}
			switch {
			case run.Br:
				out.WriteString("\u2028")
			case run.Field >= 0:
				out.WriteString("<" + vsd.ElemField + " IX='" + strconv.Itoa(run.Field) + "'>")
				out.WriteString(xmlscan.EscapeText(run.Text))
				out.WriteString("</" + vsd.ElemField + ">")
			default:
				out.WriteString(xmlscan.EscapeText(run.Text))
			}
		}
	}
	if b.Terminated && len(b.Paras) > 0 {
		out.WriteByte('\n')
	}
	out.WriteString("</" + vsd.ElemText + ">")
	return []byte(sec.String()), []byte(out.String()), nil
}

// charSection returns the Character Section with rows 0..last for the
// character formats, or nil when every character format is empty.
func charSection(f text.Formats) ([]byte, error) {
	last, set := -1, false
	for ix, cf := range f.Char {
		if ix > last {
			last = ix
		}
		if !emptyChar(cf) {
			set = true
		}
	}
	if !set {
		return nil, nil
	}
	var b strings.Builder
	b.Grow(64 + 32*(last+1))
	b.WriteString("<" + vsd.ElemSection + " N='" + vsd.SectionCharacter + "'>")
	for ix := 0; ix <= last; ix++ {
		cells, err := charCells(ix, f.Char[ix])
		if err != nil {
			return nil, err
		}
		writeRow(&b, ix, cells)
	}
	b.WriteString("</" + vsd.ElemSection + ">")
	return []byte(b.String()), nil
}

// paraSection returns the Paragraph Section with rows 0..last for the
// paragraph formats, or nil when every paragraph format is empty.
func paraSection(f text.Formats) ([]byte, error) {
	last, set := -1, false
	for ix, pf := range f.Para {
		if ix > last {
			last = ix
		}
		if !emptyPara(pf) {
			set = true
		}
	}
	if !set {
		return nil, nil
	}
	var b strings.Builder
	b.Grow(64 + 32*(last+1))
	b.WriteString("<" + vsd.ElemSection + " N='" + vsd.SectionParagraph + "'>")
	for ix := 0; ix <= last; ix++ {
		cells, err := paraCells(ix, f.Para[ix])
		if err != nil {
			return nil, err
		}
		writeRow(&b, ix, cells)
	}
	b.WriteString("</" + vsd.ElemSection + ">")
	return []byte(b.String()), nil
}

// emptyChar reports whether cf sets no character cell.
func emptyChar(cf text.CharFormat) bool {
	return cf == (text.CharFormat{})
}

// emptyPara reports whether pf sets no paragraph cell.
func emptyPara(pf text.ParaFormat) bool {
	return pf == (text.ParaFormat{})
}

// charCells returns the cells of one Character row ix in the order
// Style, Size, Color, Font, Strikethru.
func charCells(ix int, cf text.CharFormat) (string, error) {
	var b strings.Builder
	if cf.Bold != "" || cf.Italic != "" || cf.Underline != "" {
		bits := 0
		for _, pair := range []struct {
			value string
			bit   int
		}{{cf.Bold, 1}, {cf.Italic, 2}, {cf.Underline, 4}} {
			switch pair.value {
			case "", "off":
			case "on":
				bits |= pair.bit
			default:
				return "", fmt.Errorf("vsdx emit: TextXML: character row %d: style value %q is not on, off or empty", ix, pair.value)
			}
		}
		writeCell(&b, "Style", strconv.Itoa(bits))
	}
	if cf.Size != "" {
		inches, err := pointsInches(ix, "Size", cf.Size)
		if err != nil {
			return "", err
		}
		writeCell(&b, "Size", inches)
	}
	if cf.Color != "" {
		if _, ok := vsd.Color(cf.Color); !ok {
			return "", fmt.Errorf("vsdx emit: TextXML: character row %d: color %q is not #RRGGBB", ix, cf.Color)
		}
		writeCell(&b, "Color", xmlscan.EscapeAttr(cf.Color, '\''))
	}
	if cf.Font != "" {
		writeCell(&b, "Font", xmlscan.EscapeAttr(cf.Font, '\''))
	}
	if cf.Strike != "" {
		switch cf.Strike {
		case "on":
			writeCell(&b, "Strikethru", one)
		case "off":
			writeCell(&b, "Strikethru", zero)
		default:
			return "", fmt.Errorf("vsdx emit: TextXML: character row %d: strikethru %q is not on, off or empty", ix, cf.Strike)
		}
	}
	return b.String(), nil
}

// paraCells returns the cells of one Paragraph row ix in the order
// HorzAlign, Bullet, BulletStr, IndLeft, IndRight, IndFirst, SpBefore,
// SpAfter, SpLine.
func paraCells(ix int, pf text.ParaFormat) (string, error) {
	var b strings.Builder
	if pf.Align != "" {
		var align string
		switch pf.Align {
		case "left":
			align = "0"
		case "center":
			align = "1"
		case "right":
			align = "2"
		case "justify":
			align = "3"
		default:
			return "", fmt.Errorf("vsdx emit: TextXML: paragraph row %d: align %q is not left, center, right, justify or empty", ix, pf.Align)
		}
		writeCell(&b, "HorzAlign", align)
	}
	if pf.Bullet {
		writeCell(&b, "Bullet", one)
	}
	if pf.BulletChar != "" {
		writeCell(&b, "BulletStr", xmlscan.EscapeAttr(pf.BulletChar, '\''))
	}
	for _, cell := range []struct {
		name, value string
	}{
		{"IndLeft", pf.MarginLeft},
		{"IndRight", pf.MarginRight},
		{"IndFirst", pf.Indent},
		{"SpBefore", pf.SpaceBefore},
		{"SpAfter", pf.SpaceAfter},
	} {
		if cell.value == "" {
			continue
		}
		inches, err := pointsInches(ix, cell.name, cell.value)
		if err != nil {
			return "", err
		}
		writeCell(&b, cell.name, inches)
	}
	if pf.LineHeight != "" {
		inches, err := lineHeightInches(ix, pf.LineHeight)
		if err != nil {
			return "", err
		}
		writeCell(&b, "SpLine", inches)
	}
	return b.String(), nil
}

// writeRow writes one Section row ix: self-closed when cells is empty,
// otherwise a Row holding the cells.
func writeRow(b *strings.Builder, ix int, cells string) {
	if cells == "" {
		b.WriteString("<" + vsd.ElemRow + " IX='" + strconv.Itoa(ix) + "'/>")
		return
	}
	b.WriteString("<" + vsd.ElemRow + " IX='" + strconv.Itoa(ix) + "'>" + cells + "</" + vsd.ElemRow + ">")
}

// pointsInches converts a point length (a bare number such as Size
// "12" or a suffixed one such as "18pt") to its stored inches.
func pointsInches(ix int, cell, value string) (string, error) {
	points, err := strconv.ParseFloat(strings.TrimSuffix(value, "pt"), 64)
	if err != nil || math.IsNaN(points) || math.IsInf(points, 0) {
		return "", fmt.Errorf("vsdx emit: TextXML: row %d cell %s: %q is not a point length", ix, cell, value)
	}
	return vsd.FormatInches(points / vsd.PointsPerInch), nil
}

// lineHeightInches converts a line height (a percentage such as
// "120%", stored as the negative multiple -1.2, or points) to inches.
func lineHeightInches(ix int, value string) (string, error) {
	if strings.HasSuffix(value, "%") {
		percent, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
		if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) {
			return "", fmt.Errorf("vsdx emit: TextXML: row %d cell SpLine: %q is not a percentage or points", ix, value)
		}
		return vsd.FormatInches(-percent / 100), nil
	}
	return pointsInches(ix, "SpLine", value)
}
