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
	MarginLeft, MarginRight string
	Indent, SpaceBefore     string
	SpaceAfter, LineHeight  string
}

// Formats gives the resolved formats of the shape's rows by IX; a row
// it has no entry for shows no formatting.
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
