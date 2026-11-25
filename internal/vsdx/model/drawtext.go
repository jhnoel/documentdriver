package model

import (
	"math"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/place"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// TextBlock is an object's text placed for drawing: Frame maps the
// block's own space (points, y down, origin at its top-left corner, W
// by H) to the view; Margin is left, top, right and bottom in points;
// VAlign is "top", "middle" or "bottom"; Paras are the paragraphs with
// their effective formatting (after master, style and theme
// inheritance), unlike the HTML read shows, which carries only what
// the shape itself stores.
type TextBlock struct {
	Frame  geom.Affine
	W, H   float64
	Margin [4]float64
	VAlign string
	Paras  []TextPara
}

// TextPara is one paragraph: Align "left", "center", "right" or
// "justify", Bullet the bullet text ("" for none), LineSpacing the
// line height as a multiple of the font size (SpLine as a negative
// percentage; 0 when single or in points) and its runs.
type TextPara struct {
	Align       string
	Bullet      string
	LineSpacing float64
	Runs        []TextRun
}

// TextRun is a stretch of text under one effective character style,
// or a line break (Br).
type TextRun struct {
	Text  string
	Br    bool
	Style CharStyle
}

// CharStyle is a run's effective character formatting: Font the face
// name ("" when not derived or themed), Size in points (0 when not
// derived), Color "#RRGGBB" ("" when not derived) and the style bits.
type CharStyle struct {
	Font                            string
	Size                            float64
	Color                           string
	Bold, Italic, Underline, Strike bool
}

// textBlockOf sets d.Text from the shape's text (the Text it shows,
// as textOf reads it) and its effective text block cells. A text block
// cell that no sheet stores takes the value of Visio's default formula
// (the block is the shape's box, its pin at the centre); a margin or
// vertical alignment that is not derived is 0 or middle, with a note.
func textBlockOf(d *Drawing, r *inherit.Resolver, s *inherit.Shape, t place.Transform, f place.Frame, pageH float64) {
	node, _ := r.Text(s)
	if node == nil {
		return
	}
	body, _ := text.Parse(node)
	num := func(name string, def float64) float64 {
		if v, ok := shapeNumber(r, s, name); ok {
			return v
		}
		return def
	}
	tw := num(vsd.CellTxtWidth, t.Width)
	th := num(vsd.CellTxtHeight, t.Height)
	px := num(vsd.CellTxtPinX, t.Width/2)
	py := num(vsd.CellTxtPinY, t.Height/2)
	lx := num(vsd.CellTxtLocPinX, tw/2)
	ly := num(vsd.CellTxtLocPinY, th/2)
	ang := num(vsd.CellTxtAngle, 0)
	tb := &TextBlock{W: tw * vsd.PointsPerInch, H: th * vsd.PointsPerInch}
	// Block points (y down) to block inches (y up), to the shape's
	// local space, to the view.
	toBlock := geom.Scale(1.0/vsd.PointsPerInch, -1.0/vsd.PointsPerInch).Then(geom.Translate(0, th))
	local := geom.Translate(-lx, -ly).Then(geom.Rotate(ang * 180 / math.Pi)).Then(geom.Translate(px, py))
	frame := toBlock.Then(local).Then(t.Local()).Then(f.M).Then(place.View(pageH))
	if frame.A*frame.D-frame.B*frame.C < 0 {
		// A flipped shape does not mirror its text: reflect the block
		// about its vertical centre line.
		frame = geom.Scale(-1, 1).Then(geom.Translate(tb.W, 0)).Then(frame)
	}
	tb.Frame = frame
	for i, name := range [4]string{vsd.CellLeftMargin, vsd.CellTopMargin, vsd.CellRightMargin, vsd.CellBottomMargin} {
		if v, ok := shapeNumber(r, s, name); ok {
			tb.Margin[i] = v * vsd.PointsPerInch
		} else {
			d.Notes = append(d.Notes, name+" not derived: 0 used")
		}
	}
	tb.VAlign = "middle"
	if v, ok := shapeNumber(r, s, vsd.CellVerticalAlign); ok {
		switch v {
		case 0:
			tb.VAlign = "top"
		case 2:
			tb.VAlign = "bottom"
		}
	} else {
		d.Notes = append(d.Notes, "VerticalAlign not derived: middle used")
	}
	chars := rowKeys(r, s, vsd.SectionCharacter)
	paras := rowKeys(r, s, vsd.SectionParagraph)
	styles := map[int]CharStyle{}
	charStyle := func(ix int) CharStyle {
		if ix < 0 {
			ix = 0
		}
		if cs, ok := styles[ix]; ok {
			return cs
		}
		cs := effectiveChar(r, s, chars[ix])
		styles[ix] = cs
		return cs
	}
	for _, p := range body.Paras {
		ix := max(p.PP, 0)
		tp := effectivePara(r, s, paras[ix])
		for _, run := range p.Runs {
			if run.Br {
				tp.Runs = append(tp.Runs, TextRun{Br: true})
				continue
			}
			tp.Runs = append(tp.Runs, TextRun{Text: run.Text, Style: charStyle(run.CP)})
		}
		tb.Paras = append(tb.Paras, tp)
	}
	d.Text = tb
}

// rowKeys maps the row IX of each effective row of section to its key.
func rowKeys(r *inherit.Resolver, s *inherit.Shape, section string) map[int]sheet.Key {
	rows := r.Rows(s, section)
	out := make(map[int]sheet.Key, len(rows))
	for _, row := range rows {
		if row.Key.RowIX >= 0 {
			if _, dup := out[row.Key.RowIX]; !dup {
				out[row.Key.RowIX] = row.Key
			}
		}
	}
	return out
}

// rowCell returns the effective cell name of row k; a row the shape
// has no key for (k zero) has no cells.
func rowCell(r *inherit.Resolver, s *inherit.Shape, k sheet.Key, name string) (sheet.Cell, bool) {
	if k.Section == "" {
		return sheet.Cell{}, false
	}
	c, _, ok := r.Cell(s, k.WithCell(name))
	return c, ok
}

// effectiveChar returns the effective style of Character row k.
func effectiveChar(r *inherit.Resolver, s *inherit.Shape, k sheet.Key) CharStyle {
	var cs CharStyle
	if c, ok := rowCell(r, s, k, vsd.CellCharFont); ok && c.V != vsd.ValueThemed {
		cs.Font = strings.TrimSpace(c.V)
	}
	if c, ok := rowCell(r, s, k, vsd.CellCharSize); ok {
		if v, ok := vsd.Number(c.V); ok && v > 0 {
			cs.Size = v * vsd.PointsPerInch
		}
	}
	if c, ok := rowCell(r, s, k, vsd.CellCharColor); ok {
		if hex, ok := paintColor(r, c.V); ok {
			cs.Color = hex
		}
	}
	if c, ok := rowCell(r, s, k, vsd.CellCharStyle); ok {
		if v, ok := vsd.Number(c.V); ok {
			bits := int(v)
			cs.Bold, cs.Italic, cs.Underline = bits&1 != 0, bits&2 != 0, bits&4 != 0
		}
	}
	if c, ok := rowCell(r, s, k, vsd.CellCharStrike); ok {
		cs.Strike = formatsIsOn(c.V)
	}
	return cs
}

// effectivePara returns the effective formatting of Paragraph row k,
// without runs.
func effectivePara(r *inherit.Resolver, s *inherit.Shape, k sheet.Key) TextPara {
	tp := TextPara{Align: "center"}
	if c, ok := rowCell(r, s, k, vsd.CellParaHorzAlign); ok {
		if v, ok := vsd.Number(c.V); ok {
			switch v {
			case 0:
				tp.Align = "left"
			case 2:
				tp.Align = "right"
			case 3, 4:
				tp.Align = "justify"
			}
		}
	}
	if c, ok := rowCell(r, s, k, vsd.CellParaBullet); ok && formatsIsOn(c.V) {
		tp.Bullet = "•"
		if b, ok := rowCell(r, s, k, vsd.CellParaBulletStr); ok && b.V != "" {
			tp.Bullet = b.V
		}
	}
	if c, ok := rowCell(r, s, k, vsd.CellParaSpLine); ok {
		if v, ok := vsd.Number(c.V); ok && v < 0 {
			tp.LineSpacing = -v
		}
	}
	return tp
}
