package model

import "github.com/jhnoel/documentdriver/internal/vsdx/inherit"

// readProps returns the shape's effective shape data rows (Property
// section) in order: label, type from the Type cell, value from the
// Value cell by type, format, hidden (Invisible) and linked
// (DataLinked). A row whose value does not parse as its type keeps the
// stored text as a string and gets an omission. Chunk M3.
func readProps(shape string, r *inherit.Resolver, s *inherit.Shape) ([]Prop, []Omission) {
	_, _, _ = shape, r, s
	return nil, nil
}
