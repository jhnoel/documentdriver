package formula

import (
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jhnoel/documentdriver/internal/corpustest"
	"github.com/jhnoel/documentdriver/internal/laws"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/model"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsdxtest"
)

// corpusSheet is one sheet (shape, page sheet or style sheet) as
// enumerated for the FL4 sweep, self-contained: its address and part
// for reporting, its cells in document order, and the master
// shape's cells by key for Inh formulas (nil when it has no master).
type corpusSheet struct {
	addr   string
	part   string
	cells  []corpusCell
	master map[sheet.Key]sheet.Cell
}

// corpusCell is one stored cell of a corpusSheet, in document order.
type corpusCell struct {
	key  sheet.Key
	cell sheet.Cell
	ref  string
}

// corpusCounts are one file's FL4 tally.
type corpusCounts struct {
	visio         bool
	sheets        int
	cells         int
	withFormula   int
	inh           int
	inhNoMaster   int
	inhEvaluated  int
	inGrammar     int
	eligible      int
	evaluated     int
	equal         int
	clamped       int
	outside       int
	refMissing    int
	refNonNumeric int
	cellNonNum    int
	evalErrors    int
	disagreements []string
	unreadable    []string
}

// classifiedEntry is one explained disagreement of the FL4 sweep:
// key is the file, part, shape, cell and F of the disagreement line
// (the test's disagreement format), and reason says why Visio's
// cached value legitimately differs from Eval on the cached
// inputs. A disagreement with no entry fails the test.
type classifiedEntry struct {
	key, reason string
}

// classifiedDisagreements is the FL4 classified table: empty,
// because the sweep of the 53 Visio-authored corpus drawings
// evaluates every eligible cell and, after the Char.Size 1pt
// clamp of formula.Clamp (four GUARD(0) Char.Size cells cache
// 1pt), every one agrees with its cached value within tolerance,
// so there is nothing to explain. A future disagreement is added
// here with its inspected reason, with the designer's approval in
// the commit message, or the test fails.
var classifiedDisagreements = []classifiedEntry{}

// isVisioAuthoredFile reports whether pkg's app.xml names
// Microsoft Visio as its Application (the Visio-authored subset
// of the corpus).
func isVisioAuthoredFile(pkg *opc.Package) bool {
	part, err := pkg.Part("/docProps/app.xml")
	if err != nil {
		return false
	}
	src, ok, err := part.OriginalString()
	if err != nil || !ok {
		return false
	}
	d, err := tree.ParseString(src)
	if err != nil || d.Root() == nil {
		return false
	}
	for _, e := range d.Root().Elements() {
		if e.Local() == "Application" {
			return strings.TrimSpace(e.Text()) == "Microsoft Visio"
		}
	}
	return false
}

// listPartName returns the pages or masters list part of d, or "".
func listPartName(d *doc.Document, want string) string {
	rels, err := d.Pkg.Rels(d.Part)
	if err != nil {
		return ""
	}
	for _, rel := range rels.All() {
		if !rel.External() && ctypes.Is(rel.Type, want) {
			if part, err := rels.TargetPart(rel); err == nil {
				return part
			}
		}
	}
	return ""
}

// enumFrame is one element waiting to be walked for cells, with the
// section and row context its Cell children inherit.
type enumFrame struct {
	node    *tree.Node
	section string
	secIX   int
	row     string
	rowIX   int
}

// enumerateCells returns the stored cells of root (a Shape,
// PageSheet or StyleSheet element) in document order, first cell
// per key, skipping cells, sections and rows sheet.Index skips.
// It walks sibling links backwards onto a stack, building no list
// of a node's children.
func enumerateCells(root *tree.Node) []corpusCell {
	if root == nil {
		return nil
	}
	var out []corpusCell
	seen := map[sheet.Key]bool{}
	stack := []enumFrame{{node: root, secIX: -1, rowIX: -1}}
	for len(stack) > 0 {
		fr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := fr.node
		switch n.Local() {
		case vsd.ElemCell:
			name, ok := n.Attr("N")
			if !ok || name == "" {
				continue
			}
			key := sheet.Key{Section: fr.section, SectionIX: fr.secIX, Row: fr.row, RowIX: fr.rowIX, Cell: name}
			if seen[key] {
				continue
			}
			seen[key] = true
			cell := sheet.Cell{N: name, Node: n}
			if v, ok := n.Attr("V"); ok {
				cell.V, cell.HasV = v, true
			}
			if v, ok := n.Attr("U"); ok {
				cell.U = v
			}
			if v, ok := n.Attr("F"); ok {
				cell.F, cell.HasF = v, true
			}
			if v, ok := n.Attr("E"); ok {
				cell.E = v
			}
			out = append(out, corpusCell{key: key, cell: cell, ref: model.CellKey(key)})
		case vsd.ElemSection:
			name, ok := n.Attr("N")
			if !ok || name == "" {
				continue
			}
			secIX := -1
			if v, ok := n.Attr("IX"); ok {
				ix, err := strconv.Atoi(v)
				if err != nil || ix < 0 {
					continue
				}
				secIX = ix
			}
			pushEnumChildren(&stack, n, enumFrame{section: name, secIX: secIX, rowIX: -1})
		case vsd.ElemRow:
			row, rowIX, ok := rowIdentityForCorpus(n)
			if !ok {
				continue
			}
			pushEnumChildren(&stack, n, enumFrame{section: fr.section, secIX: fr.secIX, row: row, rowIX: rowIX})
		case vsd.ElemShapes:
			// Nested shapes are separate sheets.
		default:
			pushEnumChildren(&stack, n, fr)
		}
	}
	return out
}

// rowIdentityForCorpus returns the name and index of a Row element
// for the FL4 enumeration: a named row has its name and index -1,
// an indexed row has an empty name and its index.
func rowIdentityForCorpus(row *tree.Node) (string, int, bool) {
	if name, ok := row.Attr("N"); ok && name != "" {
		return name, -1, true
	}
	if v, ok := row.Attr("IX"); ok {
		if ix, err := strconv.Atoi(v); err == nil && ix >= 0 {
			return "", ix, true
		}
	}
	return "", 0, false
}

// pushEnumChildren pushes the element children of n onto stack in
// reverse document order, each with context ctx (its node replaced
// by the child), walking sibling links backwards and building no
// list of children.
func pushEnumChildren(stack *[]enumFrame, n *tree.Node, ctx enumFrame) {
	for c := n.LastChild(); c != nil; c = c.PrevSibling() {
		if c.Kind() != tree.ElementNode {
			continue
		}
		f := ctx
		f.node = c
		*stack = append(*stack, f)
	}
}

// shapeFrame is one element waiting to be walked for shapes, with
// the master the enclosing group instance carries.
type shapeFrame struct {
	node      *tree.Node
	master    uint32
	hasMaster bool
}

// walkShapes walks the shapes of contents (a PageContents or
// MasterContents tree) in document order, calling fn for each
// shape with its element, its effective master (its own Master
// attribute, or the one a group instance above it carries) and its
// MasterShape attribute. It walks sibling links backwards onto a
// stack, building no list of a node's children.
func walkShapes(contents *tree.Document, fn func(node *tree.Node, master uint32, hasMaster bool, masterShape uint32, hasMShape bool)) {
	if contents == nil || contents.Root() == nil {
		return
	}
	stack := []shapeFrame{{node: contents.Root()}}
	for len(stack) > 0 {
		fr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := fr.node
		master, hasMaster, ms, hasMS := fr.master, fr.hasMaster, uint32(0), false
		if n.Local() == vsd.ElemShape {
			for name, value := range n.AttrsSeq() {
				switch name {
				case "Master":
					if u, err := strconv.ParseUint(value, 10, 32); err == nil {
						master, hasMaster = uint32(u), true
					}
				case "MasterShape":
					if u, err := strconv.ParseUint(value, 10, 32); err == nil {
						ms, hasMS = uint32(u), true
					}
				}
			}
			fn(n, master, hasMaster, ms, hasMS)
		}
		for c := n.LastChild(); c != nil; c = c.PrevSibling() {
			if c.Kind() != tree.ElementNode {
				continue
			}
			stack = append(stack, shapeFrame{node: c, master: master, hasMaster: hasMaster})
		}
	}
}

// uintAttrForCorpus parses element n's attribute name as a uint32.
func uintAttrForCorpus(n *tree.Node, name string) (uint32, bool) {
	v, ok := n.Attr(name)
	if !ok {
		return 0, false
	}
	u, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(u), true
}

// sheetsOfFile extracts every sheet of the opened document d: page
// sheets, master page sheets, style sheets, and every shape sheet
// of every page and master, in a deterministic order. A page or
// master whose contents cannot be read, and a shape whose ID does
// not parse, are recorded in unreadable.
func sheetsOfFile(d *doc.Document, r *inherit.Resolver) (sheets []corpusSheet, unreadable []string) {
	pagesPart := listPartName(d, ctypes.RelPages)
	mastersPart := listPartName(d, ctypes.RelMasters)
	for i := range d.Pages {
		e := &d.Pages[i]
		if e.PageSheet == nil {
			continue
		}
		sheets = append(sheets, corpusSheet{
			addr:  vsd.PageID(e.ID, pageDupForCorpus(d, e)),
			part:  pagesPart,
			cells: enumerateCells(e.PageSheet),
		})
	}
	for i := range d.Masters {
		m := &d.Masters[i]
		if m.PageSheet == nil {
			continue
		}
		sheets = append(sheets, corpusSheet{
			addr:  vsd.MasterID(m.ID),
			part:  mastersPart,
			cells: enumerateCells(m.PageSheet),
		})
	}
	ids := make([]uint32, 0, len(d.Styles))
	for id := range d.Styles {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		st := d.Styles[id]
		if st == nil || st.Node == nil {
			continue
		}
		sheets = append(sheets, corpusSheet{
			addr:  fmt.Sprintf("style%d", id),
			part:  d.Part,
			cells: enumerateCells(st.Node),
		})
	}
	masterCache := map[*tree.Node]map[sheet.Key]sheet.Cell{}
	for i := range d.Pages {
		e := &d.Pages[i]
		contents, err := d.PageContents(e)
		if err != nil {
			unreadable = append(unreadable, fmt.Sprintf("%s: %v", vsd.PageID(e.ID, pageDupForCorpus(d, e)), err))
			continue
		}
		container := vsd.PageID(e.ID, pageDupForCorpus(d, e))
		sh, noID := shapeSheetsOf(contents, container, e.Part, r, masterCache)
		sheets = append(sheets, sh...)
		unreadable = append(unreadable, noID...)
	}
	for i := range d.Masters {
		m := &d.Masters[i]
		contents, err := d.MasterContents(m.ID)
		if err != nil {
			unreadable = append(unreadable, fmt.Sprintf("%s: %v", vsd.MasterID(m.ID), err))
			continue
		}
		container := vsd.MasterID(m.ID)
		sh, noID := shapeSheetsOf(contents, container, m.Part, r, masterCache)
		sheets = append(sheets, sh...)
		unreadable = append(unreadable, noID...)
	}
	return sheets, unreadable
}

// pageDupForCorpus returns the 1-based occurrence of e among
// d.Pages with its ID.
func pageDupForCorpus(d *doc.Document, e *doc.PageEntry) int {
	dup := 1
	for i := range d.Pages {
		if &d.Pages[i] == e {
			return dup
		}
		if d.Pages[i].ID == e.ID {
			dup++
		}
	}
	return 1
}

// shapeSheetsOf returns the shape sheets of one contents tree, in
// document order, with the master cells each shape inherits from
// for its Inh formulas (cached per master shape node in cache),
// and a note for each shape whose ID does not parse.
func shapeSheetsOf(contents *tree.Document, container, part string, r *inherit.Resolver, cache map[*tree.Node]map[sheet.Key]sheet.Cell) (out []corpusSheet, noID []string) {
	seen := map[uint32]int{}
	walkShapes(contents, func(node *tree.Node, master uint32, hasMaster bool, ms uint32, hasMS bool) {
		id, ok := uintAttrForCorpus(node, "ID")
		if !ok {
			noID = append(noID, container+": shape id does not parse")
			return
		}
		seen[id]++
		cs := corpusSheet{
			addr:  vsd.ShapeID(container, id, seen[id]),
			part:  part,
			cells: enumerateCells(node),
		}
		if hasMaster {
			s := &inherit.Shape{Master: master, HasMaster: true, MasterShape: ms, HasMShape: hasMS}
			if msh, ok := r.MasterShape(s); ok && msh != nil && msh.Sheet != nil && msh.Sheet.Node != nil {
				if cached, hit := cache[msh.Sheet.Node]; hit {
					cs.master = cached
				} else {
					m := map[sheet.Key]sheet.Cell{}
					for _, cc := range enumerateCells(msh.Sheet.Node) {
						m[cc.key] = cc.cell
					}
					cache[msh.Sheet.Node] = m
					cs.master = m
				}
			}
		}
		out = append(out, cs)
	})
	return out, noID
}

// refValue is one same-sheet cell a formula reference resolves to:
// its cached numeric value, or numeric false when the cell exists
// but its V is not a finite number.
type refValue struct {
	v       float64
	numeric bool
}

// buildRefMap builds the reference map of a sheet: for each cell
// its CellKey spelling (the reference syntax where Visio has one)
// and its shorthand spellings, lower-cased for the case-insensitive
// lookup Visio reads names with. A named row's Value or X cell
// also answers to the row spelling (User.LockDate, Controls.Row_1),
// and an indexed row 0 also answers without its [1] (Char.Size).
func buildRefMap(cells []corpusCell) map[string]refValue {
	m := make(map[string]refValue, len(cells)*2)
	for _, cc := range cells {
		v, numeric := float64(0), false
		if cc.cell.HasV {
			v, numeric = vsd.Number(cc.cell.V)
		}
		rv := refValue{v: v, numeric: numeric}
		add := func(spelling string) {
			key := strings.ToLower(spelling)
			if _, dup := m[key]; !dup {
				m[key] = rv
			}
		}
		add(cc.ref)
		if cc.key.Row != "" && (cc.key.Cell == "Value" || cc.key.Cell == "X") {
			if short, ok := strings.CutSuffix(cc.ref, "."+cc.key.Cell); ok {
				add(short)
			}
		}
		if strings.HasSuffix(cc.ref, "[1]") {
			add(strings.TrimSuffix(cc.ref, "[1]"))
		}
	}
	return m
}

// grammarNoRefs reports whether f, which names no same-sheet
// reference, is in the closed grammar: Eval with a dummy cell
// does not report ErrOutsideGrammar. It is the constant case of
// the grammar check, for which Refs returns nil.
func grammarNoRefs(f string) bool {
	_, err := Eval(f, func(string) (float64, bool) { return 0, true })
	return !errors.Is(err, ErrOutsideGrammar)
}

// sweepSheet evaluates the in-grammar, same-sheet, numeric cells
// of one sheet and tallies the result in c, appending a
// disagreement line (part, shape, cell, F, cached and evaluated
// values) for every eligible cell that Eval errors on or
// disagrees with beyond tolerance. A cell whose reference is not
// a stored cell of the same sheet, or whose cached value is not
// numeric, is not eligible and is tallied as such; an Inh cell
// whose master holds no formula for it is tallied separately.
func sweepSheet(c *corpusCounts, sh corpusSheet) {
	c.sheets++
	c.cells += len(sh.cells)
	refs := buildRefMap(sh.cells)
	for _, cc := range sh.cells {
		if !cc.cell.HasF || cc.cell.F == "" || cc.cell.F == vsd.FormulaNone {
			continue
		}
		c.withFormula++
		formula := cc.cell.F
		isInh := false
		if formula == vsd.FormulaInherited {
			isInh = true
			c.inh++
			mc, ok := sh.master[cc.key]
			if !ok || !mc.HasF || mc.F == "" || mc.F == vsd.FormulaNone || mc.F == vsd.FormulaInherited {
				c.inhNoMaster++
				continue
			}
			formula = mc.F
		}
		formulaRefs := Refs(formula)
		if formulaRefs == nil && !grammarNoRefs(formula) {
			c.outside++
			continue
		}
		c.inGrammar++
		want, ok := vsd.Number(cc.cell.V)
		if !cc.cell.HasV || !ok {
			c.cellNonNum++
			continue
		}
		bad := false
		for _, ref := range formulaRefs {
			rv, hit := refs[strings.ToLower(ref)]
			if !hit {
				c.refMissing++
				bad = true
				break
			}
			if !rv.numeric {
				c.refNonNumeric++
				bad = true
				break
			}
		}
		if bad {
			continue
		}
		c.eligible++
		got, err := Eval(formula, func(ref string) (float64, bool) {
			rv, hit := refs[strings.ToLower(ref)]
			return rv.v, hit && rv.numeric
		})
		if err != nil {
			c.evalErrors++
			c.disagreements = append(c.disagreements, fmt.Sprintf("%s %s %s F=%q cached=%v eval-error=%v", sh.part, sh.addr, cc.ref, cc.cell.F, want, err))
			continue
		}
		c.evaluated++
		if isInh {
			c.inhEvaluated++
		}
		if clamped := Clamp(cc.ref, got); clamped != got {
			c.clamped++
			got = clamped
		}
		diff := math.Abs(got - want)
		if diff <= 1e-12 || diff <= 1e-9*math.Abs(want) {
			c.equal++
			continue
		}
		c.disagreements = append(c.disagreements, fmt.Sprintf("%s %s %s F=%q cached=%v eval=%v", sh.part, sh.addr, cc.ref, cc.cell.F, want, got))
	}
}

// sweepFile sweeps every sheet of the Visio-authored drawing at
// path, returning its tally with visio set. A file that is not a
// package, or is not Visio-authored, returns visio false and is
// not in the sweep; a Visio file that cannot be opened as a
// Visio document records the error in unreadable.
func sweepFile(path string) (counts corpusCounts) {
	pkg, err := opc.Open(path)
	if err != nil {
		return counts
	}
	defer func() { _ = pkg.Close() }()
	if !isVisioAuthoredFile(pkg) {
		return counts
	}
	counts.visio = true
	d, err := doc.Open(pkg)
	if err != nil {
		counts.unreadable = append(counts.unreadable, "doc.Open: "+err.Error())
		return counts
	}
	r := inherit.New(d)
	sheets, unreadable := sheetsOfFile(d, r)
	counts.unreadable = append(counts.unreadable, unreadable...)
	for _, sh := range sheets {
		sweepSheet(&counts, sh)
	}
	return counts
}

// classifiedReason reports why the disagreement line dg is
// explained by the classified table: its file, part, shape, cell
// and F prefix matches an entry, whose reason is returned.
func classifiedReason(dg string) (string, bool) {
	for _, e := range classifiedDisagreements {
		if strings.HasPrefix(dg, e.key) {
			return e.reason, true
		}
	}
	return "", false
}

// TestEvalCorpus checks the FL4 corpus row: over the Visio-authored
// VSDX_CORPUS drawings, every cell whose effective formula is in
// the closed grammar and whose references are numeric cached cells
// of the same sheet evaluates (Eval) to its cached value within
// 1e-9 relative (1e-12 absolute near 0), master formulas against
// master cells and Inh instance cells against the instance's
// cached inputs and the master's formula. Every disagreement is
// listed with file, part, shape, cell, F and both values, and is
// accepted only when the classified table explains it; any other
// fails. Counts are logged. It skips without VSDX_CORPUS and fails
// naming the path when VSDX_CORPUS is set but holds no corpus.
func TestEvalCorpus(t *testing.T) {
	dir := lexCorpusDir()
	if dir == "" {
		if d := os.Getenv("VSDX_CORPUS"); d != "" {
			t.Fatalf("VSDX_CORPUS %s holds no vsdx corpus (no repos directory under it)", d)
		}
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	paths := lexCorpusDrawings(dir)
	if len(paths) == 0 {
		t.Fatalf("VSDX_CORPUS %s holds no vsdx drawings under %s", os.Getenv("VSDX_CORPUS"), dir)
	}
	files := corpustest.Files(paths, dir)
	if len(corpustest.Shard(t, files)) == 0 {
		t.Skip("no corpus drawings in this CORPUS_SHARD")
	}
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *corpusCounts) {
		counts := sweepFile(f.Path)
		if !counts.visio {
			return
		}
		for _, u := range counts.unreadable {
			t.Errorf("%s: %s", f.Name, u)
		}
		for i, dg := range counts.disagreements {
			counts.disagreements[i] = f.Name + " " + dg
		}
		*r = counts
	})
	var total corpusCounts
	var visioFiles, classified int
	for _, r := range res {
		if !r.Ran {
			continue
		}
		if r.Value.visio {
			visioFiles++
		}
		total.sheets += r.Value.sheets
		total.cells += r.Value.cells
		total.withFormula += r.Value.withFormula
		total.inh += r.Value.inh
		total.inhNoMaster += r.Value.inhNoMaster
		total.inhEvaluated += r.Value.inhEvaluated
		total.inGrammar += r.Value.inGrammar
		total.eligible += r.Value.eligible
		total.evaluated += r.Value.evaluated
		total.equal += r.Value.equal
		total.clamped += r.Value.clamped
		total.outside += r.Value.outside
		total.refMissing += r.Value.refMissing
		total.refNonNumeric += r.Value.refNonNumeric
		total.cellNonNum += r.Value.cellNonNum
		total.evalErrors += r.Value.evalErrors
		total.disagreements = append(total.disagreements, r.Value.disagreements...)
	}
	for _, dg := range total.disagreements {
		if reason, ok := classifiedReason(dg); ok {
			classified++
			t.Logf("classified disagreement: %s: %s", dg, reason)
			continue
		}
		t.Errorf("unexplained disagreement: %s", dg)
		t.Logf("disagreement: %s", dg)
	}
	t.Logf("vsdx FL4 corpus: %d Visio files, %d sheets, %d cells, %d with formula, %d Inh (%d without a master formula, %d evaluated), %d in grammar, %d eligible, %d evaluated, %d equal, %d clamped, %d classified, %d outside grammar, %d ref missing, %d ref non-numeric, %d cell non-numeric, %d eval errors, %d disagreements",
		visioFiles, total.sheets, total.cells, total.withFormula, total.inh, total.inhNoMaster, total.inhEvaluated, total.inGrammar, total.eligible, total.evaluated, total.equal, total.clamped, classified, total.outside, total.refMissing, total.refNonNumeric, total.cellNonNum, total.evalErrors, len(total.disagreements))
	if !corpustest.Sharded() {
		if visioFiles == 0 {
			t.Error("corpus holds no Visio-authored drawings")
		}
		if total.eligible == 0 {
			t.Error("corpus holds no eligible cells")
		}
		if total.clamped != 4 {
			t.Errorf("clamped %d, want 4 (the GUARD(0) Char.Size cells)", total.clamped)
		}
	}
}

// TestEvalCorpusScaling checks the FL4 scaling row: sweeping N
// and 4N copies of one file's sheets (N = laws.Small, 4N =
// laws.Large) allocates at most 5.5 times as much (laws.Scales)
// and the laws.TimeScales time ratio is at most 8.
func TestEvalCorpusScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		pkg := vsdxtest.OpenPackage(t, "v2-master")
		d, err := doc.Open(pkg)
		if err != nil {
			return nil, err
		}
		r := inherit.New(d)
		sheets, unreadable := sheetsOfFile(d, r)
		if len(unreadable) != 0 {
			return nil, fmt.Errorf("v2-master unreadable: %v", unreadable)
		}
		if len(sheets) == 0 {
			return nil, fmt.Errorf("v2-master holds no sheets")
		}
		var chosen corpusSheet
		found := false
		for _, sh := range sheets {
			var probe corpusCounts
			sweepSheet(&probe, sh)
			if probe.evaluated > 0 {
				chosen, found = sh, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("v2-master holds no sheet with an evaluated cell")
		}
		copies := make([]corpusSheet, n)
		for i := range copies {
			copies[i] = chosen
		}
		var want corpusCounts
		for _, sh := range copies {
			sweepSheet(&want, sh)
		}
		if want.evaluated == 0 || want.equal != want.evaluated {
			return nil, fmt.Errorf("v2-master sheet: evaluated %d, equal %d, want equal evaluated pairs", want.evaluated, want.equal)
		}
		return func() error {
			var got corpusCounts
			for _, sh := range copies {
				sweepSheet(&got, sh)
			}
			if got.evaluated != want.evaluated || got.equal != want.equal {
				return fmt.Errorf("sweep of %d copies: evaluated %d, equal %d; want %d, %d", n, got.evaluated, got.equal, want.evaluated, want.equal)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx FL4 corpus sweep copies", setup)
	laws.TimeScales(t, "vsdx FL4 corpus sweep copies", setup)
}

// oneDCellNames are the 1-D transform and trigger cell names of CC1.
var oneDCellNames = []string{
	"Width", "Height", "PinX", "PinY", "LocPinX", "LocPinY", "Angle",
	"BeginX", "BeginY", "EndX", "EndY", "BegTrigger", "EndTrigger",
}

// oneDCounts are one file's CC1 1-D tally.
type oneDCounts struct {
	visio         bool
	shapes        int
	cells         int
	constant      int
	inh           int
	inhNoMaster   int
	evaluable     int
	evaluated     int
	equal         int
	clamped       int
	gluePoint     int
	glueWhole     int
	glueTrigger   int
	refused       int
	refMissing    int
	refNonNumeric int
	cellNonNum    int
	evalErrors    int
	disagreements []string
	unreadable    []string
	forms         map[string]int
}

// isOneDCell reports whether name is a CC1 1-D cell.
func isOneDCell(name string) bool {
	return slices.Contains(oneDCellNames, name)
}

// oneDFormName returns the aggregated form name of a stored 1-D
// formula for the CC1 log: PAR and _XFTRIGGER sheet numbers and
// Width*0.5-like coefficients are normalised, as in the CC1 table.
func oneDFormName(cell, f string, hasF bool) string {
	if !hasF {
		return cell + " constant"
	}
	switch {
	case strings.HasPrefix(f, "PAR(PNT(Sheet."):
		return cell + " PAR(PNT(Sheet.N!Connections.Xk,...))"
	case strings.HasPrefix(f, "_XFTRIGGER(Sheet."):
		return cell + " _XFTRIGGER(Sheet.N!EventXFMod)"
	case strings.HasPrefix(f, "Width*"):
		return cell + " Width*0.5"
	case strings.HasPrefix(f, "Height*"):
		return cell + " Height*0.5"
	case strings.HasPrefix(f, "GUARD(") && strings.Contains(f, "DL)"):
		return cell + " GUARD(nDL)"
	default:
		return cell + " " + f
	}
}

// sweepOneDSheet tallies the 1-D cells of one sheet in c: a sheet
// with a BeginX cell is a 1-D shape; its Width, Height, Pin, LocPin,
// Angle, Begin, End and trigger cells are classified as constant,
// Inh, evaluable (in the closed grammar, evaluated against the
// sheet's cached cells), a glue form (GlueForm) or refused, and an
// evaluable cell that Eval errors on or disagrees with beyond
// tolerance is a disagreement.
func sweepOneDSheet(c *oneDCounts, sh corpusSheet) {
	hasBegin := false
	for _, cc := range sh.cells {
		if cc.key.Cell == "BeginX" {
			hasBegin = true
			break
		}
	}
	if !hasBegin {
		return
	}
	c.shapes++
	if c.forms == nil {
		c.forms = map[string]int{}
	}
	refs := buildRefMap(sh.cells)
	for _, cc := range sh.cells {
		if !isOneDCell(cc.key.Cell) {
			continue
		}
		c.cells++
		c.forms[oneDFormName(cc.key.Cell, cc.cell.F, cc.cell.HasF)]++
		if !cc.cell.HasF || cc.cell.F == "" || cc.cell.F == vsd.FormulaNone {
			c.constant++
			continue
		}
		formula := cc.cell.F
		if formula == vsd.FormulaInherited {
			c.inh++
			mc, ok := sh.master[cc.key]
			if !ok || !mc.HasF || mc.F == "" || mc.F == vsd.FormulaNone || mc.F == vsd.FormulaInherited {
				c.inhNoMaster++
				continue
			}
			formula = mc.F
		}
		if g, ok := GlueForm(formula); ok {
			switch g.Kind {
			case GluePoint:
				c.gluePoint++
			case GlueWhole:
				c.glueWhole++
			case GlueTrigger:
				c.glueTrigger++
			}
			continue
		}
		formulaRefs := Refs(formula)
		if formulaRefs == nil && !grammarNoRefs(formula) {
			c.refused++
			continue
		}
		c.evaluable++
		want, ok := vsd.Number(cc.cell.V)
		if !cc.cell.HasV || !ok {
			c.cellNonNum++
			continue
		}
		bad := false
		for _, ref := range formulaRefs {
			rv, hit := refs[strings.ToLower(ref)]
			if !hit {
				c.refMissing++
				bad = true
				break
			}
			if !rv.numeric {
				c.refNonNumeric++
				bad = true
				break
			}
		}
		if bad {
			continue
		}
		got, err := Eval(formula, func(ref string) (float64, bool) {
			rv, hit := refs[strings.ToLower(ref)]
			return rv.v, hit && rv.numeric
		})
		if err != nil {
			c.evalErrors++
			c.disagreements = append(c.disagreements, fmt.Sprintf("%s %s %s F=%q cached=%v eval-error=%v", sh.part, sh.addr, cc.ref, cc.cell.F, want, err))
			continue
		}
		c.evaluated++
		if clamped := Clamp(cc.ref, got); clamped != got {
			c.clamped++
			got = clamped
		}
		diff := math.Abs(got - want)
		if diff <= 1e-12 || diff <= 1e-9*math.Abs(want) {
			c.equal++
			continue
		}
		c.disagreements = append(c.disagreements, fmt.Sprintf("%s %s %s F=%q cached=%v eval=%v", sh.part, sh.addr, cc.ref, cc.cell.F, want, got))
	}
}

// sweepOneDFile sweeps the 1-D sheets of the Visio-authored drawing
// at path, returning its tally. A file that is not Visio-authored
// returns visio false and is not in the sweep.
func sweepOneDFile(path string) (counts oneDCounts) {
	counts.forms = map[string]int{}
	pkg, err := opc.Open(path)
	if err != nil {
		return counts
	}
	defer func() { _ = pkg.Close() }()
	if !isVisioAuthoredFile(pkg) {
		return counts
	}
	counts.visio = true
	d, err := doc.Open(pkg)
	if err != nil {
		counts.unreadable = append(counts.unreadable, "doc.Open: "+err.Error())
		return counts
	}
	r := inherit.New(d)
	sheets, unreadable := sheetsOfFile(d, r)
	counts.unreadable = append(counts.unreadable, unreadable...)
	for _, sh := range sheets {
		sweepOneDSheet(&counts, sh)
	}
	return counts
}

// TestOneDCorpus checks the CC1 FL4 re-run row: over the
// Visio-authored VSDX_CORPUS drawings, every 1-D cell formula
// (Width, Height, Pin, LocPin, Angle, Begin, End and the triggers
// of a shape with a BeginX cell) is classified as evaluable, a
// glue form or refused, and the evaluable ones reproduce the
// stored V within 1e-9 (relative for angles), with 0
// disagreements. Counts are logged and match the CC1 table:
// Width GUARD(EndX-BeginX) 66 cells, SQRT 16, GUARD(nDL) 11;
// Height GUARD(EndY-BeginY) 48, GUARD(nDL) 27; PinX/PinY
// (Begin+End)/2 and GUARD of it 37 each; LocPin Width*0.5 and
// GUARD of it 37 each; Angle ATAN2 16 and GUARD(0DA) 21; point
// glue 168 cells, whole glue 52, triggers 114, and 1 refused
// (a SETATREF Height). It skips without VSDX_CORPUS and fails
// naming the path when VSDX_CORPUS is set but holds no corpus.
func TestOneDCorpus(t *testing.T) {
	dir := lexCorpusDir()
	if dir == "" {
		if d := os.Getenv("VSDX_CORPUS"); d != "" {
			t.Fatalf("VSDX_CORPUS %s holds no vsdx corpus (no repos directory under it)", d)
		}
		t.Skip("no vsdx corpus (set VSDX_CORPUS)")
	}
	paths := lexCorpusDrawings(dir)
	if len(paths) == 0 {
		t.Fatalf("VSDX_CORPUS %s holds no vsdx drawings under %s", os.Getenv("VSDX_CORPUS"), dir)
	}
	files := corpustest.Files(paths, dir)
	if len(corpustest.Shard(t, files)) == 0 {
		t.Skip("no corpus drawings in this CORPUS_SHARD")
	}
	res := corpustest.Run(t, files, func(t *testing.T, f corpustest.File, r *oneDCounts) {
		counts := sweepOneDFile(f.Path)
		if !counts.visio {
			return
		}
		for _, u := range counts.unreadable {
			t.Errorf("%s: %s", f.Name, u)
		}
		for i, dg := range counts.disagreements {
			counts.disagreements[i] = f.Name + " " + dg
		}
		*r = counts
	})
	var total oneDCounts
	var visioFiles int
	for _, r := range res {
		if !r.Ran {
			continue
		}
		if r.Value.visio {
			visioFiles++
		}
		total.shapes += r.Value.shapes
		total.cells += r.Value.cells
		total.constant += r.Value.constant
		total.inh += r.Value.inh
		total.inhNoMaster += r.Value.inhNoMaster
		total.evaluable += r.Value.evaluable
		total.evaluated += r.Value.evaluated
		total.equal += r.Value.equal
		total.clamped += r.Value.clamped
		total.gluePoint += r.Value.gluePoint
		total.glueWhole += r.Value.glueWhole
		total.glueTrigger += r.Value.glueTrigger
		total.refused += r.Value.refused
		total.refMissing += r.Value.refMissing
		total.refNonNumeric += r.Value.refNonNumeric
		total.cellNonNum += r.Value.cellNonNum
		total.evalErrors += r.Value.evalErrors
		total.disagreements = append(total.disagreements, r.Value.disagreements...)
		if total.forms == nil {
			total.forms = map[string]int{}
		}
		for k, v := range r.Value.forms {
			total.forms[k] += v
		}
	}
	for _, dg := range total.disagreements {
		t.Errorf("disagreement: %s", dg)
	}
	t.Logf("vsdx CC1 1-D corpus: %d Visio files, %d shapes, %d cells, %d constant, %d Inh (%d without a master formula), %d evaluable, %d evaluated, %d equal, %d clamped, point glue %d, whole glue %d, trigger glue %d, %d refused, %d ref missing, %d ref non-numeric, %d cell non-numeric, %d eval errors, %d disagreements",
		visioFiles, total.shapes, total.cells, total.constant, total.inh, total.inhNoMaster, total.evaluable, total.evaluated, total.equal, total.clamped, total.gluePoint, total.glueWhole, total.glueTrigger, total.refused, total.refMissing, total.refNonNumeric, total.cellNonNum, total.evalErrors, len(total.disagreements))
	keys := make([]string, 0, len(total.forms))
	for k := range total.forms {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		t.Logf("form %q: %d", k, total.forms[k])
	}
	if !corpustest.Sharded() {
		if visioFiles == 0 {
			t.Error("corpus holds no Visio-authored drawings")
		}
		if total.shapes == 0 {
			t.Error("corpus holds no 1-D shapes")
		}
		if total.evaluated != total.evaluable || total.equal != total.evaluated {
			t.Errorf("evaluated %d, equal %d, evaluable %d; want equal evaluated pairs", total.evaluated, total.equal, total.evaluable)
		}
		// The table counts, summed over page and master cells:
		// each shape stores X and Y cells with the same formula,
		// so a per-shape table count is doubled in cells.
		wantForms := map[string]int{
			"Width GUARD(EndX-BeginX)":                               66,
			"Width SQRT((EndX-BeginX)^2+(EndY-BeginY)^2)":            16,
			"Width GUARD(nDL)":                                       11,
			"Height GUARD(EndY-BeginY)":                              48,
			"Height GUARD(nDL)":                                      27,
			"PinX (BeginX+EndX)/2":                                   16,
			"PinY (BeginY+EndY)/2":                                   16,
			"PinX GUARD((BeginX+EndX)/2)":                            21,
			"PinY GUARD((BeginY+EndY)/2)":                            21,
			"LocPinX Width*0.5":                                      16,
			"LocPinX GUARD(Width*0.5)":                               21,
			"Angle ATAN2(EndY-BeginY,EndX-BeginX)":                   16,
			"Angle GUARD(0DA)":                                       21,
			"BeginX _WALKGLUE(BegTrigger,EndTrigger,WalkPreference)": 9,
			"EndX _WALKGLUE(EndTrigger,BegTrigger,WalkPreference)":   17,
			"BegTrigger _XFTRIGGER(Sheet.N!EventXFMod)":              50,
			"BegTrigger _XFTRIGGER(EventXFMod)":                      7,
			"EndTrigger _XFTRIGGER(Sheet.N!EventXFMod)":              50,
			"EndTrigger _XFTRIGGER(EventXFMod)":                      7,
			"PinX Inh":                                               57,
			"PinY Inh":                                               57,
			"LocPinX Inh":                                            56,
		}
		for k, want := range wantForms {
			if got := total.forms[k]; got != want {
				t.Errorf("form %q: %d, want %d (CC1 table)", k, got, want)
			}
		}
		if total.gluePoint != 168 || total.glueWhole != 52 || total.glueTrigger != 114 {
			t.Errorf("glue point %d, whole %d, trigger %d; want 168, 52, 114", total.gluePoint, total.glueWhole, total.glueTrigger)
		}
		if total.refused != 1 {
			t.Errorf("refused %d, want 1 (the SETATREF Height)", total.refused)
		}
	}
}

// TestOneDCorpusScaling checks the scaling row for the 1-D sweep:
// sweeping N and 4N copies of one 1-D sheet (N = laws.Small,
// 4N = laws.Large) allocates at most 5.5 times as much
// (laws.Scales) and the laws.TimeScales time ratio is at most 8.
func TestOneDCorpusScaling(t *testing.T) {
	setup := func(n int) (func() error, error) {
		mk := func(name, v, f string) corpusCell {
			cell := sheet.Cell{N: name, V: v, HasV: true}
			if f != "" {
				cell.F, cell.HasF = f, true
			}
			return corpusCell{
				key:  sheet.Key{Section: "Transform", SectionIX: 0, RowIX: -1, Cell: name},
				cell: cell,
				ref:  name,
			}
		}
		sh := corpusSheet{
			addr: "page0/s1",
			part: "/visio/pages/page1.xml",
			cells: []corpusCell{
				mk("BeginX", "1", ""),
				mk("BeginY", "1", ""),
				mk("EndX", "3", ""),
				mk("EndY", "1", ""),
				mk("Width", "2", "GUARD(EndX-BeginX)"),
				mk("Height", "0", "GUARD(EndY-BeginY)"),
				mk("PinX", "2", "(BeginX+EndX)/2"),
			},
		}
		copies := make([]corpusSheet, n)
		for i := range copies {
			copies[i] = sh
		}
		var want oneDCounts
		for _, s := range copies {
			sweepOneDSheet(&want, s)
		}
		if want.evaluated == 0 || want.equal != want.evaluated {
			return nil, fmt.Errorf("1-D sheet: evaluated %d, equal %d, want equal evaluated pairs", want.evaluated, want.equal)
		}
		return func() error {
			var got oneDCounts
			for _, s := range copies {
				sweepOneDSheet(&got, s)
			}
			if got.evaluated != want.evaluated || got.equal != want.equal {
				return fmt.Errorf("sweep of %d copies: evaluated %d, equal %d; want %d, %d", n, got.evaluated, got.equal, want.evaluated, want.equal)
			}
			return nil
		}, nil
	}
	laws.Scales(t, "vsdx CC1 1-D corpus sweep copies", setup)
	laws.TimeScales(t, "vsdx CC1 1-D corpus sweep copies", setup)
}
