package edit

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	core "github.com/jhnoel/documentdriver/internal/ooxml/edit"
	"github.com/jhnoel/documentdriver/internal/ooxml/schema"
	"github.com/jhnoel/documentdriver/internal/ooxml/splice"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// ErrNotFound is returned (wrapped, with what is missing) for a sheet,
// table or name the workbook does not have: not_found.
var ErrNotFound = errors.New("edit: not in the workbook")

// ErrNoWorkbook is returned by New for a package without a workbook part
// (invalid_document).
var ErrNoWorkbook = errors.New("edit: package has no workbook part")

// Unreadable is an op's own target that cannot be edited safely
// (unreadable_target): its part is missing or not well formed, it is not
// a worksheet, or its rows or cells are out of order or outside the grid
// (sheet.Scan skipped some, so the index does not say where everything
// is).
type Unreadable struct {
	// Target is the address of what is unreadable ("sheet:Budget").
	Target string
	// Detail says what is wrong (the contract's detail).
	Detail string
}

// Error implements error.
func (u *Unreadable) Error() string { return u.Target + ": " + u.Detail }

// SheetKind is the kind of a workbook <sheet> entry.
type SheetKind uint8

// The sheet kinds.
const (
	// KindMissing: the entry's relationship or part is missing.
	KindMissing SheetKind = iota
	// KindWorksheet is a worksheet (cells).
	KindWorksheet
	// KindChartsheet is a chartsheet.
	KindChartsheet
	// KindOther is a dialog or macro sheet, or an unknown kind.
	KindOther
)

// SheetRef is one <sheet> entry of the workbook, in tab order.
type SheetRef struct {
	// Name is sheet@name as stored; ID sheet@sheetId; RID its r:id;
	// State "visible", "hidden" or "veryHidden" as stored.
	Name  string
	ID    int
	RID   string
	State string
	// Part is the target part ("" when the relationship or part is
	// missing); Kind its relationship's kind.
	Part string
	Kind SheetKind
	// Index is the entry's 0-based position in tab order.
	Index int
}

// Workspace is a plan's editing view of an xlsx package: the edit
// core's workspace over the package, the workbook's sheets, the sheets
// opened so far and the recalculation state. It implements
// plan.Workspace. It never saves: plan transactions do.
type Workspace struct {
	pkg  *opc.Package
	core *core.Workspace
	// wb is the workbook part, sst the shared strings part ("" for none).
	wb, sst string
	// big holds the part keys never parsed into a tree.
	big map[string]bool

	sheets   []SheetRef
	date1904 bool
	// fullCalc is calcPr@fullCalcOnLoad as the input stored it.
	fullCalc bool

	// open are the sheets opened so far, by part key.
	open map[string]*Sheet
	// strings is the shared strings index, opened on first use.
	strings     *sheet.SST
	stringsDone bool
	stringsErr  error

	styles *Styles

	// values is set when a cell value, formula or structure changed (the
	// save must ask for recalculation); formulas when a formula cell was
	// added, removed or moved (the calcChain goes).
	values, formulas bool
	finished         bool
}

// New returns a workspace over pkg: the workbook part is the target of
// the package's officeDocument relationship, and its sheets are read
// from its tree. Errors: ErrNoWorkbook, or the workbook's tree or
// relationships failing to parse (invalid_document).
func New(pkg *opc.Package) (*Workspace, error) {
	w := &Workspace{pkg: pkg, core: core.New(pkg, coreFormat), big: map[string]bool{}, open: map[string]*Sheet{}}
	rels, err := pkg.Rels("/")
	if err != nil {
		return nil, err
	}
	for _, r := range rels.All() {
		if ctypes.Is(r.Type, ctypes.RelOfficeDocument) && !r.External() {
			if p, err := rels.TargetPart(r); err == nil && pkg.Has(p) {
				w.wb = p
				break
			}
		}
	}
	if w.wb == "" {
		return nil, ErrNoWorkbook
	}
	if err := w.readBook(); err != nil {
		return nil, err
	}
	return w, nil
}

// coreFormat is the edit core's view of SpreadsheetML: the main namespaces
// (new elements unprefixed, as Excel writes them) and no units.
var coreFormat = core.Format{
	Vocab: core.Vocab{SML, core.REL},
	Units: Units{},
	Type:  smlType,
	Bound: func(*tree.Node) bool { return false },
}

// SML is the SpreadsheetML namespace family (Transitional, Strict), the
// schema table "x".
var SML = core.NS{URIs: []string{sml.NSMain, sml.NSStrict}, Prefix: "x", Table: "x"}

// smlType is the schema type of a handle's element: a part's root
// (x:CT_Workbook, x:CT_Stylesheet, x:CT_Table) or, below it, the type
// its parent's table gives it.
func smlType(e *tree.Node) (string, bool) {
	if !SML.Has(e) {
		return "", false
	}
	p := e.Parent()
	if p == nil || p.Kind() != tree.ElementNode {
		return schema.RootType("x", e.Local())
	}
	pt, ok := smlType(p)
	if !ok {
		return "", false
	}
	return schema.ChildType(pt, "x:"+e.Local())
}

// Package returns the working copy.
func (w *Workspace) Package() *opc.Package { return w.pkg }

// Core returns the edit core's workspace, through which the small parts
// are changed (core.Workspace.Part declares the part as a whole).
func (w *Workspace) Core() *core.Workspace { return w.core }

// WorkbookPart returns the workbook part's name.
func (w *Workspace) WorkbookPart() string { return w.wb }

// WorkbookHandle opens the workbook part for editing: the core's
// whole-part region, which declares the part.
func (w *Workspace) WorkbookHandle() (*core.Handle, error) { return w.core.Part(w.wb) }

// Date1904 reports workbookPr@date1904.
func (w *Workspace) Date1904() bool { return w.date1904 }

// InputFullCalc reports whether the input already asked for
// recalculation on open (calcPr@fullCalcOnLoad), so that the
// stale-values hazard is new only when this plan set it.
func (w *Workspace) InputFullCalc() bool { return w.fullCalc }

// Sheets returns the sheet entries in tab order. The slice is the
// workspace's; callers do not change it.
func (w *Workspace) Sheets() []SheetRef { return w.sheets }

// Order returns the sheet names in tab order (formula.Sheets.Order).
func (w *Workspace) Order() []string {
	out := make([]string, len(w.sheets))
	for i, s := range w.sheets {
		out[i] = s.Name
	}
	return out
}

// SheetRef returns the entry of the sheet named name, ignoring case
// (Excel's rule).
func (w *Workspace) SheetRef(name string) (SheetRef, bool) {
	for _, s := range w.sheets {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return SheetRef{}, false
}

// ReloadBook reads the sheet entries again from the workbook tree, after
// an op changed them (sheet.add, sheet.rename). Sheets already open keep
// their overlays (they are keyed by part).
func (w *Workspace) ReloadBook() error {
	if err := w.readBook(); err != nil {
		return err
	}
	for _, s := range w.open {
		for _, r := range w.sheets {
			if opc.PartKey(r.Part) == opc.PartKey(s.ref.Part) {
				s.ref = r
			}
		}
	}
	return nil
}

// Sheet opens the worksheet named name (ignoring case) for editing, or
// returns the one already open. Errors: ErrNotFound (no such sheet);
// *Unreadable when its part is missing, is not a worksheet, is not well
// formed, or has rows or cells sheet.Scan skipped (out of order or
// outside the grid). Opening costs one sheet.Scan of the part and holds
// its original string.
func (w *Workspace) Sheet(name string) (*Sheet, error) {
	ref, ok := w.SheetRef(name)
	if !ok {
		return nil, fmt.Errorf("%w: sheet %s", ErrNotFound, name)
	}
	if s, ok := w.open[opc.PartKey(ref.Part)]; ok && ref.Part != "" {
		return s, nil
	}
	target := model.SheetID(ref.Name)
	switch {
	case ref.Part == "":
		return nil, &Unreadable{Target: target, Detail: "its part is missing"}
	case ref.Kind != KindWorksheet:
		return nil, &Unreadable{Target: target, Detail: "it is not a worksheet"}
	}
	s, err := openSheet(w, ref)
	if err != nil {
		return nil, err
	}
	w.open[opc.PartKey(ref.Part)] = s
	return s, nil
}

// Release drops an open sheet that has no edits, so that a pass over
// many sheets (a structural op's other sheets, sheet.rename) holds one
// at a time. It does nothing for a sheet with edits.
func (w *Workspace) Release(name string) {
	ref, ok := w.SheetRef(name)
	if !ok {
		return
	}
	s, ok := w.open[opc.PartKey(ref.Part)]
	if !ok || s.dirty() {
		return
	}
	delete(w.open, opc.PartKey(ref.Part))
	s.part.Forget()
}

// SST returns the shared strings index, opened on first use; nil when
// the workbook has none. The shared strings are only read: new text is
// written inline (decision 11). An unreadable part is *Unreadable.
func (w *Workspace) SST() (*sheet.SST, error) {
	if w.stringsDone {
		return w.strings, w.stringsErr
	}
	w.stringsDone = true
	if w.sst == "" || !w.pkg.Has(w.sst) {
		return nil, nil
	}
	pt, err := w.pkg.Part(w.sst)
	if err != nil {
		w.stringsErr = err
		return nil, err
	}
	src, _, err := pt.OriginalString()
	if err == nil {
		w.strings, err = sheet.OpenSST(src)
	}
	if err != nil {
		w.stringsErr = &Unreadable{Target: w.sst, Detail: "the shared strings cannot be read: " + err.Error()}
	}
	return w.strings, w.stringsErr
}

// MarkValues records that a cell value, formula or the structure of a
// sheet changed: Finish asks Excel to recalculate (fullCalcOnLoad).
// Sheet's edits call it; an op calls it only for a change made another
// way (a defined name's formula).
func (w *Workspace) MarkValues() { w.values = true }

// MarkFormulas records that a formula cell was added, removed or moved:
// Finish removes the calculation chain. It implies MarkValues.
func (w *Workspace) MarkFormulas() { w.values, w.formulas = true, true }

// RecalcRequested reports whether the save asks for recalculation (the
// stale-values hazard).
func (w *Workspace) RecalcRequested() bool { return w.values }

// AddPart adds a part of the given content type, declared as a whole
// (sheet.add's worksheet). The content type must be a ctypes constant.
func (w *Workspace) AddPart(name string, data []byte, contentType string) error {
	w.core.DeclarePart(name)
	w.core.DeclarePart(opc.ContentTypesName)
	_, err := w.pkg.AddPart(name, data, contentType)
	return err
}

// DeletePart deletes a part, declared as a whole (the calcChain).
func (w *Workspace) DeletePart(name string) error {
	w.core.DeclarePart(name)
	w.core.DeclarePart(opc.ContentTypesName)
	return w.pkg.DeletePart(name)
}

// Touched returns the keys of the parts the plan has changed or
// declared so far: the edit core's declared parts and every sheet with
// edits. The runner reads it after each op to name the first op that
// touched each part.
func (w *Workspace) Touched() []string {
	out := w.core.Declared().Parts()
	for k, s := range w.open {
		if s.dirty() && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Flush writes every open sheet's edits into its part (SetPatch,
// DeclareSpliced), in part order. Finish calls it; the op tests call it
// to see what ops wrote without the recalculation policy. A sheet
// flushed once is written again by a later Flush only when it changed
// since (its patch is rebuilt over the original).
func (w *Workspace) Flush() error {
	keys := slices.Sorted(maps.Keys(w.open))
	for _, k := range keys {
		if err := w.open[k].flush(); err != nil {
			return err
		}
	}
	return nil
}

// Finish ends the operations: Flush, then the recalculation policy
// (applyRecalcPolicy). It is called once, after the last op and before
// the transaction seals; a no-op plan's Finish changes nothing. A plan
// whose ops marked values or formulas but left every part as it was (a
// value set and set back, a name set to what it was) is a no-op too: the
// policy runs only when some part changed.
func (w *Workspace) Finish() error {
	if w.finished {
		return errors.New("edit: workspace already finished")
	}
	w.finished = true
	if err := w.Flush(); err != nil {
		return err
	}
	if w.values || w.formulas {
		changed, err := w.pkg.ChangedParts()
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			return nil
		}
	}
	return w.applyRecalcPolicy()
}

// Verify implements plan.Workspace: the working copy against the
// declared write set.
func (w *Workspace) Verify() error { return w.core.Verify() }

// declareSpliced declares spans of a sheet part as spliced.
func (w *Workspace) declareSpliced(part string, spans []splice.Span) {
	w.core.DeclareSpliced(part, spans...)
}

// Drop releases the working copy's contents once it is sealed: the open
// sheets (their originals, overlays and indexes), the shared strings and
// styles views, and every part's cached bytes (opc.Package.DropContents).
// apply calls it in the sealed callback before the gate, so the gate's
// scan of the output is not held beside the input sheet (the 1.3 N + T
// budget). The workspace must not be used after it.
func (w *Workspace) Drop() {
	w.open, w.strings, w.styles = map[string]*Sheet{}, nil, nil
	w.pkg.DropContents()
}
