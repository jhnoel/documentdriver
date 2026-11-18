package ops

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// resolvePage returns the page address at names, resolving $alias
// (with what may follow it) through ResolveAlias: a resolved
// address that is no page (a shape, layer, prop or field suffix
// that names one) is operation_failed at at.
func resolvePage(at string, aliases map[string]string) (string, error) {
	resolved, err := ResolveAlias(at, aliases, "at")
	if err != nil {
		return "", err
	}
	if a, err := vsd.Parse(resolved); err != nil || a.Kind != vsd.KindPage || a.InMaster {
		return "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a page address", resolved)
	}
	return resolved, nil
}

// sizeInches returns the plan size in inches (units pt or in).
func sizeInches(size []float64, units string) (float64, float64) {
	if units == "in" {
		return size[0], size[1]
	}
	return size[0] / vsd.PointsPerInch, size[1] / vsd.PointsPerInch
}

// sizeOkay reports whether size is two finite positive numbers.
func sizeOkay(size []float64) bool {
	return len(size) == 2 && size[0] > 0 && size[1] > 0 &&
		!math.IsNaN(size[0]) && !math.IsInf(size[0], 0) &&
		!math.IsNaN(size[1]) && !math.IsInf(size[1], 0)
}

// orientationOf returns the shape of a w by h inch page.
func orientationOf(w, h float64) string {
	switch {
	case h > w:
		return "portrait"
	case w > h:
		return "landscape"
	}
	return ""
}

// orientationInt returns the print value of an orientation.
func orientationInt(o string) int {
	switch o {
	case "portrait":
		return 1
	case "landscape":
		return 2
	}
	return 0
}

// openDoc opens the workspace package for styles and part names.
func openDoc(ws *edit.Workspace) (*doc.Document, *inherit.Resolver, error) {
	d, err := doc.Open(ws.Package())
	if err != nil {
		return nil, nil, fmt.Errorf("vsdx page: %w", err)
	}
	return d, inherit.New(d), nil
}

// pagesPartName returns the pages.xml part of the drawing.
func pagesPartName(ws *edit.Workspace, d *doc.Document) (string, error) {
	rels, err := ws.Package().Rels(d.Part)
	if err != nil {
		return "", fmt.Errorf("vsdx page: %w", err)
	}
	for _, rel := range rels.All() {
		if rel.External() || !ctypes.Is(rel.Type, ctypes.RelPages) {
			continue
		}
		part, err := rels.TargetPart(rel)
		if err != nil {
			continue
		}
		return part, nil
	}
	return "", fmt.Errorf("%w: %s", edit.ErrNotFound, "pages")
}

// shapeIDOf returns the stored ID of a Shape or Page element.
func shapeIDOf(n *tree.Node) (uint32, bool) {
	v, ok := n.Attr("ID")
	if !ok {
		return 0, false
	}
	u, err := strconv.ParseUint(v, 10, 32)
	return uint32(u), err == nil
}

// pageElem returns the dup-th Page with stored ID id in root.
func pageElem(root *tree.Node, id uint32, dup int) *tree.Node {
	seen := 0
	for c := range root.ElementsSeq() {
		if c.Local() != vsd.ElemPage || !vsd.IsMain(c.Space()) {
			continue
		}
		pid, ok := shapeIDOf(c)
		if !ok || pid != id {
			continue
		}
		seen++
		if seen == dup {
			return c
		}
	}
	return nil
}

// pageSheetOf returns the PageSheet child of a Page element.
func pageSheetOf(elem *tree.Node) *tree.Node {
	if elem == nil {
		return nil
	}
	for c := range elem.ElementsSeq() {
		if c.Local() == vsd.ElemPageSheet && vsd.IsMain(c.Space()) {
			return c
		}
	}
	return nil
}

// pageFacts returns the effective size and print value of a sheet.
func pageFacts(r *inherit.Resolver, pageSheet *tree.Node) (float64, float64, bool, int) {
	var sh *sheet.Sheet
	if pageSheet != nil {
		sh, _ = sheet.Index(pageSheet)
	}
	shape := &inherit.Shape{Sheet: sh}
	wc, _, wok := r.Cell(shape, sheet.ShapeCell(vsd.CellPageWidth))
	hc, _, hok := r.Cell(shape, sheet.ShapeCell(vsd.CellPageHeight))
	w, wn := vsd.Number(wc.V)
	h, hn := vsd.Number(hc.V)
	if !wok || !hok || !wn || !hn {
		return 0, 0, false, 0
	}
	printInt := 0
	if c, _, ok := r.Cell(shape, sheet.ShapeCell(vsd.CellOrientation)); ok {
		if f, ok := vsd.Number(c.V); ok && (f == 1 || f == 2) {
			printInt = int(f)
		}
	}
	return w, h, true, printInt
}

// inheritedPrint returns the effective print of a new page.
func inheritedPrint(r *inherit.Resolver) int {
	c, _, ok := r.Cell(nil, sheet.ShapeCell(vsd.CellOrientation))
	if !ok {
		return 0
	}
	f, ok := vsd.Number(c.V)
	if !ok || (f != 1 && f != 2) {
		return 0
	}
	return int(f)
}

// pagePartOf resolves the contents part of a Page element.
func pagePartOf(ws *edit.Workspace, pagesPart string, elem *tree.Node) string {
	if elem == nil {
		return ""
	}
	var relID string
	for c := range elem.ElementsSeq() {
		if c.Local() == vsd.ElemRel && vsd.IsMain(c.Space()) {
			relID, _ = c.AttrNS(vsd.NSRel, "id")
			break
		}
	}
	if relID == "" {
		return ""
	}
	rels, err := ws.Package().Rels(pagesPart)
	if err != nil {
		return ""
	}
	rel, ok := rels.Get(relID)
	if !ok {
		return ""
	}
	part, err := rels.TargetPart(rel)
	if err != nil {
		return ""
	}
	return part
}

// topObjects returns the top-level object addresses on contents.
func topObjects(contents *tree.Document, container string) []string {
	if contents == nil || contents.Root() == nil {
		return nil
	}
	var shapes *tree.Node
	for c := range contents.Root().ElementsSeq() {
		if c.Local() == vsd.ElemShapes && vsd.IsMain(c.Space()) {
			shapes = c
			break
		}
	}
	if shapes == nil {
		return nil
	}
	var out []string
	seen := map[uint32]int{}
	contents.Root().Walk(func(n *tree.Node) bool {
		if n.Kind() != tree.ElementNode || n.Local() != vsd.ElemShape || !vsd.IsMain(n.Space()) {
			return true
		}
		id, ok := shapeIDOf(n)
		if !ok {
			return true
		}
		seen[id]++
		if n.Parent() == shapes {
			out = append(out, vsd.ShapeID(container, id, seen[id]))
		}
		return true
	})
	return out
}

// staleDependents returns "addr Cell" for ThePage! dependents.
func staleDependents(contents *tree.Document, container string, wChanged, hChanged bool) []string {
	if (!wChanged && !hChanged) || contents == nil || contents.Root() == nil {
		return nil
	}
	var out []string
	seen := map[uint32]int{}
	cur := ""
	contents.Root().Walk(func(n *tree.Node) bool {
		if n.Kind() != tree.ElementNode {
			return true
		}
		if n.Local() == vsd.ElemShape && vsd.IsMain(n.Space()) {
			if id, ok := shapeIDOf(n); ok {
				seen[id]++
				cur = vsd.ShapeID(container, id, seen[id])
			} else {
				cur = ""
			}
			return true
		}
		if cur == "" || len(out) >= 10 || n.Local() != vsd.ElemCell || !vsd.IsMain(n.Space()) {
			return true
		}
		f, ok := n.Attr("F")
		if !ok || !strings.Contains(f, "ThePage!") {
			return true
		}
		if (hChanged && strings.Contains(f, vsd.CellPageHeight)) || (wChanged && strings.Contains(f, vsd.CellPageWidth)) {
			name, _ := n.Attr("N")
			out = append(out, cur+" "+name)
		}
		return true
	})
	return out
}

// formulaOK reports whether f is in the decision (c) allow list.
func formulaOK(f string) bool {
	if f == "" || f == vsd.FormulaInherited || f == vsd.FormulaNone || f == "THEMEVAL()" || strings.HasPrefix(f, "THEMEGUARD(") {
		return true
	}
	t := strings.TrimSpace(f)
	if _, ok := vsd.Number(t); ok {
		return true
	}
	parts := strings.Fields(t)
	if len(parts) != 2 {
		return false
	}
	if _, ok := vsd.Number(parts[0]); !ok {
		return false
	}
	switch parts[1] {
	case "IN", "FT", "MM", "CM", "M", "PT", "DEG", "RAD":
		return true
	}
	return false
}

// unwritableCells returns the first cell of names outside the
// allow list, for the formula refusal form.
func unwritableCells(pageSheet *tree.Node, names []string) []formulaCell {
	if pageSheet == nil {
		return nil
	}
	sh, _ := sheet.Index(pageSheet)
	for _, name := range names {
		if c, ok := sh.Cell(sheet.ShapeCell(name)); ok && !formulaOK(c.F) {
			return []formulaCell{{name: name, formula: c.F}}
		}
	}
	return nil
}

// Run applies page.set (decision 6).
func (s *PageSetSpec) Run(ws *edit.Workspace, aliases map[string]string, units string, _ ids.Source) (string, []string, []string, string, error) {
	addr, err := resolvePage(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if s.Size != nil && !sizeOkay(s.Size) {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "size", "size must be two positive numbers")
	}
	if s.Orientation != "" && s.Orientation != "portrait" && s.Orientation != "landscape" {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "orientation", "orientation must be portrait or landscape")
	}
	d, r, err := openDoc(ws)
	if err != nil {
		return "", nil, nil, "", err
	}
	part, err := pagesPartName(ws, d)
	if err != nil {
		return "", nil, nil, "", err
	}
	pages, err := ws.Core().Tree(part)
	if err != nil {
		return "", nil, nil, "", &edit.Unreadable{Target: addr, Detail: err.Error()}
	}
	a, err := vsd.Parse(addr)
	if err != nil || a.Kind != vsd.KindPage || a.InMaster {
		return "", nil, nil, "", fmt.Errorf("vsdx page: %s: want a page address (page0)", addr)
	}
	elem := pageElem(pages.Root(), a.Container, a.PageDup)
	if elem == nil {
		return "", nil, nil, "", fmt.Errorf("%w: %s", edit.ErrNotFound, addr)
	}
	pageSheet := pageSheetOf(elem)
	oldW, oldH, hasSize, oldPrint := pageFacts(r, pageSheet)
	if !hasSize {
		return "", nil, nil, "", &edit.Unreadable{Target: addr, Detail: "page size not derived"}
	}
	var newW, newH float64
	var desired int
	if s.Size != nil {
		newW, newH = sizeInches(s.Size, units)
		if s.Orientation != "" && orientationOf(newW, newH) != s.Orientation {
			return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "orientation", "size [%v %v] is %s, not %s", s.Size[0], s.Size[1], orientationOf(newW, newH), s.Orientation)
		}
		if s.Orientation != "" {
			desired = orientationInt(s.Orientation)
		} else {
			desired = orientationInt(orientationOf(newW, newH))
		}
	} else {
		if s.Orientation == "" {
			return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "", "page.set needs orientation or size")
		}
		if derived := orientationOf(oldW, oldH); derived == "" || s.Orientation == derived {
			newW, newH = oldW, oldH
		} else {
			newW, newH = oldH, oldW
		}
		desired = orientationInt(s.Orientation)
	}
	sizeChanged := newW != oldW || newH != oldH
	if sizeChanged {
		if err := scaledRefusal(r, d, a.Container, a.PageDup, addr, "size"); err != nil {
			return "", nil, nil, "", err
		}
	}
	var cells []edit.Cell
	if sizeChanged {
		if refused := unwritableCells(pageSheet, []string{vsd.CellPageWidth, vsd.CellPageHeight}); len(refused) > 0 {
			return "", nil, nil, "", formulaRefusal(addr, "size", refused)
		}
		cells = append(cells,
			edit.Cell{N: vsd.CellPageWidth, V: vsd.FormatInches(newW)},
			edit.Cell{N: vsd.CellPageHeight, V: vsd.FormatInches(newH)})
	}
	if desired != 0 && desired != oldPrint {
		if refused := unwritableCells(pageSheet, []string{vsd.CellOrientation}); len(refused) > 0 {
			return "", nil, nil, "", formulaRefusal(addr, "orientation", refused)
		}
		cells = append(cells, edit.Cell{N: vsd.CellOrientation, V: strconv.Itoa(desired)})
	}
	if len(cells) == 0 {
		return "", nil, nil, "", nil
	}
	var contents *tree.Document
	if sizeChanged {
		if contentPart := pagePartOf(ws, part, elem); contentPart != "" {
			contents, _ = ws.Core().Tree(contentPart)
		}
	}
	stale := ""
	if sizeChanged && contents != nil {
		if kept := staleDependents(contents, vsd.PageID(a.Container, a.PageDup), newW != oldW, newH != oldH); len(kept) != 0 {
			stale = strings.Join(kept, ", ") + " left stale"
		}
	}
	changed := []string{addr}
	if sizeChanged && contents != nil {
		changed = append(changed, topObjects(contents, vsd.PageID(a.Container, a.PageDup))...)
	}
	if err := ws.SetCells(addr, cells); err != nil {
		return "", nil, nil, "", err
	}
	return "", changed, nil, stale, nil
}
