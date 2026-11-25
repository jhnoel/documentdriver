package ops

import (
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/emit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/text"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run implements text.set: it replaces the shape's shown text with the
// HTML's body, when the body already shows the HTML (as read renders
// it) it writes nothing, and a shape showing its master's text gains
// a local Text with local rows only where the HTML differs from the
// inherited ones. The HTML must account for every field of the shown
// text (contract section 7, chunk CC11): sent back, or listed in
// delete (a listed field is removed from the Text and its Field row
// deleted, and is in the result's deleted; a field both sent back and
// listed is deleted), else operation_failed at html with the addresses
// left out in detail. A field placeholder whose shape is not the target,
// or whose number is past the shape's fields, is not_found at html; a
// Foreign or guide shape and a master shape are operation_failed at
// at; a page whose part cannot be edited is unreadable_target.
func (s *TextSetSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	resolved, addr, err := textSetAddress(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	body, formats, err := text.ParseHTML(s.HTML)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeInvalidOperation, "html", "%s", err)
	}
	st, err := loadTextShape(ws, addr, resolved)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := shapeRefusal(st.res, st.shape, resolved, "at", false); err != nil {
		return "", nil, nil, "", err
	}
	if err := resolveTextFields(&body, st.fields, addr, resolved); err != nil {
		return "", nil, nil, "", err
	}
	deleted, err := resolveTextDeletes(s.Delete, aliases, st.fields, addr, resolved)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := checkTextFieldAccounting(&body, st, deleted, resolved); err != nil {
		return "", nil, nil, "", err
	}
	stripTextDeletes(&body, deleted)
	if text.Render(st.body, st.form, resolved) == text.Render(body, formats, resolved) {
		return "", nil, nil, "", nil
	}
	filtered := textSetFilter(st, formats)
	sections, textEl, err := emit.TextXML(body, filtered)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "html", "%s", err)
	}
	if err := ws.SetText(resolved, sections, textEl); err != nil {
		return "", nil, nil, "", err
	}
	if len(deleted) > 0 && !st.masterText {
		ixs := make([]int, len(deleted))
		for i, n := range deleted {
			ixs[i] = st.fields[n-1].ix
		}
		if err := ws.DeleteFieldRows(resolved, ixs); err != nil {
			return "", nil, nil, "", err
		}
	}
	var deletedAddrs []string
	if len(deleted) > 0 {
		deletedAddrs = make([]string, len(deleted))
		for i, n := range deleted {
			deletedAddrs[i] = vsd.FieldID(resolved, n)
		}
	}
	return "", []string{resolved}, deletedAddrs, "", nil
}

// textSetAddress resolves an `at` of text.set to a shape address and
// its parsed form: an alias is looked up in aliases (with what may
// follow it, $p/s5), a master shape and a stencil are refused here
// (the edit layer would otherwise have no master to check against),
// and anything else is not_found at at.
func textSetAddress(at string, aliases map[string]string) (string, vsd.Address, error) {
	resolved, err := ResolveAlias(at, aliases, "at")
	if err != nil {
		return "", vsd.Address{}, err
	}
	a, err := vsd.Parse(resolved)
	if err != nil || a.Kind != vsd.KindShape {
		return "", vsd.Address{}, cli.Errorf(cli.CodeOperationFailed, "at", "%s is not a shape address", resolved)
	}
	if a.InMaster {
		e := cli.Errorf(cli.CodeOperationFailed, "at", "%s is a master shape: masters cannot be edited", resolved)
		return "", vsd.Address{}, e
	}
	return resolved, a, nil
}

// textShape holds what text.set reads of its target before writing:
// the shape and its owner (itself, or its master shape when it shows
// the master's Text) as resolver shapes, the shown body and formats,
// masterText whether the shown Text is the master's (its Field rows
// live on the master shape, so text.set deletes no local row for a
// field it drops), and the shown fields in text order.
type textShape struct {
	res        *inherit.Resolver
	shape      *inherit.Shape
	body       text.Body
	form       text.Formats
	masterText bool
	// fields is the shown Text's fld elements in order: the IX of the
	// n-th field and its cached text.
	fields []textField
}

// textField is one fld of a shown Text: its Field row IX and the text
// stored in the element.
type textField struct {
	ix   int
	text string
}

// loadTextShape reads the target shape of address a from the working
// tree of its page: the shape node from the core's tree (so a prior
// op's text is visible), indexed through inherit against the
// workspace's document (ws.Doc: pages an earlier op added included;
// masters and styles as opened, which no op changes). It refuses stencils, Foreign and guide shapes, and
// reports a missing page or shape as not_found at at and an unreadable
// page part as unreadable_target.
func loadTextShape(ws *edit.Workspace, a vsd.Address, resolved string) (*textShape, error) {
	// The workspace's document has the pages earlier operations
	// added; doc.Open of the package does not.
	d := ws.Doc()
	if d.Kind == doc.Stencil {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", resolved)
	}
	var entry *doc.PageEntry
	depth := 0
	for i := range d.Pages {
		p := &d.Pages[i]
		if p.ID != a.Container {
			continue
		}
		depth++
		if depth == a.PageDup {
			entry = p
			break
		}
	}
	if entry == nil {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", vsd.PageID(a.Container, a.PageDup))
	}
	if entry.Part == "" {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it has no part"}
	}
	coreDoc, err := ws.Core().Tree(entry.Part)
	if err != nil {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: err.Error()}
	}
	root := coreDoc.Root()
	if root == nil || !vsd.IsMain(root.Space()) || root.Local() != vsd.ElemPageContents {
		return nil, &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it is not a " + vsd.ElemPageContents + " part"}
	}
	node := findTextShape(root, a.Shape, a.Dup)
	if node == nil {
		return nil, cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", resolved)
	}
	if typ, _ := node.Attr("Type"); typ == vsd.ShapeTypeForeign || typ == vsd.ShapeTypeGuide {
		return nil, cli.Errorf(cli.CodeOperationFailed, "at", "%s is a %s shape: text cannot be set", resolved, typ)
	}
	r := inherit.New(d)
	sh := textShapeOf(node)
	st := &textShape{res: r, shape: sh}
	eff, origin := r.Text(sh)
	if eff == nil {
		st.body, st.form = text.Body{}, text.Formats{}
		return st, nil
	}
	owner := sh
	if origin == inherit.Master {
		st.masterText = true
		if master, ok := r.MasterShape(sh); ok {
			owner = master
		}
	}
	st.body, _ = text.Parse(eff)
	st.form = textSetFormats(r, owner)
	st.fields = textSetFields(eff)
	return st, nil
}

// findTextShape returns the dup-th Shape with stored ID id under root
// in document order, or nil. It walks the tree once.
func findTextShape(root *tree.Node, id uint32, dup int) *tree.Node {
	var found *tree.Node
	seen := 0
	root.Walk(func(n *tree.Node) bool {
		if found != nil {
			return false
		}
		if n.Kind() == tree.ElementNode && vsd.IsMain(n.Space()) && n.Local() == vsd.ElemShape {
			if v, ok := n.Attr("ID"); ok {
				if u, err := strconv.ParseUint(v, 10, 32); err == nil && uint32(u) == id {
					seen++
					if seen == dup {
						found = n
						return false
					}
				}
			}
		}
		return true
	})
	return found
}

// textShapeOf builds the resolver shape of a shape node: its sheet,
// its style attributes and its master, inherited from the nearest
// enclosing shape that stores one (a group child's master).
func textShapeOf(node *tree.Node) *inherit.Shape {
	sh := &inherit.Shape{}
	sh.Sheet, _ = sheet.Index(node)
	for name, value := range node.AttrsSeq() {
		switch name {
		case "Master":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Master, sh.HasMaster = uint32(u), true
			}
		case "MasterShape":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.MasterShape, sh.HasMShape = uint32(u), true
			}
		case "LineStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[0], sh.HasStyle[0] = uint32(u), true
			}
		case "FillStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[1], sh.HasStyle[1] = uint32(u), true
			}
		case "TextStyle":
			if u, err := strconv.ParseUint(value, 10, 32); err == nil {
				sh.Styles[2], sh.HasStyle[2] = uint32(u), true
			}
		}
	}
	if sh.HasMaster {
		return sh
	}
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() != tree.ElementNode || p.Local() != vsd.ElemShape {
			continue
		}
		if v, ok := p.Attr("Master"); ok {
			if u, err := strconv.ParseUint(v, 10, 32); err == nil {
				sh.Master, sh.HasMaster = uint32(u), true
				break
			}
		}
	}
	return sh
}

// textSetFields returns the fld elements of a shown Text in text
// order, with their IX and cached text.
func textSetFields(eff *tree.Node) []textField {
	var out []textField
	for c := range eff.ChildrenSeq() {
		if c.Kind() != tree.ElementNode || c.Local() != vsd.ElemField {
			continue
		}
		v, ok := c.Attr("IX")
		ix, err := strconv.Atoi(v)
		if !ok || err != nil || ix < 0 {
			continue
		}
		out = append(out, textField{ix: ix, text: c.Text()})
	}
	return out
}

// resolveTextFields maps every field placeholder of body to the IX and
// cached text of the shape's n-th fld, leaving Ref as written: a
// placeholder whose shape is not the target, or whose number is past
// the shape's fields, is not_found at html.
func resolveTextFields(body *text.Body, fields []textField, addr vsd.Address, resolved string) error {
	for i := range body.Paras {
		for j := range body.Paras[i].Runs {
			run := &body.Paras[i].Runs[j]
			if run.Ref == "" {
				continue
			}
			ref, err := vsd.Parse(run.Ref)
			if err != nil || ref.Kind != vsd.KindField || ref.InMaster ||
				ref.Container != addr.Container || ref.PageDup != addr.PageDup ||
				ref.Shape != addr.Shape || ref.Dup != addr.Dup {
				return cli.Errorf(cli.CodeNotFound, "html", "field %s is not in %s: not found", run.Ref, resolved)
			}
			if ref.N < 1 || ref.N > len(fields) {
				return cli.Errorf(cli.CodeNotFound, "html", "field %s is not in %s: not found", run.Ref, resolved)
			}
			run.Field = fields[ref.N-1].ix
			run.Text = fields[ref.N-1].text
		}
	}
	return nil
}

// resolveTextDeletes resolves the delete list to the 1-based field
// numbers (in text order) of the shape's shown fields it names: a raw
// is resolved through aliases, and one that is not a field of the
// target shape (or past its fields) is not_found at delete. Listed
// twice counts once.
func resolveTextDeletes(list []string, aliases map[string]string, fields []textField, addr vsd.Address, resolved string) ([]int, error) {
	if len(list) == 0 {
		return nil, nil
	}
	want := make(map[int]bool, len(list))
	for _, raw := range list {
		target, err := ResolveAlias(raw, aliases, "delete")
		if err != nil {
			return nil, err
		}
		ref, err := vsd.Parse(target)
		if err != nil || ref.Kind != vsd.KindField || ref.InMaster ||
			ref.Container != addr.Container || ref.PageDup != addr.PageDup ||
			ref.Shape != addr.Shape || ref.Dup != addr.Dup ||
			ref.N < 1 || ref.N > len(fields) {
			return nil, cli.Errorf(cli.CodeNotFound, "delete", "field %s is not in %s: not found", raw, resolved)
		}
		want[ref.N] = true
	}
	out := make([]int, 0, len(want))
	for i := range fields {
		if want[i+1] {
			out = append(out, i+1)
		}
	}
	return out, nil
}

// checkTextFieldAccounting refuses a text.set whose body leaves a
// shown field out without listing it in delete: every field of the
// shown text is either sent back (its placeholder is in body) or
// deleted, and one left out is operation_failed at html with the
// addresses left out in detail, in text order.
func checkTextFieldAccounting(body *text.Body, st *textShape, deleted []int, resolved string) error {
	kept := make(map[int]bool, len(st.fields))
	for i := range body.Paras {
		for _, run := range body.Paras[i].Runs {
			if run.Ref == "" {
				continue
			}
			if ref, err := vsd.Parse(run.Ref); err == nil && ref.Kind == vsd.KindField {
				kept[ref.N] = true
			}
		}
	}
	gone := make(map[int]bool, len(deleted))
	for _, n := range deleted {
		gone[n] = true
	}
	var leftOut []string
	for i := range st.fields {
		if n := i + 1; !kept[n] && !gone[n] {
			leftOut = append(leftOut, vsd.FieldID(resolved, n))
		}
	}
	if len(leftOut) > 0 {
		e := cli.Errorf(cli.CodeOperationFailed, "html", "html must send back every field of %s or list it in delete", resolved)
		e.Detail = strings.Join(leftOut, ", ")
		return e
	}
	return nil
}

// stripTextDeletes removes the placeholders of the deleted fields
// from body: a field both sent back and listed for deletion is
// deleted, so its run does not reach the written Text.
func stripTextDeletes(body *text.Body, deleted []int) {
	if len(deleted) == 0 {
		return
	}
	gone := make(map[int]bool, len(deleted))
	for _, n := range deleted {
		gone[n] = true
	}
	for i := range body.Paras {
		runs := body.Paras[i].Runs
		kept := runs[:0]
		for _, run := range runs {
			if run.Ref != "" {
				if ref, err := vsd.Parse(run.Ref); err == nil && ref.Kind == vsd.KindField && gone[ref.N] {
					continue
				}
			}
			kept = append(kept, run)
		}
		body.Paras[i].Runs = kept
	}
}

// textSetFormats builds the formats the HTML shows for a shape's Text:
// only the owner sheet's own rows, a stored value the inherited one
// already shows (12 pt spelled either way) shown as nothing.
func textSetFormats(r *inherit.Resolver, owner *inherit.Shape) text.Formats {
	if r == nil || owner == nil {
		return text.Formats{}
	}
	below := *owner
	below.Sheet = nil
	var out text.Formats
	for _, row := range r.Rows(owner, vsd.SectionCharacter) {
		if row.Origin != inherit.Local || row.Key.RowIX < 0 {
			continue
		}
		if out.Char == nil {
			out.Char = make(map[int]text.CharFormat)
		}
		out.Char[row.Key.RowIX] = textSetChar(r, owner, &below, row.Key)
	}
	for _, row := range r.Rows(owner, vsd.SectionParagraph) {
		if row.Origin != inherit.Local || row.Key.RowIX < 0 {
			continue
		}
		if out.Para == nil {
			out.Para = make(map[int]text.ParaFormat)
		}
		out.Para[row.Key.RowIX] = textSetPara(r, owner, &below, row.Key)
	}
	return out
}

// textSetCellPair is the own and inherited cells of one row cell, as
// the shown formats read them.
type textSetCellPair struct {
	own, inh       sheet.Cell
	hasOwn, hasInh bool
}

// textSetPair returns the pair of owner and below at key: the own cell
// only when owner stores it locally, the inherited cell whenever the
// chain below owner stores it.
func textSetPair(r *inherit.Resolver, owner, below *inherit.Shape, key sheet.Key) textSetCellPair {
	var p textSetCellPair
	if own, origin, ok := r.Cell(owner, key); ok && origin == inherit.Local {
		p.own, p.hasOwn = own, true
	}
	p.inh, _, p.hasInh = r.Cell(below, key)
	return p
}

// textSetChar builds the shown character format of one local row.
func textSetChar(r *inherit.Resolver, owner, below *inherit.Shape, row sheet.Key) text.CharFormat {
	var f text.CharFormat
	if p := textSetPair(r, owner, below, row.WithCell("Style")); p.hasOwn {
		if n, ok := vsd.Number(p.own.V); ok {
			inhNum := 0
			if p.hasInh {
				if m, ok := vsd.Number(p.inh.V); ok {
					inhNum = int(m)
				}
			}
			ownInt := int(n)
			f.Bold = textSetOnOff(ownInt&1 != 0, inhNum&1 != 0)
			f.Italic = textSetOnOff(ownInt&2 != 0, inhNum&2 != 0)
			f.Underline = textSetOnOff(ownInt&4 != 0, inhNum&4 != 0)
		}
	}
	if p := textSetPair(r, owner, below, row.WithCell("Strikethru")); p.hasOwn {
		if on, ok := textSetBool(p.own.V); ok {
			inhOn := false
			if p.hasInh {
				inhOn = textSetIsOn(p.inh.V)
			}
			f.Strike = textSetOnOff(on, inhOn)
		}
	}
	if p := textSetPair(r, owner, below, row.WithCell("Size")); p.hasOwn {
		f.Size = textSetShown(p.own, p.inh, p.hasInh, textSetPoints)
	}
	if p := textSetPair(r, owner, below, row.WithCell("Color")); p.hasOwn {
		if hex, ok := r.Doc().Color(p.own.V); ok {
			match := false
			if p.hasInh {
				if inhHex, ok := r.Doc().Color(p.inh.V); ok && inhHex == hex {
					match = true
				}
			}
			if !match {
				f.Color = hex
			}
		}
	}
	if p := textSetPair(r, owner, below, row.WithCell("Font")); p.hasOwn {
		if p.own.V != "" && p.own.V != vsd.ValueThemed && !strings.Contains(p.own.V, ";") && !(p.hasInh && p.own.V == p.inh.V) {
			f.Font = p.own.V
		}
	}
	return f
}

// textSetPara builds the shown paragraph format of one local row.
func textSetPara(r *inherit.Resolver, owner, below *inherit.Shape, row sheet.Key) text.ParaFormat {
	var f text.ParaFormat
	if p := textSetPair(r, owner, below, row.WithCell("HorzAlign")); p.hasOwn {
		f.Align = textSetAlign(p.own, p.inh, p.hasInh)
	}
	if p := textSetPair(r, owner, below, row.WithCell("Bullet")); p.hasOwn {
		if textSetIsOn(p.own.V) && !(p.hasInh && textSetIsOn(p.inh.V) == textSetIsOn(p.own.V)) {
			f.Bullet = true
		}
	}
	if p := textSetPair(r, owner, below, row.WithCell("BulletStr")); p.hasOwn {
		if !p.hasInh || p.own.V != p.inh.V {
			f.BulletChar = p.own.V
		}
	}
	type length struct {
		cell string
		set  *string
	}
	for _, l := range []length{
		{"IndLeft", &f.MarginLeft}, {"IndRight", &f.MarginRight}, {"IndFirst", &f.Indent},
		{"SpBefore", &f.SpaceBefore}, {"SpAfter", &f.SpaceAfter},
	} {
		if p := textSetPair(r, owner, below, row.WithCell(l.cell)); p.hasOwn {
			*l.set = textSetShown(p.own, p.inh, p.hasInh, textSetLength)
		}
	}
	if p := textSetPair(r, owner, below, row.WithCell("SpLine")); p.hasOwn {
		f.LineHeight = textSetShown(p.own, p.inh, p.hasInh, textSetLineHeight)
	}
	return f
}

// textSetOnOff returns the shown form of a bit: "on" only locally, "off"
// only when the inherited one sets it, "" when they agree.
func textSetOnOff(own, inh bool) string {
	if own && !inh {
		return "on"
	}
	if !own && inh {
		return "off"
	}
	return ""
}

// textSetBool parses a stored boolean V: a Visio boolean spelling, or
// a number other than zero.
func textSetBool(v string) (bool, bool) {
	if b, ok := vsd.Bool(v); ok {
		return b, true
	}
	n, ok := vsd.Number(v)
	if !ok {
		return false, false
	}
	return n != 0, true
}

// textSetIsOn reports whether a stored V is set.
func textSetIsOn(v string) bool {
	on, ok := textSetBool(v)
	return ok && on
}

// textSetShown returns own's value as show spells it, or "" when own
// is not a number or shows as the inherited one does.
func textSetShown(own, inh sheet.Cell, inhOK bool, show func(float64) string) string {
	ownNum, ownOK := vsd.Number(own.V)
	if !ownOK {
		return ""
	}
	shown := show(ownNum)
	if !inhOK {
		return shown
	}
	inhNum, inhOK2 := vsd.Number(inh.V)
	if !inhOK2 || show(inhNum) != shown {
		return shown
	}
	return ""
}

// textSetAlign returns own's HorzAlign as shown, or "" when it is not
// 0 to 3 or stores the inherited number.
func textSetAlign(own, inh sheet.Cell, inhOK bool) string {
	n, ok := vsd.Number(own.V)
	if !ok || n != float64(int(n)) {
		return ""
	}
	if inhOK {
		if m, ok := vsd.Number(inh.V); ok && m == n {
			return ""
		}
	}
	switch int(n) {
	case 0:
		return "left"
	case 1:
		return "center"
	case 2:
		return "right"
	case 3:
		return "justify"
	}
	return ""
}

// textSetPoints returns inches in points, as Size shows it.
func textSetPoints(n float64) string {
	return geom.FormatNum(n * vsd.PointsPerInch)
}

// textSetLength returns inches in points with the unit, as indents
// and spacings show it.
func textSetLength(n float64) string {
	return geom.FormatNum(n*vsd.PointsPerInch) + "pt"
}

// textSetLineHeight returns a SpLine value as shown: a negative value
// as a percentage, otherwise in points.
func textSetLineHeight(n float64) string {
	if n >= 0 {
		return textSetLength(n)
	}
	return geom.FormatNum(-n*100) + "%"
}

// textSetFilter returns a copy of formats with every field the
// inherited rows already show cleared, so a write stores local cells
// only where the HTML differs from what the shape inherits: Style is
// kept as one value that carries the inherited bits the HTML does not
// mention, and clears when it would equal the inheritance.
func textSetFilter(st *textShape, in text.Formats) text.Formats {
	below := *st.shape
	below.Sheet = nil
	out := text.Formats{}
	if len(in.Char) > 0 {
		out.Char = make(map[int]text.CharFormat, len(in.Char))
		for ix, cf := range in.Char {
			out.Char[ix] = textSetFilterChar(st, &below, ix, cf)
		}
	}
	if len(in.Para) > 0 {
		out.Para = make(map[int]text.ParaFormat, len(in.Para))
		for ix, pf := range in.Para {
			out.Para[ix] = textSetFilterPara(st, &below, ix, pf)
		}
	}
	return out
}

// textSetFilterChar clears the character fields of one row that the
// inheritance already shows, merging Style with the inherited bits.
func textSetFilterChar(st *textShape, below *inherit.Shape, ix int, cf text.CharFormat) text.CharFormat {
	key := sheet.IndexedRowCell(vsd.SectionCharacter, -1, ix, "")
	if cf.Bold != "" || cf.Italic != "" || cf.Underline != "" {
		inh := 0
		if c, _, ok := st.res.Cell(below, key.WithCell("Style")); ok {
			if n, ok := vsd.Number(c.V); ok {
				inh = int(n)
			}
		}
		effective := inh
		if cf.Bold != "" {
			effective = textSetBit(effective, 1, cf.Bold == "on")
		}
		if cf.Italic != "" {
			effective = textSetBit(effective, 2, cf.Italic == "on")
		}
		if cf.Underline != "" {
			effective = textSetBit(effective, 4, cf.Underline == "on")
		}
		if effective == inh {
			cf.Bold, cf.Italic, cf.Underline = "", "", ""
		} else {
			cf.Bold = textSetOnOff(effective&1 != 0, inh&1 != 0)
			cf.Italic = textSetOnOff(effective&2 != 0, inh&2 != 0)
			cf.Underline = textSetOnOff(effective&4 != 0, inh&4 != 0)
			// TextXML combines the bits into Style; a bit the HTML
			// did not mention and the inheritance agrees on must not
			// be lost when it is written as a combined value.
			if cf.Bold == "" && effective&1 != 0 {
				cf.Bold = "on"
			}
			if cf.Italic == "" && effective&2 != 0 {
				cf.Italic = "on"
			}
			if cf.Underline == "" && effective&4 != 0 {
				cf.Underline = "on"
			}
		}
	}
	if cf.Size != "" {
		if inh, ok := textSetInherited(st, below, key.WithCell("Size")); ok {
			want, err := strconv.ParseFloat(strings.TrimSuffix(cf.Size, "pt"), 64)
			if err == nil {
				want /= vsd.PointsPerInch
			}
			if n, ok := vsd.Number(inh.V); ok && err == nil && textSetPoints(n) == textSetPoints(want) {
				cf.Size = ""
			}
		}
	}
	if cf.Color != "" {
		if inh, ok := textSetInherited(st, below, key.WithCell("Color")); ok {
			if hex, ok := st.res.Doc().Color(inh.V); ok && hex == cf.Color {
				cf.Color = ""
			}
		}
	}
	if cf.Font != "" {
		if inh, ok := textSetInherited(st, below, key.WithCell("Font")); ok && inh.V == cf.Font {
			cf.Font = ""
		}
	}
	if cf.Strike != "" {
		if inh, ok := textSetInherited(st, below, key.WithCell("Strikethru")); ok {
			if textSetIsOn(inh.V) == (cf.Strike == "on") {
				cf.Strike = ""
			}
		}
	}
	return cf
}

// textSetBit returns style with bit set or cleared.
func textSetBit(style, bit int, on bool) int {
	if on {
		return style | bit
	}
	return style &^ bit
}

// textSetInherited returns the cell the chain below shape stores at
// key, or false when no sheet there stores it.
func textSetInherited(st *textShape, below *inherit.Shape, key sheet.Key) (sheet.Cell, bool) {
	c, _, ok := st.res.Cell(below, key)
	return c, ok
}

// textSetFilterPara clears the paragraph fields of one row that the
// inheritance already shows.
func textSetFilterPara(st *textShape, below *inherit.Shape, ix int, pf text.ParaFormat) text.ParaFormat {
	key := sheet.IndexedRowCell(vsd.SectionParagraph, -1, ix, "")
	if pf.Align != "" {
		if inh, ok := textSetInherited(st, below, key.WithCell("HorzAlign")); ok {
			if textSetAlign(inh, sheet.Cell{}, false) == pf.Align {
				pf.Align = ""
			}
		}
	}
	clearLength := func(cell, value string) string {
		if value == "" {
			return ""
		}
		inh, ok := textSetInherited(st, below, key.WithCell(cell))
		if !ok {
			return value
		}
		n, ok := vsd.Number(inh.V)
		if !ok {
			return value
		}
		if textSetLength(n) == textSetLengthPoints(value) {
			return ""
		}
		return value
	}
	pf.MarginLeft = clearLength("IndLeft", pf.MarginLeft)
	pf.MarginRight = clearLength("IndRight", pf.MarginRight)
	pf.Indent = clearLength("IndFirst", pf.Indent)
	pf.SpaceBefore = clearLength("SpBefore", pf.SpaceBefore)
	pf.SpaceAfter = clearLength("SpAfter", pf.SpaceAfter)
	if pf.LineHeight != "" {
		if inh, ok := textSetInherited(st, below, key.WithCell("SpLine")); ok {
			if n, ok := vsd.Number(inh.V); ok && textSetLineHeight(n) == textSetLineHeightPoints(pf.LineHeight) {
				pf.LineHeight = ""
			}
		}
	}
	return pf
}

// textSetLengthPoints returns a point length as textSetLength spells
// it, or the value itself when it is not a number.
func textSetLengthPoints(value string) string {
	n, err := strconv.ParseFloat(strings.TrimSuffix(value, "pt"), 64)
	if err != nil {
		return value
	}
	return geom.FormatNum(n) + "pt"
}

// textSetLineHeightPoints returns a line height as textSetLineHeight
// spells it, or the value itself when it is not a number.
func textSetLineHeightPoints(value string) string {
	if strings.HasSuffix(value, "%") {
		n, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
		if err != nil {
			return value
		}
		return geom.FormatNum(n) + "%"
	}
	return textSetLengthPoints(value)
}
