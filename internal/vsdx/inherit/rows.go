package inherit

import "github.com/jhnoel/documentdriver/internal/vsdx/sheet"

// Row is one effective row of an enumerated section: its key (Cell "")
// and where the row itself is stored.
type Row struct {
	Key    sheet.Key
	Origin Origin
}

// Rows returns the effective rows of section for s: the master shape's
// rows (then the text style's, for Character and Paragraph) in their
// stored order, then the shape's own rows that are not among them, with
// rows a Del="1" removes left out. Property rows are matched by name,
// indexed rows by IX. Chunk H3.
func (r *Resolver) Rows(s *Shape, section string) []Row {
	_, _ = s, section
	return nil
}
