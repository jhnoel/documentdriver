package edit

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ascii"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/formula"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// RecomputeShape rewrites, in dependency order, the dependents of the
// cells changed on the page shape at shape (decision c-R, chunk RC1).
// The dependents are the transitive closure over same-shape references
// of the shape's effective cells: its own stored cells plus, for cells
// it does not store, its master shape's (an unreadable master
// contributes no formulas, as inherit resolves it). A formula that
// names a changed cell through a qualified reference (Sheet.5!Width)
// also collects its cell: it cannot be evaluated, so it is reported
// stale below.
//
// If every dependent's formula is in the closed grammar of formula,
// each dependent's V is rewritten with its recomputed value
// (vsd.FormatInches), keeping any local formula. A dependent the
// shape does not store is materialised locally as
// <Cell N=.. V=.. F="Inh"/> (with its U when it has one), creating the
// Section and Row it lives in when the shape stores none. The
// result is an empty stale list.
//
// If any dependent is outside the grammar (a function such as GUARD,
// another shape's or the page's reference), evaluation fails or a
// dependency cycle is found, nothing is rewritten and stale lists
// every dependent, in the shape's stored order then the master's, as
// "<shape> <cell reference>" (page0/s2 LocPinX, page0/s2
// Geometry1.X2): the caller falls back to S for this shape.
//
// A missing shape is an ErrNotFound error.
func (w *Workspace) RecomputeShape(shape string, changed []string) ([]string, error) {
	a, err := vsd.Parse(shape)
	if err != nil {
		return nil, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return nil, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", shape)
	}
	for _, name := range changed {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("vsdx edit: a changed cell has no name")
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return nil, err
	}
	shapeNode := w.shapeIndexFor(part, d.Root()).find(a.Shape, a.Dup)
	if shapeNode == nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	var masterNode *tree.Node
	if ms := w.recomputeMasterShape(shapeNode); ms != nil && ms.Sheet != nil {
		masterNode = ms.Sheet.Node
	}
	local, localDel := recomputeStored(shapeNode)
	var master []recomputeStoredCell
	if masterNode != nil {
		master, _ = recomputeStored(masterNode)
	}
	cells := recomputeEffective(local, master, localDel)
	refIndex := make(map[string]int, len(cells))
	keyIndex := make(map[sheet.Key]int, len(cells))
	for i := range cells {
		refIndex[ascii.Lower(cells[i].ref)] = i
		keyIndex[cells[i].key] = i
	}
	changedIdx := map[int]bool{}
	closure := map[string]bool{}
	queue := make([]string, 0, len(changed))
	for _, name := range changed {
		name = strings.TrimSpace(name)
		low := ascii.Lower(name)
		if !closure[low] {
			closure[low] = true
			queue = append(queue, low)
		}
		i, ok := refIndex[low]
		if !ok {
			if key, parsed := recomputeParseRef(name); parsed {
				i, ok = keyIndex[key]
			}
		}
		if !ok {
			continue
		}
		changedIdx[i] = true
		if lr := ascii.Lower(cells[i].ref); !closure[lr] {
			closure[lr] = true
			queue = append(queue, lr)
		}
	}
	// Reverse edges from a formula's own reference tokens (formula.Lex,
	// so a GUARD(Width) or a qualified Sheet.5!Width still collects its
	// cell), and forward edges to the cells a bare reference names.
	rev := map[string][]int{}
	fwd := make([][]int, len(cells))
	for i := range cells {
		if !cells[i].hasFormula {
			continue
		}
		for _, tok := range formula.Lex(cells[i].formula) {
			if tok.Kind != formula.Ref {
				continue
			}
			if j := strings.LastIndex(tok.Text, "!"); j >= 0 {
				low := ascii.Lower(tok.Text[j+1:])
				rev[low] = append(rev[low], i)
				continue
			}
			low := ascii.Lower(tok.Text)
			rev[low] = append(rev[low], i)
			if idx, ok := refIndex[low]; ok {
				fwd[i] = append(fwd[i], idx)
			}
		}
	}
	dependent := make([]bool, len(cells))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, i := range rev[name] {
			if dependent[i] || changedIdx[i] {
				continue
			}
			dependent[i] = true
			if lr := ascii.Lower(cells[i].ref); !closure[lr] {
				closure[lr] = true
				queue = append(queue, lr)
			}
		}
	}
	ev := &recomputeEval{cells: cells, fwd: fwd, dependent: dependent, refIndex: refIndex,
		values: make([]float64, len(cells)), done: make([]bool, len(cells))}
	if !ev.run() {
		var stale []string
		for i := range cells {
			if dependent[i] {
				stale = append(stale, shape+" "+cells[i].ref)
			}
		}
		return stale, nil
	}
	rw := newRecomputeWriter(w, part, shape, shapeNode)
	for i := range cells {
		if !dependent[i] {
			continue
		}
		c := &cells[i]
		v := vsd.FormatInches(ev.values[i])
		if c.local != nil {
			if err := rw.h.SetNodeAttr(c.local.node, "V", v); err != nil {
				return nil, fmt.Errorf("vsdx edit: %w", err)
			}
			continue
		}
		if err := rw.materialise(c, v); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// recomputeMasterShape returns the master shape of shapeNode, through
// inherit: its own Master and MasterShape, and for a group child the
// Master of the nearest ancestor shape that stores one. It returns
// nil when the shape has no master or the master cannot be read.
func (w *Workspace) recomputeMasterShape(shapeNode *tree.Node) *inherit.Shape {
	is := &inherit.Shape{}
	if v, ok := shapeNode.Attr("MasterShape"); ok {
		if u, err := strconv.ParseUint(v, 10, 32); err == nil {
			is.MasterShape, is.HasMShape = uint32(u), true
		}
	}
	for n := shapeNode; n != nil; n = n.Parent() {
		if !isElem(n, vsd.ElemShape) {
			continue
		}
		if v, ok := n.Attr("Master"); ok {
			if u, err := strconv.ParseUint(v, 10, 32); err == nil {
				is.Master, is.HasMaster = uint32(u), true
			}
			break
		}
	}
	if !is.HasMaster {
		return nil
	}
	if ms, ok := inherit.New(w.doc).MasterShape(is); ok {
		return ms
	}
	return nil
}

// recomputeStoredCell is one cell as one sheet stores it: its key and
// its node and attributes. The stored cells of a sheet are read by
// recomputeStored, which mirrors sheet.Index's keys and first-wins
// duplicates; the merge with a master is recomputeEffective's.
type recomputeStoredCell struct {
	key  sheet.Key
	node *tree.Node
	v    string
	hasV bool
	u    string
	f    string
	hasF bool
}

// recomputeCell is one effective cell of a shape: the merged local and
// master stored cells, its reference name, its effective formula, its
// display unit and its numeric value when its stored V is one.
type recomputeCell struct {
	key        sheet.Key
	ref        string
	local      *recomputeStoredCell
	master     *recomputeStoredCell
	formula    string
	hasFormula bool
	u          string
	value      float64
	hasValue   bool
}

// recomputeStored returns the stored cells of the sheet at el in
// document order and the keys of the sections and rows it deletes
// with Del="1" (keyed as sheet.Sheet keys them, with Cell ""). Cells
// of a deleted section or row are not returned, and a repeated key
// keeps its first cell, as sheet.Index does.
func recomputeStored(el *tree.Node) ([]recomputeStoredCell, map[sheet.Key]bool) {
	if el == nil {
		return nil, nil
	}
	var out []recomputeStoredCell
	del := map[sheet.Key]bool{}
	seen := map[sheet.Key]bool{}
	add := func(node *tree.Node, key sheet.Key) {
		if seen[key] {
			return
		}
		seen[key] = true
		cell := recomputeStoredCell{key: key, node: node}
		cell.v, cell.hasV = node.Attr("V")
		cell.u, _ = node.Attr("U")
		cell.f, cell.hasF = node.Attr("F")
		out = append(out, cell)
	}
	for child := range el.ElementsSeq() {
		switch child.Local() {
		case vsd.ElemCell:
			if name, ok := child.Attr("N"); ok && name != "" {
				add(child, sheet.ShapeCell(name))
			}
		case vsd.ElemSection:
			name, ok := child.Attr("N")
			if !ok || name == "" {
				continue
			}
			secIX := recomputeIX(child)
			if secIX == -2 {
				continue
			}
			secKey := sheet.Key{Section: name, SectionIX: secIX, RowIX: -1}
			if v, ok := child.Attr("Del"); ok && v == "1" {
				del[secKey] = true
				continue
			}
			for sub := range child.ElementsSeq() {
				switch sub.Local() {
				case vsd.ElemCell:
					if cellName, ok := sub.Attr("N"); ok && cellName != "" {
						add(sub, sheet.Key{Section: name, SectionIX: secIX, RowIX: -1, Cell: cellName})
					}
				case vsd.ElemRow:
					rowName, rowIX, ok := recomputeRowID(sub)
					if !ok {
						continue
					}
					rowKey := sheet.Key{Section: name, SectionIX: secIX, Row: rowName, RowIX: rowIX}
					if v, ok := sub.Attr("Del"); ok && v == "1" {
						del[rowKey] = true
						continue
					}
					for cell := range sub.ElementsSeq() {
						if cell.Local() != vsd.ElemCell {
							continue
						}
						if cellName, ok := cell.Attr("N"); ok && cellName != "" {
							add(cell, rowKey.WithCell(cellName))
						}
					}
				}
			}
		}
	}
	return out, del
}

// recomputeIX returns the IX attribute of el as an index: -1 when it
// has none, -2 when it does not parse (a key that matches nothing).
func recomputeIX(el *tree.Node) int {
	v, ok := el.Attr("IX")
	if !ok {
		return -1
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return -2
	}
	return n
}

// recomputeRowID returns the name and index of a Row element: a named
// row has its name and index -1, an indexed row an empty name and
// its index. It reports false when the row has neither.
func recomputeRowID(row *tree.Node) (name string, ix int, ok bool) {
	if n, ok := row.Attr("N"); ok && n != "" {
		return n, -1, true
	}
	if ix := recomputeIX(row); ix >= 0 {
		return "", ix, true
	}
	return "", 0, false
}

// recomputeDeleted reports whether the section or row of k is deleted
// in del (the deleted keys of recomputeStored).
func recomputeDeleted(del map[sheet.Key]bool, k sheet.Key) bool {
	return del[sheet.Key{Section: k.Section, SectionIX: k.SectionIX, RowIX: -1}] ||
		del[sheet.Key{Section: k.Section, SectionIX: k.SectionIX, Row: k.Row, RowIX: k.RowIX}]
}

// recomputeEffective merges the local and master stored cells of a
// shape into its effective cells, in local document order then master
// document order: a master cell the local sheet deletes is left out,
// and a key both store is one cell, the local one winning. A cell
// stored locally with F="Inh" takes its master's formula; a master
// cell takes its own formula.
func recomputeEffective(local, master []recomputeStoredCell, localDel map[sheet.Key]bool) []recomputeCell {
	cells := make([]recomputeCell, 0, len(local)+len(master))
	index := make(map[sheet.Key]int, len(local)+len(master))
	for i := range local {
		index[local[i].key] = len(cells)
		cells = append(cells, recomputeCell{key: local[i].key, local: &local[i]})
	}
	for i := range master {
		if recomputeDeleted(localDel, master[i].key) {
			continue
		}
		if j, ok := index[master[i].key]; ok {
			cells[j].master = &master[i]
			continue
		}
		index[master[i].key] = len(cells)
		cells = append(cells, recomputeCell{key: master[i].key, master: &master[i]})
	}
	for i := range cells {
		recomputeFinish(&cells[i])
	}
	return cells
}

// recomputeFinish settles one merged cell: its reference name, its
// effective formula (the local formula, or the master's when the
// local one is F="Inh" or absent) and its numeric value and unit
// (the local stored ones, or the master's when it stores none). The
// formulas Inh and No Formula are markers, never evaluated.
func recomputeFinish(c *recomputeCell) {
	c.ref = recomputeRefName(c.key)
	switch {
	case c.local != nil && c.local.hasF && c.local.f == vsd.FormulaInherited:
		if c.master != nil && c.master.hasF && recomputeRealFormula(c.master.f) {
			c.formula = c.master.f
		}
	case c.local != nil && c.local.hasF && recomputeRealFormula(c.local.f):
		c.formula = c.local.f
	case c.local == nil && c.master != nil && c.master.hasF && recomputeRealFormula(c.master.f):
		c.formula = c.master.f
	}
	c.hasFormula = c.formula != ""
	src := c.local
	if src == nil {
		src = c.master
	}
	if src == nil {
		return
	}
	c.u = src.u
	if !src.hasV {
		return
	}
	if v, ok := vsd.Number(src.v); ok {
		c.value, c.hasValue = v, true
		return
	}
	if b, ok := vsd.Bool(src.v); ok {
		c.hasValue = true
		if b {
			c.value = 1
		}
	}
}

// recomputeRealFormula reports whether f is a formula to evaluate,
// as opposed to Inh, No Formula or empty.
func recomputeRealFormula(f string) bool {
	return f != "" && f != vsd.FormulaInherited && f != vsd.FormulaNone
}

// recomputeRefName spells a sheet key as the ShapeSheet reference a
// formula names it by (the spelling model.CellKey shows in read):
// PinX, Geometry1.X2, Connections.X1, Prop.Cost.Value, Char.Size[1].
// It is written here rather than imported because model imports edit
// in its tests, so edit cannot import model.
func recomputeRefName(k sheet.Key) string {
	if k.Section == "" {
		return k.Cell
	}
	if k.Row != "" {
		prefix := k.Section
		switch k.Section {
		case vsd.SectionProperty:
			prefix = "Prop"
		case "Control":
			prefix = "Controls"
		}
		return prefix + "." + k.Row + "." + k.Cell
	}
	if k.RowIX >= 0 {
		switch k.Section {
		case vsd.SectionGeometry:
			return "Geometry" + strconv.Itoa(k.SectionIX+1) + "." + k.Cell + strconv.Itoa(k.RowIX)
		case vsd.SectionConnection:
			return "Connections." + k.Cell + strconv.Itoa(k.RowIX+1)
		case vsd.SectionLayer:
			return "Layer[" + strconv.Itoa(k.RowIX+1) + "]." + k.Cell
		case vsd.SectionCharacter:
			return "Char." + k.Cell + "[" + strconv.Itoa(k.RowIX+1) + "]"
		case vsd.SectionParagraph:
			return "Para." + k.Cell + "[" + strconv.Itoa(k.RowIX+1) + "]"
		case vsd.SectionField:
			return "Fields." + k.Cell + "[" + strconv.Itoa(k.RowIX+1) + "]"
		case "Scratch":
			return "Scratch." + k.Cell + strconv.Itoa(k.RowIX+1)
		default:
			return k.Section + "." + k.Cell + "[" + strconv.Itoa(k.RowIX+1) + "]"
		}
	}
	if k.Section == vsd.SectionGeometry {
		return "Geometry" + strconv.Itoa(k.SectionIX+1) + "." + k.Cell
	}
	return k.Section + "." + k.Cell
}

// recomputeParseRef parses a ShapeSheet reference name back to its
// sheet key, for a changed cell the shape stores under another
// spelling. It reports false for a qualified (Sheet.5!Width) or
// malformed name.
func recomputeParseRef(s string) (sheet.Key, bool) {
	if s == "" || strings.Contains(s, "!") {
		return sheet.Key{}, false
	}
	if !strings.ContainsAny(s, ".[") {
		return sheet.ShapeCell(s), true
	}
	if rest, ok := strings.CutPrefix(s, "Layer["); ok {
		end := strings.Index(rest, "]")
		if end < 0 {
			return sheet.Key{}, false
		}
		n, err := strconv.Atoi(rest[:end])
		if err != nil || n < 1 {
			return sheet.Key{}, false
		}
		return sheet.IndexedRowCell(vsd.SectionLayer, -1, n-1, strings.TrimPrefix(rest[end+1:], ".")), true
	}
	if strings.HasSuffix(s, "]") {
		open := strings.Index(s, "[")
		if open < 0 {
			return sheet.Key{}, false
		}
		n, err := strconv.Atoi(s[open+1 : len(s)-1])
		if err != nil || n < 1 {
			return sheet.Key{}, false
		}
		parts := strings.Split(s[:open], ".")
		if len(parts) != 2 {
			return sheet.Key{}, false
		}
		sec := parts[0]
		switch sec {
		case "Char":
			sec = vsd.SectionCharacter
		case "Para":
			sec = vsd.SectionParagraph
		case "Fields":
			sec = vsd.SectionField
		}
		return sheet.IndexedRowCell(sec, -1, n-1, parts[1]), true
	}
	parts := strings.Split(s, ".")
	switch len(parts) {
	case 2:
		if rest, ok := strings.CutPrefix(parts[0], vsd.SectionGeometry); ok && rest != "" {
			if n, err := strconv.Atoi(rest); err == nil && n >= 1 {
				if cell, row, ok := recomputeSplitNum(parts[1]); ok {
					return sheet.IndexedRowCell(vsd.SectionGeometry, n-1, row, cell), true
				}
				return sheet.Key{Section: vsd.SectionGeometry, SectionIX: n - 1, RowIX: -1, Cell: parts[1]}, true
			}
		}
		if parts[0] == "Connections" || parts[0] == "Scratch" {
			if cell, row, ok := recomputeSplitNum(parts[1]); ok {
				sec := "Scratch"
				if parts[0] == "Connections" {
					sec = vsd.SectionConnection
				}
				return sheet.IndexedRowCell(sec, -1, row-1, cell), true
			}
		}
		return sheet.Key{Section: parts[0], SectionIX: -1, RowIX: -1, Cell: parts[1]}, true
	case 3:
		sec := parts[0]
		switch sec {
		case "Prop":
			sec = vsd.SectionProperty
		case "Controls":
			sec = "Control"
		}
		return sheet.NamedRowCell(sec, parts[1], parts[2]), true
	}
	return sheet.Key{}, false
}

// recomputeSplitNum splits a trailing number off s: the letters and
// the number (X2 gives X and 2), reporting false when s has none.
func recomputeSplitNum(s string) (string, int, bool) {
	i := len(s)
	for i > 0 && ascii.IsDigit(rune(s[i-1])) {
		i--
	}
	if i == 0 || i == len(s) {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return "", 0, false
	}
	return s[:i], n, true
}

// recomputeEval evaluates the dependents of one shape in dependency
// order: values holds the computed values of the dependents done so
// far, and a reference to a dependent not yet done is unknown (so an
// order that misses one fails rather than reads a stale cached V).
type recomputeEval struct {
	cells     []recomputeCell
	fwd       [][]int
	dependent []bool
	refIndex  map[string]int
	values    []float64
	done      []bool
}

// run evaluates every dependent, returning false on the first one
// that cannot be evaluated (outside the closed grammar, an unknown
// or non-finite input, or a dependency cycle).
func (e *recomputeEval) run() bool {
	for i := range e.cells {
		if !e.dependent[i] || e.done[i] {
			continue
		}
		if !e.evalOne(i) {
			return false
		}
	}
	return true
}

// evalOne evaluates the dependent at root and, first, the dependents
// it references, with an explicit stack so a long chain recurses
// nowhere. It reports false for a cycle or a failed evaluation.
func (e *recomputeEval) evalOne(root int) bool {
	type frame struct {
		idx  int
		next int
	}
	visiting := map[int]bool{root: true}
	stack := []frame{{idx: root}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next < len(e.fwd[top.idx]) {
			dep := e.fwd[top.idx][top.next]
			top.next++
			if !e.dependent[dep] || e.done[dep] {
				continue
			}
			if visiting[dep] {
				return false
			}
			visiting[dep] = true
			stack = append(stack, frame{idx: dep})
			continue
		}
		v, err := formula.Eval(e.cells[top.idx].formula, e.cellValue)
		if err != nil {
			return false
		}
		e.values[top.idx] = v
		e.done[top.idx] = true
		delete(visiting, top.idx)
		stack = stack[:len(stack)-1]
	}
	return true
}

// cellValue is the formula.Eval cell callback: a reference resolves
// to the computed value of a dependent already done, or to the stored
// value of any other effective cell.
func (e *recomputeEval) cellValue(ref string) (float64, bool) {
	i, ok := e.refIndex[ascii.Lower(ref)]
	if !ok {
		return 0, false
	}
	if e.done[i] {
		return e.values[i], true
	}
	if e.dependent[i] {
		return 0, false
	}
	return e.cells[i].value, e.cells[i].hasValue
}

// recomputeSection names one Section of a shape: its name and index.
type recomputeSection struct {
	name string
	ix   int
}

// recomputeRow names one Row of a shape: its Section, its name or
// its index.
type recomputeRow struct {
	section recomputeSection
	row     string
	rowIX   int
}

// recomputeWriter materialises master-held dependents in the working
// page part, creating the Sections, Rows and Cells locally. The
// Sections, Rows and Cells it finds or creates are cached, and each
// container's next insert chains after the previous one, so N
// dependents on one shape cost one walk per container, not one per
// dependent.
type recomputeWriter struct {
	w        *Workspace
	part     string
	h        *core.Handle
	shape    *tree.Node
	sections map[recomputeSection]*tree.Node
	rows     map[recomputeRow]*tree.Node
	last     map[*tree.Node]*tree.Node
}

// newRecomputeWriter returns the writer for the shape at label in
// part, with its write handle.
func newRecomputeWriter(w *Workspace, part, label string, shape *tree.Node) *recomputeWriter {
	return &recomputeWriter{w: w, part: part, h: w.core.Target(part, label, []*tree.Node{shape}),
		shape: shape, sections: map[recomputeSection]*tree.Node{}, rows: map[recomputeRow]*tree.Node{}, last: map[*tree.Node]*tree.Node{}}
}

// materialise writes the dependent c, which the shape does not store,
// as a local <Cell N=.. V=v F="Inh"/> in its Section and Row.
func (rw *recomputeWriter) materialise(c *recomputeCell, v string) error {
	container := rw.shape
	typ := "v:Shape"
	if c.key.Section != "" {
		sec, err := rw.sectionFor(c.key)
		if err != nil {
			return err
		}
		container, typ = sec, "v:Section"
		if c.key.Row != "" || c.key.RowIX >= 0 {
			rowType := ""
			if c.master != nil {
				if row := c.master.node.Parent(); row != nil {
					rowType, _ = row.Attr("T")
				}
			}
			row, err := rw.rowFor(sec, c.key, rowType)
			if err != nil {
				return err
			}
			container, typ = row, "v:Row"
		}
	}
	frag := recomputeCellFragment(c.key.Cell, v, c.u)
	if _, err := rw.insert(container, typ, vsd.ElemCell, frag); err != nil {
		return fmt.Errorf("vsdx edit: cell %s: %w", c.ref, err)
	}
	return nil
}

// sectionFor returns the shape's Section of the cell's key, creating
// it in child order when the shape stores none.
func (rw *recomputeWriter) sectionFor(k sheet.Key) (*tree.Node, error) {
	sk := recomputeSection{name: k.Section, ix: k.SectionIX}
	if sec, ok := rw.sections[sk]; ok {
		return sec, nil
	}
	for c := range rw.shape.ElementsSeq() {
		if !isElem(c, vsd.ElemSection) {
			continue
		}
		if n, _ := c.Attr("N"); n == k.Section && recomputeIX(c) == k.SectionIX {
			rw.sections[sk] = c
			return c, nil
		}
	}
	frag := "<Section N='" + xmlscan.EscapeAttr(k.Section, '\'')
	if k.SectionIX >= 0 {
		frag += "' IX='" + strconv.Itoa(k.SectionIX)
	}
	frag += "'/>"
	sec, err := rw.insert(rw.shape, "v:Shape", vsd.ElemSection, frag)
	if err != nil {
		return nil, err
	}
	rw.sections[sk] = sec
	return sec, nil
}

// rowFor returns the section's Row of the cell's key, creating it
// when the section stores none, with the master's row type typ.
func (rw *recomputeWriter) rowFor(sec *tree.Node, k sheet.Key, typ string) (*tree.Node, error) {
	rk := recomputeRow{section: recomputeSection{name: k.Section, ix: k.SectionIX}, row: k.Row, rowIX: k.RowIX}
	if row, ok := rw.rows[rk]; ok {
		return row, nil
	}
	for c := range sec.ElementsSeq() {
		if !isElem(c, vsd.ElemRow) {
			continue
		}
		if name, ix, ok := recomputeRowID(c); ok && name == k.Row && ix == k.RowIX {
			rw.rows[rk] = c
			return c, nil
		}
	}
	var frag string
	if k.Row != "" {
		frag = "<Row N='" + xmlscan.EscapeAttr(k.Row, '\'') + "'/>"
	} else {
		frag = "<Row IX='" + strconv.Itoa(k.RowIX) + "'"
		if typ != "" {
			frag += " T='" + xmlscan.EscapeAttr(typ, '\'') + "'"
		}
		frag += "/>"
	}
	row, err := rw.insert(sec, "v:Section", vsd.ElemRow, frag)
	if err != nil {
		return nil, err
	}
	rw.rows[rk] = row
	return row, nil
}

// insert parses frag under container and inserts the element at the
// end of its slot in container (chaining after the previous insert
// into the same container), returning the inserted element.
func (rw *recomputeWriter) insert(container *tree.Node, typ, local, frag string) (*tree.Node, error) {
	nodes, err := rw.w.core.Parse(container, []byte(frag), nil)
	if err != nil {
		return nil, fmt.Errorf("vsdx edit: %s: %w", local, err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], local) {
		return nil, fmt.Errorf("vsdx edit: %s fragment is not a single %s element", local, local)
	}
	var pos core.Position
	if last := rw.last[container]; last != nil {
		pos = core.After(last, nil)
	} else {
		pos, err = rw.w.core.SlotEnd(container, typ, vsdxNS.Name(local), true, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("vsdx edit: %w", err)
		}
	}
	if err := rw.w.core.Insert(rw.part, pos, nodes[0], nil); err != nil {
		return nil, fmt.Errorf("vsdx edit: %w", err)
	}
	rw.last[container] = nodes[0]
	return nodes[0], nil
}

// recomputeCellFragment returns the materialised Cell of name with
// value v, its unit u (when it has one) and F="Inh" as XML.
func recomputeCellFragment(name, v, u string) string {
	var b strings.Builder
	b.WriteString("<Cell N='")
	b.WriteString(xmlscan.EscapeAttr(name, '\''))
	b.WriteString("' V='")
	b.WriteString(xmlscan.EscapeAttr(v, '\''))
	b.WriteString("'")
	if u != "" {
		b.WriteString(" U='")
		b.WriteString(xmlscan.EscapeAttr(u, '\''))
		b.WriteString("'")
	}
	b.WriteString(" F='Inh'/>")
	return b.String()
}
