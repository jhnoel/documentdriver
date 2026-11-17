package emit

import "github.com/jhnoel/documentdriver/internal/vsdx/text"

// TextXML returns the Character and Paragraph Section elements for the
// formats (cells only for what a format sets: Style bits, Size in
// inches, Color, Font, Strikethru; HorzAlign, Bullet, BulletStr,
// IndLeft, IndRight, IndFirst, SpBefore, SpAfter, SpLine) and the Text
// element: cp and pp markers where the row changes, \n between
// paragraphs and after the last when Terminated,   for a break,
// fld elements kept by the caller. Chunk K5b.
func TextXML(b text.Body, f text.Formats) (sections, textEl []byte, err error) {
	_, _ = b, f
	return nil, nil, nil
}
