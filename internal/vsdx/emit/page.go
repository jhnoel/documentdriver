package emit

// Page is a new page: stored ID, name, size in inches and print
// orientation (1 portrait, 2 landscape; 0 writes none), and the
// relationship id of its part in pages.xml.rels.
type Page struct {
	ID          uint32
	Name        string
	Width       float64
	Height      float64
	Orientation int
	RelID       string
}

// PageXML returns the Page element of pages.xml: ID, NameU and Name,
// ViewScale -1, ViewCenterX/Y at the page centre, a PageSheet
// (LineStyle, FillStyle, TextStyle 0) with PageWidth, PageHeight,
// PageScale and DrawingScale 1 (U IN), DrawingSizeType 1,
// DrawingScaleType 0 and, when Orientation is set,
// PrintPageOrientation; then Rel r:id. Chunk K6.
func PageXML(p Page) ([]byte, error) {
	_ = p
	return nil, nil
}

// PageContentsXML returns an empty page part: PageContents in the main
// namespace with xml:space="preserve" and an empty Shapes element, as
// the corpus's pages hold. Chunk K6.
func PageContentsXML() []byte { return nil }
