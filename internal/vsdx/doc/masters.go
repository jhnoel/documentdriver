package doc

// MasterEntry is one Master of masters.xml in stored order.
type MasterEntry struct {
	ID       uint32
	Name     string
	NameU    string
	UniqueID string
	BaseID   string
	Hidden   bool
	Part     string
}

// readMasters parses masters.xml into entries, as readPages does.
// Chunk D1.
func (d *Document) readMasters() error {
	return nil
}
