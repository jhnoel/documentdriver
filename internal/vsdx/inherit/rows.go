package inherit

import (
	"slices"

	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

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
	if s == nil || section == "" {
		return nil
	}
	var master *Shape
	if r != nil {
		if ms, ok := r.MasterShape(s); ok {
			master = ms
		}
	}
	var ownSheet, masterSheet *sheet.Sheet
	var ownRows, masterRows []sheet.Key
	if s.Sheet != nil {
		ownSheet = s.Sheet
		ownRows = ownSheet.Rows(section)
	}
	if master != nil && master.Sheet != nil {
		masterSheet = master.Sheet
		masterRows = masterSheet.Rows(section)
	}
	m := &rowMerger{ownRows: ownRows, masterRows: masterRows}
	if masterSheet != nil {
		m.add(masterRows, Master, masterSheet, []*sheet.Sheet{ownSheet})
	}
	if section == vsd.SectionCharacter || section == vsd.SectionParagraph {
		if id, ok := styleStart(s, master, vsd.TextCategory); ok {
			blockers := []*sheet.Sheet{ownSheet, masterSheet}
			for _, sh := range r.styleSheets(vsd.TextCategory, id) {
				if sh == nil {
					continue
				}
				m.add(sh.Rows(section), Style, sh, blockers)
				blockers = append(blockers, sh)
			}
		}
	}
	if ownSheet != nil {
		m.add(ownRows, Local, ownSheet, nil)
	}
	return m.rows
}

// rowMerger merges the rows of one section from the master, style and
// own sheets into the effective list: a key already listed keeps its
// place and takes the higher origin, a key a sheet deletes is blocked
// for the sheets below it, and a key a higher sheet holds is protected
// from a deletion below it. Chunk H3.
type rowMerger struct {
	rows       []Row
	seen       map[sheet.Key]int
	blocked    map[sheet.Key]bool
	ownRows    []sheet.Key
	masterRows []sheet.Key
}

// add merges keys, from src with origin, into the list, skipping keys
// a blocker (a sheet above src) deletes. Chunk H3.
func (m *rowMerger) add(keys []sheet.Key, origin Origin, src *sheet.Sheet, blockers []*sheet.Sheet) {
	if len(keys) == 0 {
		return
	}
	if m.seen == nil {
		m.seen = map[sheet.Key]int{}
	}
	for _, key := range keys {
		if m.blocked[key] {
			continue
		}
		if m.deleted(key, src, blockers) {
			if !m.protected(key, origin) {
				if m.blocked == nil {
					m.blocked = map[sheet.Key]bool{}
				}
				m.blocked[key] = true
			}
			continue
		}
		if i, ok := m.seen[key]; ok {
			if originRank(origin) > originRank(m.rows[i].Origin) {
				m.rows[i].Origin = origin
			}
			continue
		}
		m.seen[key] = len(m.rows)
		m.rows = append(m.rows, Row{Key: key, Origin: origin})
	}
}

// deleted reports whether src or a blocker deletes key (its row or its
// section). Chunk H3.
func (m *rowMerger) deleted(key sheet.Key, src *sheet.Sheet, blockers []*sheet.Sheet) bool {
	if src != nil && src.Deleted(key) {
		return true
	}
	for _, b := range blockers {
		if b != nil && b.Deleted(key) {
			return true
		}
	}
	return false
}

// protected reports whether a sheet above the source of origin holds
// key, so a deletion by that source does not block the higher sheet's
// row. Chunk H3.
func (m *rowMerger) protected(key sheet.Key, origin Origin) bool {
	switch origin {
	case Master:
		return slices.Contains(m.ownRows, key)
	case Style:
		return slices.Contains(m.ownRows, key) || slices.Contains(m.masterRows, key)
	}
	return false
}

// originRank ranks origins for row merging: local above master above
// style, as Cell resolves them. Chunk H3.
func originRank(o Origin) int {
	switch o {
	case Local:
		return 3
	case Master:
		return 2
	case Style:
		return 1
	}
	return 0
}
