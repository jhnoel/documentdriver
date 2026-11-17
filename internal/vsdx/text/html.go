package text

import (
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// CharFormat is the formatting of one Character row as the HTML shows
// it, each "" when no sheet on the inheritance chain sets it: Bold,
// Italic, Underline and Strike are "on" or "off"; Size in points
// ("10.5"); Color "#RRGGBB"; Font the face name. Pos, Case, small caps
// and the other Character cells are not shown (docs/vsdx-design.md,
// decision f).
type CharFormat struct {
	Bold, Italic, Underline, Strike string
	Size, Color, Font               string
}

// ParaFormat is the formatting of one Paragraph row as the HTML shows
// it: Align "left", "center", "right", "justify" ("" also for Visio's
// force-justify 4, which HTML does not show); Bullet true for a
// bulleted paragraph; MarginLeft, MarginRight and Indent in points
// ("18pt"); SpaceBefore, SpaceAfter ("6pt") and LineHeight ("120%" or
// "14pt").
type ParaFormat struct {
	Align                   string
	Bullet                  bool
	BulletChar              string // BulletStr as stored; "" when not stored
	MarginLeft, MarginRight string
	Indent, SpaceBefore     string
	SpaceAfter, LineHeight  string
}

// Formats gives the formats of the shape's rows by IX; a row it has no
// entry for shows no formatting. As pptx shows only an object's own run
// and paragraph properties, never inherited ones, a format holds only
// the cells stored on the sheet that owns the shown Text: the shape's
// own rows when it has its own Text, the master shape's rows when it
// shows the master's Text. Values from style sheets, and master rows
// under a shape's own Text, are not shown. An "off" (Bold "off") is
// shown only when that sheet stores the bit cleared and the next sheet
// on the chain sets it; a stored cell equal to what it inherits shows
// nothing (Visio stores Style and Size on most rows). Built by
// model.formats (chunk M5).
type Formats struct {
	Char map[int]CharFormat
	Para map[int]ParaFormat
}

// Render writes b as HTML in the pptx text grammar's subset: <p>, and
// <ul><li> for bulleted paragraphs; <b>, <i>, <u>, <s>; <span style>
// with font-weight:normal (and the other "off" forms), font-size, color
// and font-family, in pptx's order and separators; <br/>; a field
// as <opaque data-ref="<shape>/field:<n>" data-kind="field"
// data-text="cached text"></opaque> (n is 1-based in the shape). shape
// is the shape's address. Text is escaped through xmlscan.EscapeText.
// What read writes, a P2 text.set accepts unchanged. Chunk T2.
func Render(b Body, f Formats, shape string) string {
	if len(b.Paras) == 0 {
		return ""
	}
	var w strings.Builder
	list, fields := "", 0
	for _, p := range b.Paras {
		pf := paraFormat(f, p.PP)
		want, tag := "", "p"
		if pf.Bullet {
			want, tag = "ul", "li"
		}
		if want != list {
			if list != "" {
				w.WriteString("</" + list + ">")
			}
			if want != "" {
				w.WriteString("<" + want + ">")
			}
			list = want
		}
		var sb strings.Builder
		n := writeParaStyles(&sb, pf)
		w.WriteString("<")
		w.WriteString(tag)
		if n > 0 {
			w.WriteString(` style="`)
			w.WriteString(xmlscan.EscapeAttr(sb.String(), '"'))
			w.WriteString(`"`)
		}
		if pf.Bullet && pf.BulletChar != "" {
			w.WriteString(` data-char="`)
			w.WriteString(xmlscan.EscapeAttr(pf.BulletChar, '"'))
			w.WriteString(`"`)
		}
		w.WriteString(">")
		fields = renderRuns(&w, p.Runs, f, shape, fields)
		w.WriteString("</")
		w.WriteString(tag)
		w.WriteString(">")
	}
	if list != "" {
		w.WriteString("</" + list + ">")
	}
	return w.String()
}

// renderRuns writes one paragraph's runs, returning the field count
// so far: adjacent text runs that show the same formatting are merged
// (their boundary is not shown), a break and a field close the open
// run, and a field shows as bare <opaque> however it is formatted.
func renderRuns(w *strings.Builder, runs []Run, f Formats, shape string, fields int) int {
	var cur CharFormat
	open, span := false, false
	finish := func() {
		if !open {
			return
		}
		if span {
			w.WriteString("</span>")
		}
		if cur.Strike == "on" {
			w.WriteString("</s>")
		}
		if cur.Underline == "on" {
			w.WriteString("</u>")
		}
		if cur.Italic == "on" {
			w.WriteString("</i>")
		}
		if cur.Bold == "on" {
			w.WriteString("</b>")
		}
		open, span = false, false
	}
	for _, r := range runs {
		switch {
		case r.Br:
			finish()
			w.WriteString("<br/>")
		case r.Field >= 0:
			finish()
			fields++
			w.WriteString(`<opaque data-ref="`)
			w.WriteString(xmlscan.EscapeAttr(vsd.FieldID(shape, fields), '"'))
			w.WriteString(`" data-kind="field" data-text="`)
			w.WriteString(xmlscan.EscapeAttr(r.Text, '"'))
			w.WriteString(`"></opaque>`)
		default:
			cf := charFormat(f, r.CP)
			if r.Text == "" && cf == (CharFormat{}) {
				continue
			}
			if open && cf == cur {
				w.WriteString(xmlscan.EscapeText(r.Text))
				continue
			}
			finish()
			cur, open = cf, true
			if cf.Bold == "on" {
				w.WriteString("<b>")
			}
			if cf.Italic == "on" {
				w.WriteString("<i>")
			}
			if cf.Underline == "on" {
				w.WriteString("<u>")
			}
			if cf.Strike == "on" {
				w.WriteString("<s>")
			}
			var sb strings.Builder
			if writeCharStyles(&sb, cf) > 0 {
				span = true
				w.WriteString(`<span style="`)
				w.WriteString(xmlscan.EscapeAttr(sb.String(), '"'))
				w.WriteString(`">`)
			}
			w.WriteString(xmlscan.EscapeText(r.Text))
		}
	}
	finish()
	return fields
}

// charFormat returns the Character row in force for a run: row 0
// before any cp marker (cp -1), as Visio does.
func charFormat(f Formats, cp int) CharFormat {
	if cp < 0 {
		cp = 0
	}
	return f.Char[cp]
}

// paraFormat returns the Paragraph row in force for a paragraph:
// row 0 before any pp marker (pp -1), as Visio does.
func paraFormat(f Formats, pp int) ParaFormat {
	if pp < 0 {
		pp = 0
	}
	return f.Para[pp]
}

// writeCharStyles writes cf's span declarations to sb in pptx's
// order (the "off" forms, font-size, color, font-family), separated
// by ";", and returns how many it wrote.
func writeCharStyles(sb *strings.Builder, cf CharFormat) int {
	n := 0
	add := func(prop, value string) {
		if n > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(prop)
		sb.WriteByte(':')
		sb.WriteString(value)
		n++
	}
	if cf.Bold == "off" {
		add("font-weight", "normal")
	}
	if cf.Italic == "off" {
		add("font-style", "normal")
	}
	if cf.Underline == "off" || cf.Strike == "off" {
		add("text-decoration", "none")
	}
	if cf.Size != "" {
		add("font-size", cf.Size+"pt")
	}
	if cf.Color != "" {
		add("color", cf.Color)
	}
	if cf.Font != "" {
		add("font-family", "'"+cssStr(cf.Font)+"'")
	}
	return n
}

// writeParaStyles writes pf's paragraph declarations to sb in pptx's
// order (text-align, line-height, margin-top, margin-bottom,
// margin-left, margin-right, text-indent), separated by ";", and
// returns how many it wrote.
func writeParaStyles(sb *strings.Builder, pf ParaFormat) int {
	n := 0
	add := func(prop, value string) {
		if value == "" {
			return
		}
		if n > 0 {
			sb.WriteByte(';')
		}
		sb.WriteString(prop)
		sb.WriteByte(':')
		sb.WriteString(value)
		n++
	}
	add("text-align", pf.Align)
	add("line-height", pf.LineHeight)
	add("margin-top", pf.SpaceBefore)
	add("margin-bottom", pf.SpaceAfter)
	add("margin-left", pf.MarginLeft)
	add("margin-right", pf.MarginRight)
	add("text-indent", pf.Indent)
	return n
}

// cssStr escapes s for use inside a quoted CSS string, as pptx's
// cssStr does: backslash and single quote, in that order.
func cssStr(s string) string {
	if !strings.ContainsAny(s, `\'`) {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`)
}
