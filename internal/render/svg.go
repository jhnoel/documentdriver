package render

import (
	"encoding/base64"
	"io"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
)

// svgWriter writes a scene as SVG, remembering the first write error.
type svgWriter struct {
	w   io.Writer
	err error
}

// str writes s.
func (w *svgWriter) str(s string) {
	if w.err == nil {
		_, w.err = io.WriteString(w.w, s)
	}
}

// attr writes name="value" with the value escaped by
// xmlscan.EscapeAttr (tab, newline and carriage return as character
// references, so attribute-value normalisation keeps them, as pptx
// read --format svg writes them).
func (w *svgWriter) attr(name, value string) {
	w.str(" " + name + `="`)
	w.str(xmlscan.EscapeAttr(value, '"'))
	w.str(`"`)
}

// num formats a coordinate: at most three decimals, no trailing zeros.
func num(f float64) string {
	if f > -0.0005 && f < 0.0005 {
		return "0"
	}
	return trimNum(strconv.FormatFloat(f, 'f', 3, 64))
}

// trimNum drops trailing zeros and a trailing point.
func trimNum(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}

// PathData returns p as SVG path data.
func PathData(p Path) string {
	var b strings.Builder
	for i, s := range p {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch s.Op {
		case 'M', 'L':
			b.WriteByte(s.Op)
			b.WriteString(num(s.P[0].X) + " " + num(s.P[0].Y))
		case 'C':
			b.WriteByte('C')
			for j, pt := range s.P {
				if j > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(num(pt.X) + " " + num(pt.Y))
			}
		case 'Z':
			b.WriteByte('Z')
		}
	}
	return b.String()
}

// matrix returns an SVG transform for m, "" for the identity.
func matrix(m geom.Affine) string {
	if m == geom.Identity {
		return ""
	}
	f := func(v float64) string { return trimNum(strconv.FormatFloat(v, 'f', 6, 64)) }
	return "matrix(" + f(m.A) + " " + f(m.B) + " " + f(m.C) + " " + f(m.D) + " " + f(m.E) + " " + f(m.F) + ")"
}

// WriteSVG writes the scene as a self-contained SVG document in
// points: width, height and viewBox are the page size, a rectangle
// paints the background, and each group is a g with its id and data
// attributes. Only fills, solid strokes, text and embedded images
// appear (see Scene).
func WriteSVG(out io.Writer, s *Scene) error {
	w := &svgWriter{w: out}
	w.str(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" version="1.1"`)
	w.attr("width", num(s.W))
	w.attr("height", num(s.H))
	w.attr("viewBox", "0 0 "+num(s.W)+" "+num(s.H))
	for _, a := range s.Attrs {
		w.attr(a[0], a[1])
	}
	w.str(">\n")
	if s.Background != "" {
		w.str("  <rect")
		w.attr("width", num(s.W))
		w.attr("height", num(s.H))
		w.attr("fill", s.Background)
		w.str("/>\n")
	}
	for _, g := range s.Groups {
		w.str("  <g")
		if g.ID != "" {
			w.attr("id", g.ID)
		}
		for _, a := range g.Attrs {
			w.attr(a[0], a[1])
		}
		w.str(">\n")
		for i := range g.Items {
			w.item(&g.Items[i])
		}
		w.str("  </g>\n")
	}
	w.str("</svg>\n")
	return w.err
}

// item writes one item's element.
func (w *svgWriter) item(it *Item) {
	switch it.Kind {
	case FillItem, StrokeItem:
		if len(it.Path) == 0 {
			return
		}
		w.str("    <path")
		for _, a := range it.Attrs {
			w.attr(a[0], a[1])
		}
		w.attr("d", PathData(it.Path))
		if it.Kind == FillItem {
			w.attr("fill", it.Color)
			if it.Opacity > 0 && it.Opacity < 1 {
				w.attr("fill-opacity", num(it.Opacity))
			}
			w.attr("stroke", "none")
		} else {
			w.attr("fill", "none")
			w.attr("stroke", it.Color)
			w.attr("stroke-width", num(it.Width))
			w.attr("stroke-linecap", "butt")
			w.attr("stroke-linejoin", "round")
			if it.Opacity > 0 && it.Opacity < 1 {
				w.attr("stroke-opacity", num(it.Opacity))
			}
		}
		w.str("/>\n")
	case TextItem:
		w.text(it)
	case ImageItem:
		w.image(it)
	}
}

// text writes a text line as a text element of tspans.
func (w *svgWriter) text(it *Item) {
	t := it.Text
	if t == nil || len(t.Spans) == 0 {
		return
	}
	w.str("    <text")
	for _, a := range it.Attrs {
		w.attr(a[0], a[1])
	}
	// One span: the viewer anchors it, with its own font metrics. Several
	// spans: each starts at an x from the width tables, because some
	// rasterisers (MuPDF) anchor every tspan of a line on its own.
	x, anchor := t.X, t.Anchor
	starts := spanStarts(t)
	if len(t.Spans) > 1 {
		x, anchor = starts[0], "start"
	}
	w.attr("x", num(x))
	w.attr("y", num(t.Y))
	if anchor != "" && anchor != "start" {
		w.attr("text-anchor", anchor)
	}
	if m := matrix(t.Frame); m != "" {
		w.attr("transform", m)
	}
	w.attr("xml:space", "preserve")
	w.str(">")
	for i, sp := range t.Spans {
		w.str("<tspan")
		if len(t.Spans) > 1 {
			w.attr("x", num(starts[i]))
		}
		w.attr("font-family", sp.Family)
		w.attr("font-size", num(sp.Size))
		if sp.Bold {
			w.attr("font-weight", "bold")
		}
		if sp.Italic {
			w.attr("font-style", "italic")
		}
		w.attr("fill", sp.Color)
		w.str(">")
		w.str(xmlscan.EscapeText(svgText(sp.Text)))
		w.str("</tspan>")
	}
	w.str("</text>\n")
}

// svgText drops the characters XML 1.0 cannot hold (controls other
// than tab, newline and carriage return, surrogates, U+FFFE and
// U+FFFF) and turns tabs and line ends into spaces.
func svgText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20, r >= 0xD800 && r <= 0xDFFF, r == 0xFFFE, r == 0xFFFF:
			return -1
		}
		return r
	}, s)
}

// image writes an image as an embedded data URI, cropped by a nested
// svg viewport.
func (w *svgWriter) image(it *Item) {
	im := it.Image
	if im == nil {
		return
	}
	w.str("    <g")
	for _, a := range it.Attrs {
		w.attr(a[0], a[1])
	}
	if m := matrix(im.Frame); m != "" {
		w.attr("transform", m)
	}
	w.str(">")
	crop := im.Crop
	if crop[2] <= 0 || crop[3] <= 0 {
		crop = [4]float64{0, 0, 1, 1}
	}
	cropped := crop != [4]float64{0, 0, 1, 1}
	if cropped {
		w.str(`<svg x="0" y="0" width="1" height="1" preserveAspectRatio="none" overflow="hidden"`)
		f := func(v float64) string { return trimNum(strconv.FormatFloat(v, 'f', 6, 64)) }
		w.attr("viewBox", f(crop[0])+" "+f(crop[1])+" "+f(crop[2])+" "+f(crop[3]))
		w.str(">")
	}
	w.str(`<image x="0" y="0" width="1" height="1" preserveAspectRatio="none"`)
	w.attr("xlink:href", "data:"+im.Type+";base64,"+base64.StdEncoding.EncodeToString(im.Data))
	w.str("/>")
	if cropped {
		w.str("</svg>")
	}
	w.str("</g>\n")
}

// spanStarts returns where each span of a line starts, from the width
// tables and the line's anchor.
func spanStarts(t *TextLine) []float64 {
	width := 0.0
	ws := make([]float64, len(t.Spans))
	for i, sp := range t.Spans {
		ws[i] = TextWidth(sp.Family, sp.Bold, sp.Italic, sp.Size, sp.Text)
		width += ws[i]
	}
	x := t.X
	switch t.Anchor {
	case "middle":
		x -= width / 2
	case "end":
		x -= width
	}
	out := make([]float64, len(t.Spans))
	for i := range t.Spans {
		out[i] = x
		x += ws[i]
	}
	return out
}
