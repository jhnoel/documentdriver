package sheet

// Bounds is a rectangle of cells grown cell by cell: rows R1..R2 and
// columns C1..C2, 1-based; OK is false until the first Add. The private
// bounds in this package goes in M7.
type Bounds struct {
	R1, C1, R2, C2 int
	OK             bool
}

// Add grows b to hold the cell at row r, column c.
func (b *Bounds) Add(r, c int) {
	if !b.OK {
		*b = Bounds{r, c, r, c, true}
		return
	}
	b.R1, b.R2 = min(b.R1, r), max(b.R2, r)
	b.C1, b.C2 = min(b.C1, c), max(b.C2, c)
}
