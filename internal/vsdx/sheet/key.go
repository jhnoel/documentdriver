package sheet

// Key names one cell of a sheet: a shape-level cell has Section "";
// a cell of a named row (Property, User, Hyperlink, ...) has Row set; a
// cell of an indexed row (Geometry, Character, Paragraph, Connection,
// Layer, Field, Tabs) has RowIX >= 0. SectionIX is the IX of a
// repeatable section (Geometry), -1 otherwise. Keys compare with ==.
type Key struct {
	Section   string
	SectionIX int
	Row       string
	RowIX     int
	Cell      string
}

// ShapeCell returns the key of a shape-level cell.
func ShapeCell(name string) Key { return Key{SectionIX: -1, RowIX: -1, Cell: name} }

// NamedRowCell returns the key of a cell of a named row (Prop.Cost's
// Value is NamedRowCell("Property", "Cost", "Value")).
func NamedRowCell(section, row, cell string) Key {
	return Key{Section: section, SectionIX: -1, Row: row, RowIX: -1, Cell: cell}
}

// IndexedRowCell returns the key of a cell of an indexed row.
func IndexedRowCell(section string, sectionIX, rowIX int, cell string) Key {
	return Key{Section: section, SectionIX: sectionIX, RowIX: rowIX, Cell: cell}
}

// WithCell returns k, which names a row, with its cell set to name.
func (k Key) WithCell(name string) Key { k.Cell = name; return k }
