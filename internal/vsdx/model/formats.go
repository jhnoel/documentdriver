package model

import (
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
)

// formats builds the Character and Paragraph formats the HTML shows
// for the shape's text, by the rule of text.Formats: only the cells of
// owner (the sheet whose Text is shown), Style bits split into Bold,
// Italic and Underline, Size inches to points (geom.FormatNum), Color
// through doc.Document.Color, Font the stored face name ("Themed"
// omitted), HorzAlign 0 to 3, Bullet, BulletStr, IndLeft, IndRight,
// IndFirst, SpBefore, SpAfter, SpLine. Chunk M5.
func formats(r *inherit.Resolver, owner *inherit.Shape) text.Formats {
	_, _ = r, owner
	return text.Formats{}
}
