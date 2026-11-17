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
	var m rowMerger
	m.ownRows, m.masterRows = ownRows, masterRows
	if n := len(ownRows) + len(masterRows); n > 0 {
		m.rows = make([]Row, 0, n)
	}
	if masterSheet != nil {
		var one [1]*sheet.Sheet
		one[0] = ownSheet
		m.add(masterRows, Master, masterSheet, one[:])
	}
	if section == vsd.SectionCharacter || section == vsd.SectionParagraph {
		if id, ok := styleStart(s, master, vsd.TextCategory); ok {
			blockers := [MaxStyleDepth + 2]*sheet.Sheet{}
			blockers[0], blockers[1] = ownSheet, masterSheet
			n := 2
			r.styleWalk(vsd.TextCategory, id, func(sh *sheet.Sheet) bool {
				if sh == nil {
					return false
				}
				m.add(sh.Rows(section), Style, sh, blockers[:n])
				if n < len(blockers) {
					blockers[n] = sh
					n++
				}
				return false
			})
		}
	}
	if ownSheet != nil {
		m.add(ownRows, Local, ownSheet, nil)
	}
	return m.rows
}

// mergeMapThreshold is the number of merged rows or blocked keys at
// which a rowMerger builds lookup maps for them; at or below it the
// merger scans the small lists, so a section of a handful of rows
// merges with no map allocations.
const mergeMapThreshold = 48

// rowMerger merges the rows of one section from the master, style and
// own sheets into the effective list: a key already listed keeps its
// place and takes the higher origin, a key a sheet deletes is blocked
// for the sheets below it, and a key a higher sheet holds is protected
// from a deletion below it. Chunk H3. It stays on the stack: the merge
// below the threshold keeps the seen keys in rows and the blocked keys
// in a list, so listing rows allocates nothing but the result; above
// the threshold it builds the maps and merges in linear time.
type rowMerger struct {
	rows       []Row
	blocked    []sheet.Key
	seen       map[sheet.Key]int
	blockedSet map[sheet.Key]bool
	ownRows    []sheet.Key
	masterRows []sheet.Key
}

// add merges keys, from src with origin, into the list, skipping keys
// a blocker (a sheet above src) deletes. Chunk H3.
func (m *rowMerger) add(keys []sheet.Key, origin Origin, src *sheet.Sheet, blockers []*sheet.Sheet) {
	for _, key := range keys {
		if m.blockedHas(key) {
			continue
		}
		if m.deleted(key, src, blockers) {
			if !m.protected(key, origin) {
				m.block(key)
			}
			continue
		}
		if i := m.rowIndex(key); i >= 0 {
			if originRank(origin) > originRank(m.rows[i].Origin) {
				m.rows[i].Origin = origin
			}
			continue
		}
		m.appendRow(key, origin)
	}
}

// blockedHas reports whether key is blocked for the sheets below the
// one that deleted it. Chunk H3.
func (m *rowMerger) blockedHas(key sheet.Key) bool {
	if m.blockedSet != nil {
		return m.blockedSet[key]
	}
	return slices.Contains(m.blocked, key)
}

// block blocks key for the sheets below the one that deleted it,
// switching to a map when the blocked list grows past the threshold.
func (m *rowMerger) block(key sheet.Key) {
	if m.blockedSet != nil {
		m.blockedSet[key] = true
		return
	}
	m.blocked = append(m.blocked, key)
	if len(m.blocked) > mergeMapThreshold {
		m.toMaps()
		m.blockedSet[key] = true
	}
}

// rowIndex returns the position of key in the list, or -1 when it is
// not listed yet.
func (m *rowMerger) rowIndex(key sheet.Key) int {
	if m.seen != nil {
		if i, ok := m.seen[key]; ok {
			return i
		}
		return -1
	}
	for i := range m.rows {
		if m.rows[i].Key == key {
			return i
		}
	}
	return -1
}

// appendRow lists key with origin at the end of the list, switching to
// a map for the seen keys when the list grows past the threshold.
func (m *rowMerger) appendRow(key sheet.Key, origin Origin) {
	m.rows = append(m.rows, Row{Key: key, Origin: origin})
	if m.seen != nil {
		m.seen[key] = len(m.rows) - 1
		return
	}
	if len(m.rows) > mergeMapThreshold {
		m.toMaps()
	}
}

// toMaps builds the seen and blocked lookup maps of the lists the
// merger has built so far; it runs once, when either list outgrows the
// threshold, so a merge past it stays linear in its rows.
func (m *rowMerger) toMaps() {
	if m.seen != nil {
		return
	}
	m.seen = make(map[sheet.Key]int, len(m.rows))
	for i, row := range m.rows {
		m.seen[row.Key] = i
	}
	m.blockedSet = make(map[sheet.Key]bool, len(m.blocked))
	for _, key := range m.blocked {
		m.blockedSet[key] = true
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
