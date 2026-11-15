package edit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// The workbook-level edits (designer-owned shell): defined names, sheet
// entries, and whole small parts the shift writes back. The ops' glue
// calls these; the kernels never do.

// DefinedName is one <definedName> as stored.
type DefinedName struct {
	// Name is @name; Sheet the scope sheet's name ("" for workbook scope,
	// and for a localSheetId that names no sheet: LocalID then says
	// which); Text the stored formula; Hidden @hidden.
	Name, Sheet, Text string
	LocalID           int // -1 when absent
	Hidden            bool
}

// namesRoot returns the workbook tree's root, or its load error.
func (w *Workspace) namesRoot() (*tree.Node, error) {
	doc, err := w.WorkbookTree()
	if err != nil {
		return nil, err
	}
	return doc.Root(), nil
}

// DefinedNames returns the workbook's defined names in document order.
func (w *Workspace) DefinedNames() ([]DefinedName, error) {
	root, err := w.namesRoot()
	if err != nil {
		return nil, err
	}
	var out []DefinedName
	if c := sml.MainChild(root, "definedNames"); c != nil {
		for _, n := range sml.DefinedNames(c, len(w.sheets)) {
			d := DefinedName{Name: n.Name, Text: n.Text, LocalID: n.Local, Hidden: n.Hidden}
			if n.Local >= 0 {
				d.Sheet = w.sheets[n.Local].Name
			}
			out = append(out, d)
		}
	}
	return out, nil
}

// findName returns the definedName element named name in scope sheet
// ("" for workbook scope), ignoring case. A name whose localSheetId is
// invalid (LocalID -2) is in no scope: it matches neither a sheet nor
// the workbook.
func (w *Workspace) findName(name, sheet string) (*tree.Node, error) {
	root, err := w.namesRoot()
	if err != nil {
		return nil, err
	}
	c := sml.MainChild(root, "definedNames")
	if c == nil {
		return nil, nil
	}
	entries := sml.DefinedNames(c, len(w.sheets))
	k := -1
	for i, n := range entries {
		scope := ""
		if n.Local >= 0 {
			scope = w.sheets[n.Local].Name
		}
		if strings.EqualFold(n.Name, name) && strings.EqualFold(scope, sheet) && (sheet != "" || n.Local == -1) {
			k = i
			break
		}
	}
	if k < 0 {
		return nil, nil
	}
	// The k-th entry is the k-th definedName child in the namespace
	// sml.DefinedNames counts, so the same count finds its element.
	for e := range c.ElementsSeq() {
		if !e.Is(c.Space(), "definedName") {
			continue
		}
		if k == 0 {
			return e, nil
		}
		k--
	}
	return nil, nil
}

// AddDefinedName appends <definedName name localSheetId?>text</definedName>
// to definedNames (made in x:CT_Workbook order when missing). A name
// already in that scope (ignoring case) is an error (the op refuses
// first); a scope sheet the workbook does not have is ErrNotFound.
func (w *Workspace) AddDefinedName(name, sheet, text string) error {
	if e, err := w.findName(name, sheet); err != nil || e != nil {
		if err == nil {
			err = fmt.Errorf("edit: the name %s exists", name)
		}
		return err
	}
	var wr splice.Writer
	wr.Start("definedName")
	wr.Attr("name", name)
	if sheet != "" {
		ref, ok := w.SheetRef(sheet)
		if !ok {
			return fmt.Errorf("%w: sheet %s", ErrNotFound, sheet)
		}
		wr.Attr("localSheetId", strconv.Itoa(ref.Index))
	}
	wr.Text(text)
	wr.End()
	frag, err := wr.String()
	if err != nil {
		return err
	}
	if _, err := w.WorkbookHandle(); err != nil {
		return err
	}
	root, err := w.namesRoot()
	if err != nil {
		return err
	}
	c := sml.MainChild(root, "definedNames")
	if c == nil {
		if err := w.insertSmall(w.wb, root, "x:CT_Workbook", "definedNames", "<definedNames>"+frag+"</definedNames>"); err != nil {
			return err
		}
		return nil
	}
	return w.insertSmall(w.wb, c, "x:CT_DefinedNames", "definedName", frag)
}

// SetDefinedName replaces the stored formula of the name (keeping its
// attributes); ErrNotFound when there is none.
func (w *Workspace) SetDefinedName(name, sheet, text string) error {
	e, err := w.findName(name, sheet)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("%w: name %s", ErrNotFound, name)
	}
	h, err := w.WorkbookHandle()
	if err != nil {
		return err
	}
	return h.SetText(e, text)
}

// DeleteDefinedName removes the name, and definedNames when it empties
// (survey R7); ErrNotFound when there is none.
func (w *Workspace) DeleteDefinedName(name, sheet string) error {
	e, err := w.findName(name, sheet)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("%w: name %s", ErrNotFound, name)
	}
	h, err := w.WorkbookHandle()
	if err != nil {
		return err
	}
	parent := e.Parent()
	if err := h.RemoveNode(e); err != nil {
		return err
	}
	if !parent.HasElements() {
		return h.RemoveNode(parent)
	}
	return nil
}

// SetDefinedNameText rewrites the stored formula of the i-th definedName
// in document order (the structural shift's write-back of
// shift.DefinedName).
func (w *Workspace) SetDefinedNameText(i int, text string) error {
	root, err := w.namesRoot()
	if err != nil {
		return err
	}
	c := sml.MainChild(root, "definedNames")
	k := 0
	if c != nil {
		for e := range c.ElementsSeq() {
			if !e.Is(c.Space(), "definedName") {
				continue
			}
			if k == i {
				if e.Text() == text {
					return nil
				}
				h, err := w.WorkbookHandle()
				if err != nil {
					return err
				}
				return h.SetText(e, text)
			}
			k++
		}
	}
	return fmt.Errorf("edit: no definedName %d", i)
}

// AddSheet adds an empty worksheet named name after the last sheet: the
// part /xl/worksheets/sheetN.xml (the first free N) holding
// BlankWorksheet in the workbook's namespace, its content-type override,
// a workbook relationship and a <sheet> entry (sheetId the largest plus
// one), then ReloadBook. The caller checks the name.
func (w *Workspace) AddSheet(name string) (SheetRef, error) {
	root, err := w.namesRoot()
	if err != nil {
		return SheetRef{}, err
	}
	n := 1
	for w.pkg.Has("/xl/worksheets/sheet" + strconv.Itoa(n) + ".xml") {
		n++
	}
	part := "/xl/worksheets/sheet" + strconv.Itoa(n) + ".xml"
	if err := w.AddPart(part, []byte(BlankWorksheet(root.Space())), ctypes.CTSpreadsheetmlWorksheet); err != nil {
		return SheetRef{}, err
	}
	rels, err := w.pkg.Rels(w.wb)
	if err != nil {
		return SheetRef{}, err
	}
	w.core.DeclarePart(rels.Name())
	rid, err := rels.Add(ctypes.RelWorksheet, "worksheets/sheet"+strconv.Itoa(n)+".xml", false)
	if err != nil {
		return SheetRef{}, err
	}
	id := 0
	for _, s := range w.sheets {
		id = max(id, s.ID)
	}
	h, err := w.WorkbookHandle()
	if err != nil {
		return SheetRef{}, err
	}
	sheets := sml.MainChild(h.Elem(0), "sheets")
	if sheets == nil {
		return SheetRef{}, fmt.Errorf("edit: the workbook has no sheets element")
	}
	rp, ok := sheets.LookupNamespace(sml.NSRel)
	if !ok || rp == "" {
		if rp, ok = sheets.LookupNamespace(sml.NSRelStrict); !ok || rp == "" {
			return SheetRef{}, fmt.Errorf("edit: the workbook does not declare the relationships namespace")
		}
	}
	var wr splice.Writer
	wr.Start("sheet")
	wr.Attr("name", name)
	wr.Attr("sheetId", strconv.Itoa(id+1))
	wr.Attr(rp+":id", rid)
	wr.End()
	frag, err := wr.String()
	if err != nil {
		return SheetRef{}, err
	}
	if err := w.insertSmall(w.wb, sheets, "x:CT_Sheets", "sheet", frag); err != nil {
		return SheetRef{}, err
	}
	if err := w.ReloadBook(); err != nil {
		return SheetRef{}, err
	}
	ref, _ := w.SheetRef(name)
	return ref, nil
}

// BlankWorksheet is the worksheet sheet.add writes, in namespace ns.
func BlankWorksheet(ns string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<worksheet xmlns="` + ns + `"><dimension ref="A1"/><sheetViews><sheetView workbookViewId="0"/></sheetViews>` +
		`<sheetFormatPr defaultRowHeight="15"/><sheetData/>` +
		`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/></worksheet>`
}

// RenameSheet sets the sheet entry's name (the caller checks it and
// rewrites the references), then ReloadBook.
func (w *Workspace) RenameSheet(old, name string) error {
	ref, ok := w.SheetRef(old)
	if !ok {
		return fmt.Errorf("%w: sheet %s", ErrNotFound, old)
	}
	h, err := w.WorkbookHandle()
	if err != nil {
		return err
	}
	sheets := sml.MainChild(h.Elem(0), "sheets")
	i := 0
	for e := range sheets.ElementsSeq() {
		if e.Local() != "sheet" {
			continue
		}
		if i == ref.Index {
			if err := h.SetNodeAttr(e, "name", name); err != nil {
				return err
			}
			return w.ReloadBook()
		}
		i++
	}
	return fmt.Errorf("edit: sheet entry %d is gone", ref.Index)
}

// PartText returns a small part's current bytes as text, for kernels
// that read it (drawings, comments, pivot caches, tables); "" and no
// error for a part the package does not have.
func (w *Workspace) PartText(part string) (string, error) {
	if !w.pkg.Has(part) {
		return "", nil
	}
	if w.big[opc.PartKey(part)] {
		return "", fmt.Errorf("edit: %s is read through Sheet", part)
	}
	pt, err := w.pkg.Part(part)
	if err != nil {
		return "", err
	}
	b, err := pt.Bytes()
	return string(b), err
}

// SetPartText replaces a small part that no tree edit of this plan has
// touched (a table part, rewritten by shift.Table) with text, declared
// as a whole. Text equal to the current bytes changes nothing.
func (w *Workspace) SetPartText(part, text string) error {
	if w.big[opc.PartKey(part)] || opc.PartKey(part) == opc.PartKey(w.wb) {
		return fmt.Errorf("edit: %s is not replaced whole", part)
	}
	cur, err := w.PartText(part)
	if err != nil || cur == text {
		return err
	}
	if err := tree.Check([]byte(text)); err != nil {
		return fmt.Errorf("edit: %s: %w", part, err)
	}
	pt, err := w.pkg.Part(part)
	if err != nil {
		return err
	}
	w.core.DeclarePart(part)
	pt.SetBytes([]byte(text))
	return nil
}
