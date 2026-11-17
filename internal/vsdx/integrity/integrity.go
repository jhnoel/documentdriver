// Package integrity is the structural lint of a drawing and the gate of
// the writes (docs/vsdx-design.md, Verification item 6): shape IDs unique
// per page across nesting, Connects naming shapes that exist, Master,
// MasterShape and style IDs that resolve, page IDs unique, BackPage
// resolving, one Override per part and one relationship per (source,
// target, type), the document content type matching the package kind.
// As docx's and xlsx's gates, a plan is refused only for a finding it
// introduces.
package integrity

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Finding is one structural defect: its kind (duplicate-shape-id,
// dangling-connect, unresolved-master, unresolved-style,
// duplicate-page-id, unresolved-backpage, duplicate-override,
// duplicate-rel, missing-part, kind-mismatch), the part and the
// nearest address, and a sentence.
type Finding struct {
	Kind, Part, At, Detail string
}

// checker holds the state of one Check run: the package and its
// document, the masters' shape sets, and the findings so far.
type checker struct {
	pkg          *opc.Package
	d            *doc.Document
	pagesPart    string
	mastersPart  string
	masterIDs    map[uint32]bool
	masterShapes map[uint32]map[uint32]bool
	visited      map[string]bool
	findings     []Finding
}

// Check returns every finding in the package, sorted by part then at.
// It opens the package with doc.Open, so a package with no Visio
// document relationship or a malformed document part is an error, as
// doc.Open reports; a malformed page or master part is skipped (the
// document reader already notes it, and no finding kind covers it).
// Chunk G1.
func Check(p *opc.Package) ([]Finding, error) {
	if p == nil {
		return nil, fmt.Errorf("vsdx: integrity: no package")
	}
	d, err := doc.Open(p)
	if err != nil {
		return nil, fmt.Errorf("vsdx: integrity: %w", err)
	}
	c := &checker{
		pkg:          p,
		d:            d,
		masterIDs:    make(map[uint32]bool),
		masterShapes: make(map[uint32]map[uint32]bool),
		visited:      make(map[string]bool),
	}
	for _, m := range d.Masters {
		c.masterIDs[m.ID] = true
	}
	c.pagesPart = partByRelType(p, d.Part, ctypes.RelPages)
	c.mastersPart = partByRelType(p, d.Part, ctypes.RelMasters)
	c.checkContentTypes()
	c.checkRels()
	c.checkKind()
	c.checkLists()
	c.checkMasters()
	c.checkPages()
	slices.SortFunc(c.findings, compareFindings)
	return c.findings, nil
}

// compareFindings orders findings by part, then at, then kind and
// detail, so findings sharing a part and address are deterministic.
func compareFindings(a, b Finding) int {
	return cmp.Or(
		cmp.Compare(a.Part, b.Part),
		cmp.Compare(a.At, b.At),
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.Detail, b.Detail),
	)
}

// add records one finding of kind at at in part.
func (c *checker) add(kind, part, at, detail string) {
	c.findings = append(c.findings, Finding{Kind: kind, Part: part, At: at, Detail: detail})
}

// checkContentTypes reports a duplicate Override for one part in
// [Content_Types].xml.
func (c *checker) checkContentTypes() {
	pt, err := c.pkg.Part(opc.ContentTypesName)
	if err != nil {
		return
	}
	treeDoc, err := pt.Tree()
	if err != nil {
		return
	}
	root := treeDoc.Root()
	if root == nil || root.Local() != "Types" {
		return
	}
	seen := make(map[string]string)
	emitted := make(map[string]bool)
	for elem := range root.ElementsSeq() {
		if !elem.Is(root.Space(), "Override") {
			continue
		}
		partName, ok := elem.Attr("PartName")
		if !ok || partName == "" {
			continue
		}
		key := opc.PartKey(partName)
		if first, dup := seen[key]; !dup {
			seen[key] = partName
		} else if !emitted[key] {
			emitted[key] = true
			c.add("duplicate-override", opc.ContentTypesName, first,
				fmt.Sprintf("part %s has more than one Override in [Content_Types].xml", first))
		}
	}
}

// checkRels reports a relationship whose target part is missing and a
// second relationship of one source to the same target with the same
// type.
func (c *checker) checkRels() {
	sources := append([]string{"/"}, c.pkg.Names()...)
	for _, source := range sources {
		rels, err := c.pkg.Rels(source)
		if err != nil || rels == nil {
			continue
		}
		seen := make(map[string]string)
		emitted := make(map[string]bool)
		for _, rel := range rels.All() {
			if rel.External() {
				continue
			}
			target, err := rels.TargetPart(rel)
			if err != nil {
				continue
			}
			if !c.pkg.Has(target) {
				c.add("missing-part", rels.Name(), target,
					fmt.Sprintf("relationship %s of %s targets missing part %s", rel.ID, source, target))
			}
			key := opc.PartKey(target) + "\x00" + ctypes.Last(rel.Type)
			if first, dup := seen[key]; !dup {
				seen[key] = rel.ID
			} else if !emitted[key] {
				emitted[key] = true
				c.add("duplicate-rel", rels.Name(), target,
					fmt.Sprintf("source %s has relationships %s and %s of one type to %s", source, first, rel.ID, target))
			}
		}
	}
}

// checkKind reports a document, pages, page, masters or master part
// whose Override content type does not match the Visio kind it is.
//
// Only an Override is judged: a part that relies on the xml Default
// (application/xml) is not a mismatch, and the document check runs
// only for a drawing, whose two content types are the S0 constants.
func (c *checker) checkKind() {
	ct, err := c.pkg.ContentTypes()
	if err != nil {
		return
	}
	override := func(part string) (string, bool) {
		if part == "" {
			return "", false
		}
		return ct.Override(part)
	}
	if c.d.Kind == doc.Drawing {
		if pt, err := c.pkg.Part(c.d.Part); err == nil {
			if got, err := pt.ContentType(); err == nil && got != "" &&
				got != ctypes.CTMsVisioDrawingMain && got != ctypes.CTMsVisioDrawingMacroEnabledMain {
				c.add("kind-mismatch", c.d.Part, c.d.Part,
					fmt.Sprintf("document part content type %s does not match the drawing kind", got))
			}
		}
	}
	if got, ok := override(c.pagesPart); ok && got != ctypes.CTMsVisioPages {
		c.add("kind-mismatch", c.pagesPart, c.pagesPart,
			fmt.Sprintf("pages part content type %s is not the Visio pages type", got))
	}
	if got, ok := override(c.mastersPart); ok && got != ctypes.CTMsVisioMasters {
		c.add("kind-mismatch", c.mastersPart, c.mastersPart,
			fmt.Sprintf("masters part content type %s is not the Visio masters type", got))
	}
	for i := range c.d.Pages {
		e := &c.d.Pages[i]
		if got, ok := override(e.Part); ok && got != ctypes.CTMsVisioPage {
			c.add("kind-mismatch", e.Part, vsd.PageID(e.ID, 1),
				fmt.Sprintf("page part content type %s is not the Visio page type", got))
		}
	}
	for i := range c.d.Masters {
		e := &c.d.Masters[i]
		if got, ok := override(e.Part); ok && got != ctypes.CTMsVisioMaster {
			c.add("kind-mismatch", e.Part, vsd.MasterID(e.ID),
				fmt.Sprintf("master part content type %s is not the Visio master type", got))
		}
	}
}

// checkLists reports duplicate page IDs, pages with no part, a
// BackPage naming no page, and style IDs of page sheets and style
// sheets that resolve to no style sheet.
func (c *checker) checkLists() {
	pageIDs := make(map[uint32]bool, len(c.d.Pages))
	for _, e := range c.d.Pages {
		pageIDs[e.ID] = true
	}
	counts := make(map[uint32]int, len(c.d.Pages))
	for i := range c.d.Pages {
		e := &c.d.Pages[i]
		counts[e.ID]++
		dup := counts[e.ID]
		at := vsd.PageID(e.ID, dup)
		if dup == 2 {
			c.add("duplicate-page-id", c.pagesPart, vsd.PageID(e.ID, 1),
				fmt.Sprintf("page ID %d is used by more than one page", e.ID))
		}
		if e.Part == "" {
			c.add("missing-part", c.pagesPart, at,
				fmt.Sprintf("%s has no part: its Rel does not resolve", at))
		}
		if e.HasBack && !pageIDs[e.BackPage] {
			c.add("unresolved-backpage", c.pagesPart, at,
				fmt.Sprintf("%s has BackPage %d, which names no page", at, e.BackPage))
		}
		if e.PageSheet != nil {
			c.checkStyles(e.PageSheet, c.pagesPart, at)
		}
	}
	for i := range c.d.Masters {
		e := &c.d.Masters[i]
		if e.Part == "" {
			c.add("missing-part", c.mastersPart, vsd.MasterID(e.ID),
				fmt.Sprintf("%s has no part: its Rel does not resolve", vsd.MasterID(e.ID)))
		}
	}
	for _, ss := range c.d.Styles {
		for _, parent := range []struct {
			name    string
			id      uint32
			present bool
		}{
			{"LineStyle", ss.LineStyle, ss.HasLine},
			{"FillStyle", ss.FillStyle, ss.HasFill},
			{"TextStyle", ss.TextStyle, ss.HasText},
		} {
			if parent.present && c.d.Styles[parent.id] == nil {
				c.add("unresolved-style", c.d.Part, c.d.Part,
					fmt.Sprintf("style sheet %d has %s %d, which names no style sheet", ss.ID, parent.name, parent.id))
			}
		}
	}
}

// checkStyles reports a LineStyle, FillStyle or TextStyle attribute
// of elem that names no style sheet of the document.
func (c *checker) checkStyles(elem *tree.Node, part, at string) {
	for _, name := range []string{"LineStyle", "FillStyle", "TextStyle"} {
		id, ok := attrUint(elem, name)
		if ok && c.d.Styles[id] == nil {
			c.add("unresolved-style", part, at,
				fmt.Sprintf("%s has %s %d, which names no style sheet", at, name, id))
		}
	}
}

// checkMasters walks every master's contents once, recording its
// shape IDs for the pages' MasterShape checks.
func (c *checker) checkMasters() {
	for i := range c.d.Masters {
		e := &c.d.Masters[i]
		if e.Part == "" || c.visited[opc.PartKey(e.Part)] {
			continue
		}
		c.visited[opc.PartKey(e.Part)] = true
		treeDoc, err := c.d.MasterContents(e.ID)
		if err != nil {
			continue
		}
		st := newShapeState()
		c.walkContents(treeDoc, e.Part, vsd.MasterID(e.ID), false, st)
		if _, ok := c.masterShapes[e.ID]; !ok {
			c.masterShapes[e.ID] = st.ids
		}
	}
}

// checkPages walks every page's contents once.
func (c *checker) checkPages() {
	counts := make(map[uint32]int, len(c.d.Pages))
	for i := range c.d.Pages {
		e := &c.d.Pages[i]
		counts[e.ID]++
		if e.Part == "" || c.visited[opc.PartKey(e.Part)] {
			continue
		}
		c.visited[opc.PartKey(e.Part)] = true
		treeDoc, err := c.d.PageContents(e)
		if err != nil {
			continue
		}
		c.walkContents(treeDoc, e.Part, vsd.PageID(e.ID, counts[e.ID]), true, newShapeState())
	}
}

// walkContents walks the shapes of one page or master contents tree
// and then its connects, in stored order.
func (c *checker) walkContents(treeDoc *tree.Document, part, container string, isPage bool, st *shapeState) {
	root := treeDoc.Root()
	if root == nil {
		return
	}
	var shapes, connects *tree.Node
	for child := range root.ElementsSeq() {
		switch {
		case isMain(child, vsd.ElemShapes):
			shapes = child
		case isMain(child, vsd.ElemConnects):
			connects = child
		}
	}
	if shapes != nil {
		c.walkShapes(shapes, part, container, 0, false, isPage, st)
	}
	if connects != nil {
		c.checkConnects(connects, part, container, st)
	}
}

// walkShapes walks the Shape elements of shapes, recursing into a
// shape's nested Shapes. master is the master of the enclosing
// instance (a shape's own Master overrides it for its children).
func (c *checker) walkShapes(shapes *tree.Node, part, container string, master uint32, hasMaster, isPage bool, st *shapeState) {
	for child := range shapes.ElementsSeq() {
		if !isMain(child, vsd.ElemShape) {
			continue
		}
		id, hasID := attrUint(child, "ID")
		at := container
		if hasID {
			at = vsd.ShapeID(container, id, 1)
			st.ids[id] = true
			st.counts[id]++
			if st.counts[id] == 2 {
				c.add("duplicate-shape-id", part, at,
					fmt.Sprintf("shape ID %d is used by more than one shape on %s", id, container))
			}
		}
		effMaster, hasEff := master, hasMaster
		if own, ok := attrUint(child, "Master"); ok {
			effMaster, hasEff = own, true
			if !c.masterIDs[own] {
				c.add("unresolved-master", part, at,
					fmt.Sprintf("%s has Master %d, which names no master", at, own))
			}
		}
		if isPage {
			if ms, ok := attrUint(child, "MasterShape"); ok {
				switch {
				case !hasEff:
					c.add("unresolved-master", part, at,
						fmt.Sprintf("%s has MasterShape %d with no master", at, ms))
				case c.masterShapes[effMaster] != nil && !c.masterShapes[effMaster][ms]:
					c.add("unresolved-master", part, at,
						fmt.Sprintf("%s has MasterShape %d, which names no shape of master %d", at, ms, effMaster))
				}
			}
		}
		c.checkStyles(child, part, at)
		for nested := range child.ElementsSeq() {
			if isMain(nested, vsd.ElemShapes) {
				c.walkShapes(nested, part, container, effMaster, hasEff, isPage, st)
			}
		}
	}
}

// checkConnects reports a Connect whose FromSheet names no shape of
// the page. A Connect whose ToSheet names no shape is not a finding
// here: it is the read model's dangling-glue hazard (fixture V5's
// s4, whose end is glued to the absent s9), not a structural defect
// of the row itself.
func (c *checker) checkConnects(connects *tree.Node, part, container string, st *shapeState) {
	for child := range connects.ElementsSeq() {
		if !isMain(child, vsd.ElemConnect) {
			continue
		}
		from, ok := attrUint(child, "FromSheet")
		if !ok {
			continue
		}
		if !st.ids[from] {
			c.add("dangling-connect", part, vsd.ShapeID(container, from, 1),
				fmt.Sprintf("Connect FromSheet %d names no shape on %s", from, container))
		}
	}
}

// shapeState is the per-container shape ID state: ids holds every
// shape ID seen, counts how often each was seen.
type shapeState struct {
	ids    map[uint32]bool
	counts map[uint32]int
}

// newShapeState returns an empty shapeState.
func newShapeState() *shapeState {
	return &shapeState{ids: make(map[uint32]bool), counts: make(map[uint32]int)}
}

// partByRelType returns the part that source's relationship of type
// want targets, or "" when source has none that resolves.
func partByRelType(p *opc.Package, source, want string) string {
	rels, err := p.Rels(source)
	if err != nil || rels == nil {
		return ""
	}
	for _, rel := range rels.All() {
		if rel.External() || !ctypes.Is(rel.Type, want) {
			continue
		}
		if target, err := rels.TargetPart(rel); err == nil {
			return target
		}
	}
	return ""
}

// isMain reports whether n is an element of local name in a Visio
// main namespace (either spelling).
func isMain(n *tree.Node, local string) bool {
	return n != nil && vsd.IsMain(n.Space()) && n.Local() == local
}

// attrUint parses element n's attribute name as a uint32, reporting
// false when the attribute is absent or does not parse.
func attrUint(n *tree.Node, name string) (uint32, bool) {
	raw, found := n.Attr(name)
	if !found {
		return 0, false
	}
	val, err := strconv.ParseUint(raw, 10, 32)
	if err == nil {
		return uint32(val), true
	}
	return 0, false
}
