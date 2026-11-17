package vsd

// Kind is the kind of thing an address names.
type Kind uint8

// Address kinds (docs/vsdx-contract-draft.md, section 6).
const (
	KindPage Kind = iota + 1
	KindMaster
	KindShape
	KindProp
	KindLayer
	KindField
	KindAlias
)

// Address is a parsed agent address. Container is the page or master
// id and PageDup its ~n (1 when absent); Shape the shape id and Dup its
// ~n (1 when absent); Name the row name of a prop or the layer name; N
// the 1-based number of a field.
type Address struct {
	Kind      Kind
	InMaster  bool
	Container uint32
	PageDup   int
	Shape     uint32
	Dup       int
	Name      string
	N         int
	Alias     string
}

// PageID returns the address of a page: "page" and its stored ID
// ("page0"), and "page0~2" for the second page sharing ID 0 (dup 2;
// Visio never writes one, but a file may hold one). Chunk V2.
func PageID(id uint32, dup int) string {
	_, _ = id, dup
	return ""
}

// MasterID returns the address of a master: "master2". Chunk V2.
func MasterID(id uint32) string {
	_ = id
	return ""
}

// ShapeID returns the address of a shape in a page or master: "page0/s5",
// and "page0/s5~2" for the second shape sharing id 5 (dup 2). Chunk V2.
func ShapeID(container string, id uint32, dup int) string {
	_, _, _ = container, id, dup
	return ""
}

// PropID returns the address of a shape data row: "page0/s5/prop:Cost".
// Row names are kept as stored; a name holding "/" is written as is and
// parsed back by taking everything after "prop:". Chunk V2.
func PropID(shape, row string) string {
	_, _ = shape, row
	return ""
}

// LayerID returns the address of a page layer by its name:
// "page0/layer:Connector". Chunk V2.
func LayerID(page, name string) string {
	_, _ = page, name
	return ""
}

// FieldID returns the address of the n-th field (1-based, in text
// order) of a shape's text: "page0/s5/field:1". Chunk V2.
func FieldID(shape string, n int) string {
	_, _ = shape, n
	return ""
}

// Glue returns a connector end's glue as read shows it: "s12" for glue
// to the whole shape (ToCell PinX), "s12:3" for connection point 3
// (ToCell Connections.X3), "s12@<ToCell>" for any other ToCell
// (read-only), "#12..." when id 12 names no shape or several. Chunk V2.
func Glue(shape uint32, toCell string, resolved bool) string {
	_, _, _ = shape, toCell, resolved
	return ""
}

// Parse parses an address an agent typed (page, master, shape, prop,
// layer or $alias). It fails with an error that names the expected
// form. Chunk V2.
func Parse(s string) (Address, error) {
	_ = s
	return Address{}, ErrNotImplemented
}
