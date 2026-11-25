package emit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Base returns the value a text row of the shape takes from below it
// (its master shape's row of the same IX, else the style chain): the
// stored V of cell in row ix of section (vsd.SectionCharacter or
// vsd.SectionParagraph), and false when no sheet below stores it.
type Base func(section string, ix int, cell string) (string, bool)

// charRowCells are the cells CanonicalTextXML writes on every
// Character row, in Visio's order.
var charRowCells = []string{"Font", "Color", "Style", "Size", "Strikethru"}

// paraRowCells are the cells CanonicalTextXML writes on every
// Paragraph row, in Visio's order.
var paraRowCells = []string{"IndFirst", "IndLeft", "IndRight", "SpLine", "SpBefore", "SpAfter", "HorzAlign", "Bullet", "BulletStr"}

// zeroDefault lists the cells whose value is 0 when no sheet stores
// one (Visio's default for an absent bit or flag).
var zeroDefault = map[string]bool{"Style": true, "Strikethru": true, "Bullet": true}

// CanonicalTextXML returns the Character and Paragraph sections and the
// Text element of b in the form Visio writes and LibreOffice reads the
// same way: rows numbered in text order (text.Renumber), one Character
// row for each stretch of runs and one Paragraph row for each stretch
// of paragraphs, and every row holding the same cells (charRowCells,
// paraRowCells). A cell the row's format sets holds that value; the
// others hold base's value with F="Inh", Visio's form for an inherited
// cell, which read shows as no formatting; a cell base does not have
// is left out, except Style, Strikethru and Bullet, which hold 0 with
// F="Inh", and a Themed value is left out.
//
// LibreOffice's import takes row 0 as the defaults of every other row
// and drops an empty row, so a row that leaves a cell out shows row
// 0's value there (a plain run after a bold one is drawn bold):
// writing every cell on every row removes that dependence. A body
// with no runs has no Character section and one with no paragraphs no
// Paragraph section.
func CanonicalTextXML(b text.Body, f text.Formats, base Base) (sections, textEl []byte, err error) {
	if base == nil {
		base = func(string, int, string) (string, bool) { return "", false }
	}
	b, f = text.Renumber(b, f)
	runs := false
	for _, p := range b.Paras {
		if len(p.Runs) > 0 {
			runs = true
			break
		}
	}
	var sec strings.Builder
	if runs {
		sec.WriteString("<" + vsd.ElemSection + " N='" + vsd.SectionCharacter + "'>")
		for ix := 0; ix < len(f.Char); ix++ {
			cells, err := canonicalCharCells(ix, f.Char[ix], base)
			if err != nil {
				return nil, nil, err
			}
			writeRow(&sec, ix, cells)
		}
		sec.WriteString("</" + vsd.ElemSection + ">")
	}
	if len(b.Paras) > 0 {
		sec.WriteString("<" + vsd.ElemSection + " N='" + vsd.SectionParagraph + "'>")
		for ix := 0; ix < len(f.Para); ix++ {
			cells, err := canonicalParaCells(ix, f.Para[ix], base)
			if err != nil {
				return nil, nil, err
			}
			writeRow(&sec, ix, cells)
		}
		sec.WriteString("</" + vsd.ElemSection + ">")
	}
	textEl, err = textElement(b, runs, len(b.Paras) > 0)
	if err != nil {
		return nil, nil, err
	}
	return []byte(sec.String()), textEl, nil
}

// rowCells collects one row's cells by name: own values, written as
// plain cells, and inherited ones, written with F="Inh".
type rowCells struct {
	own map[string]string
}

// write writes the cells of names to b: the own value when there is
// one, else base's (escaped) with F="Inh", else 0 with F="Inh" for a
// zeroDefault cell; a Themed base value is left out.
func (rc rowCells) write(b *strings.Builder, section string, ix int, names []string, base Base) {
	for _, n := range names {
		if v, ok := rc.own[n]; ok {
			writeCell(b, n, v)
			continue
		}
		v, ok := base(section, ix, n)
		switch {
		case ok && v == vsd.ValueThemed:
		case ok:
			writeFormulaCell(b, n, xmlscan.EscapeAttr(v, '\''), vsd.FormulaInherited)
		case zeroDefault[n]:
			writeFormulaCell(b, n, zero, vsd.FormulaInherited)
		}
	}
}

// canonicalCharCells returns the cells of Character row ix for cf: the
// Style bits cf sets over base's bits, and Font, Color, Size and
// Strikethru from cf or base.
func canonicalCharCells(ix int, cf text.CharFormat, base Base) (string, error) {
	rc := rowCells{own: map[string]string{}}
	if cf.Bold != "" || cf.Italic != "" || cf.Underline != "" {
		bits := 0
		if v, ok := base(vsd.SectionCharacter, ix, "Style"); ok {
			if n, ok := vsd.Number(v); ok {
				bits = int(n)
			}
		}
		for _, pair := range []struct {
			value string
			bit   int
		}{{cf.Bold, 1}, {cf.Italic, 2}, {cf.Underline, 4}} {
			switch pair.value {
			case "":
			case "on":
				bits |= pair.bit
			case "off":
				bits &^= pair.bit
			default:
				return "", fmt.Errorf("vsdx emit: CanonicalTextXML: character row %d: style value %q is not on, off or empty", ix, pair.value)
			}
		}
		rc.own["Style"] = strconv.Itoa(bits)
	}
	if cf.Size != "" {
		inches, err := pointsInches(ix, "Size", cf.Size)
		if err != nil {
			return "", err
		}
		rc.own["Size"] = inches
	}
	if cf.Color != "" {
		if _, ok := vsd.Color(cf.Color); !ok {
			return "", fmt.Errorf("vsdx emit: CanonicalTextXML: character row %d: color %q is not #RRGGBB", ix, cf.Color)
		}
		rc.own["Color"] = xmlscan.EscapeAttr(cf.Color, '\'')
	}
	if cf.Font != "" {
		rc.own["Font"] = xmlscan.EscapeAttr(cf.Font, '\'')
	}
	switch cf.Strike {
	case "":
	case "on":
		rc.own["Strikethru"] = one
	case "off":
		rc.own["Strikethru"] = zero
	default:
		return "", fmt.Errorf("vsdx emit: CanonicalTextXML: character row %d: strikethru %q is not on, off or empty", ix, cf.Strike)
	}
	var b strings.Builder
	rc.write(&b, vsd.SectionCharacter, ix, charRowCells, base)
	return b.String(), nil
}

// canonicalParaCells returns the cells of Paragraph row ix for pf, each
// from pf or base.
func canonicalParaCells(ix int, pf text.ParaFormat, base Base) (string, error) {
	rc := rowCells{own: map[string]string{}}
	switch pf.Align {
	case "":
	case "left":
		rc.own["HorzAlign"] = "0"
	case "center":
		rc.own["HorzAlign"] = "1"
	case "right":
		rc.own["HorzAlign"] = "2"
	case "justify":
		rc.own["HorzAlign"] = "3"
	default:
		return "", fmt.Errorf("vsdx emit: CanonicalTextXML: paragraph row %d: align %q is not left, center, right, justify or empty", ix, pf.Align)
	}
	if pf.Bullet {
		rc.own["Bullet"] = one
	}
	if pf.BulletChar != "" {
		rc.own["BulletStr"] = xmlscan.EscapeAttr(pf.BulletChar, '\'')
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
		rc.own[cell.name] = inches
	}
	if pf.LineHeight != "" {
		inches, err := lineHeightInches(ix, pf.LineHeight)
		if err != nil {
			return "", err
		}
		rc.own["SpLine"] = inches
	}
	var b strings.Builder
	rc.write(&b, vsd.SectionParagraph, ix, paraRowCells, base)
	return b.String(), nil
}
