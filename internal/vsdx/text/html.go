package text

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
	_, _, _ = b, f, shape
	return ""
}
