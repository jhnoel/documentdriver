package read

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/opc"
	"github.com/jhnoel/documentdriver/internal/xlsx/model"
	"github.com/jhnoel/documentdriver/internal/xlsx/sheet"
	"github.com/jhnoel/documentdriver/internal/xlsx/sml"
)

// Options are what read takes from outside the package.
type Options struct {
	// Limits are the package's safety limits; the zero Limits means
	// opc.DefaultLimits (decision 15). Tests lower them.
	Limits opc.Limits
	// Checkpoint, when set, is called where reading holds the most (a
	// sheet scanned with its output beside it), for the memory test
	// (laws.MeasureMemory).
	Checkpoint func(string)
	// MaxOutput bounds the encoded cells of a read and the matches of a
	// find, in bytes; 0 means DefaultMaxOutput. Tests lower it.
	MaxOutput int
}

// DefaultMaxOutput is the output budget of one read or find: 64 MiB. A
// read past it stops at a row boundary with truncated.next, as --limit
// does; a find stops with truncated.after. A single row whose cells
// alone pass it is refused (operation_failed: read fewer columns).
// --limit counts cells, and a cell can hold up to 32,767 characters, so
// the cell limit alone does not bound memory.
const DefaultMaxOutput = 64 << 20

// maxOutput is opt.MaxOutput or the default.
func (o Options) maxOutput() int {
	if o.MaxOutput > 0 {
		return o.MaxOutput
	}
	return DefaultMaxOutput
}

// mark calls the checkpoint, if any.
func (o Options) mark(name string) {
	if o.Checkpoint != nil {
		o.Checkpoint(name)
	}
}

// DefaultLimit is read's and find's --limit when the flag is absent:
// 10,000 non-empty cells for read (decision 6) and 10,000 matches for
// find (approved).
const DefaultLimit = 10000

// SheetKind is what a <sheet> entry's relationship points at.
type SheetKind uint8

// The sheet kinds. Only a Worksheet has cells to read; the others are
// listed in the summary with a NoteSheetKind note.
const (
	Worksheet SheetKind = iota
	Chartsheet
	Dialogsheet
	Macrosheet
	// MissingSheet is a <sheet> whose relationship is absent, external,
	// of another type, or points at a part the package lacks (a
	// NoteSheet note).
	MissingSheet
)

// SheetInfo is one <sheet> of workbook.xml, in tab order.
type SheetInfo struct {
	// Name is the stored name (sheet@name).
	Name string
	// State is sheet@state as stored: "visible" (also when absent),
	// "hidden" or "veryHidden". The summary prints it through
	// stateName.
	State string
	// Kind is from the relationship type (ctypes.RelWorksheet and the
	// others; compared with ctypes.Is, so Strict types match).
	Kind SheetKind
	// Part is the target part name ("" for MissingSheet).
	Part string
	// Pos is the 0-based position in <sheets>, which a defined name's
	// localSheetId refers to.
	Pos int
}

// DefinedName is one <definedName> of workbook.xml.
type DefinedName struct {
	// Name is definedName@name as stored ("_xlnm.Print_Area" keeps its
	// prefix).
	Name string
	// Local is the sheet position of localSheetId, or -1 for workbook
	// scope. A localSheetId that is not a valid position is -2 (the
	// summary leaves scope out and adds a NoteName note).
	Local int
	// Text is the stored formula (the element's text, not trimmed).
	Text string
	// Hidden is definedName@hidden.
	Hidden bool
}

// Book is an open workbook: the package, the workbook part's facts, and
// the auxiliary parts loaded on first use. A Book is used by one command
// and is not safe for concurrent use.
type Book struct {
	// Base is cli.Hash(input).
	Base string
	// Sheets are the <sheet> entries in tab order; Names the
	// <definedName> entries in document order.
	Sheets []SheetInfo
	Names  []DefinedName
	// Date1904 is workbookPr@date1904 ("1" or "true").
	Date1904 bool
	// FullCalcOnLoad is calcPr@fullCalcOnLoad ("1" or "true"): read's
	// values_stale (decision 7).
	FullCalcOnLoad bool
	// Notes collect the notes of the command using the Book, starting
	// with opc's leniency notes (NotePackage, at "package").
	Notes *model.Notes

	pkg *opc.Package
	// The workbook part and its relationships.
	part string
	rels *opc.Rels
	// Lazily loaded parts; done says the load was tried (a failed load
	// is noted once and stays nil).
	styles     *Styles
	stylesDone bool
	sst        *sheet.SST
	sstDone    bool
	meta       *Metadata
	metaDone   bool
	tables     map[string][]Table // by sheet part
	// fatal is the first opc.ErrLimit trip of a lazy load (Err).
	fatal error
}

// openBook opens input as a workbook: opc.OpenBytes with opt.Limits, the
// package relationships' officeDocument target (the first internal one),
// its content type (one of the SpreadsheetML or macro-enabled workbook
// and template main types: ctypes.CTSpreadsheetmlSheetMain,
// CTSpreadsheetmlTemplateMain, CTMsExcelSheetMacroEnabledMain,
// CTMsExcelTemplateMacroEnabledMain; anything else, such as a binary
// workbook, is invalid_document), its tree (the <workbook> root in
// either main namespace), and from it: sheets (with their relationship
// kinds and parts), defined names, workbookPr@date1904 and
// calcPr@fullCalcOnLoad.
//
// Namespaces: elements in either main namespace (Transitional or
// Strict) are read; a sheet's r:id is the attribute in the
// relationships namespace of either flavour
// (http://schemas.openxmlformats.org/officeDocument/2006/relationships
// or http://purl.oclc.org/ooxml/officeDocument/relationships), found by
// namespace, not by the prefix "r". Relationship types are compared with
// ctypes.Is, so Strict types match.
//
// Errors are the invalid_document cases of doc.go. A sheet entry with a
// missing relationship or part is not an error: it is a MissingSheet
// with a NoteSheet note (contract §10). The workbook relationships
// failing to parse is invalid_document (no sheet can be found).
func openBook(input []byte, opt Options) (*Book, error) {
	pkg, err := opc.OpenBytes(input, opt.Limits)
	if err != nil {
		return nil, invalidDocument("the file cannot be opened as a workbook", err)
	}
	fail := func(msg string, cause error) (*Book, error) {
		pkg.Close() //nolint:errcheck // an in-memory package holds nothing to release
		return nil, invalidDocument(msg, cause)
	}

	root, err := pkg.Rels("/")
	if err != nil {
		return fail("the package relationships cannot be read", err)
	}
	var wb string
	for _, r := range root.All() {
		if !ctypes.Is(r.Type, ctypes.RelOfficeDocument) || r.External() {
			continue
		}
		if wb, err = root.TargetPart(r); err == nil {
			break
		}
	}
	if wb == "" {
		return fail("the package has no workbook part", nil)
	}
	if !pkg.Has(wb) {
		return fail("the workbook part is missing", nil)
	}

	ct, err := pkg.ContentTypes()
	if err != nil {
		return fail("the package has no readable content types", err)
	}
	got, ok := ct.Lookup(wb)
	if !ok {
		return fail("the workbook part has no content type", nil)
	}
	switch got {
	case ctypes.CTSpreadsheetmlSheetMain, ctypes.CTSpreadsheetmlTemplateMain,
		ctypes.CTMsExcelSheetMacroEnabledMain, ctypes.CTMsExcelTemplateMacroEnabledMain:
	default:
		return fail("the workbook part is not a SpreadsheetML workbook", nil)
	}

	pt, err := pkg.Part(wb)
	if err != nil {
		return fail("the workbook part is missing", err)
	}
	doc, err := pt.Tree()
	if err != nil {
		return fail("the workbook part is not well-formed XML", err)
	}
	wroot := doc.Root()
	if wroot == nil || !isWorkbookRoot(wroot) {
		return fail("the workbook part has no workbook root", nil)
	}
	rels, err := pkg.Rels(wb)
	if err != nil {
		return fail("the workbook relationships cannot be read", err)
	}

	b := &Book{
		Base:  cli.Hash(input),
		Notes: &model.Notes{},
		pkg:   pkg,
		part:  wb,
		rels:  rels,
	}
	for _, n := range pkg.Notes() {
		b.Notes.Add(model.NotePackage, "package", n)
	}

	ns := wroot.Space()
	if pr := wroot.Child(ns, "workbookPr"); pr != nil {
		if v, ok := pr.Attr("date1904"); ok {
			b.Date1904 = sml.IsTrue(v)
		}
	}
	if cp := wroot.Child(ns, "calcPr"); cp != nil {
		if v, ok := cp.Attr("fullCalcOnLoad"); ok {
			b.FullCalcOnLoad = sml.IsTrue(v)
		}
	}
	b.Sheets = readSheets(wroot.Child(ns, "sheets"), rels, pkg, b.Notes)
	for _, n := range sml.DefinedNames(wroot.Child(ns, "definedNames"), len(b.Sheets)) {
		b.Names = append(b.Names, DefinedName{Name: n.Name, Local: n.Local, Text: n.Text, Hidden: n.Hidden})
	}
	return b, nil
}

// isWorkbookRoot reports whether n is a <workbook> root in a main
// namespace.
func isWorkbookRoot(n *tree.Node) bool {
	return sml.IsMain(n.Space()) && n.Local() == "workbook"
}

// readSheets reads the <sheet> entries of sheetsEl (which may be nil),
// resolving each through sml.SheetEntries and sml.ResolveSheet and
// recording a NoteSheet note for each entry that resolves to a missing
// or non-worksheet part.
func readSheets(sheetsEl *tree.Node, rels *opc.Rels, pkg *opc.Package, notes *model.Notes) []SheetInfo {
	if sheetsEl == nil {
		return nil
	}
	// One pass over the relationships into a map: opc.Rels.Get walks
	// them all on every call, which made a workbook of n sheets cost
	// O(n^2) to open (AGENTS.md: never scan every relationship in a
	// per-element loop). The first relationship with an id wins, as Get
	// answers.
	byID := sml.RelsByID(rels)
	var out []SheetInfo
	for _, e := range sml.SheetEntries(sheetsEl) {
		info := SheetInfo{Pos: e.Pos, Name: e.Name, State: e.State}
		part, kind, problem := sml.ResolveSheet(e, rels, byID, pkg)
		info.Part = part
		switch kind {
		case sml.KindWorksheet:
			info.Kind = Worksheet
		case sml.KindChartsheet:
			info.Kind = Chartsheet
		case sml.KindDialogsheet:
			info.Kind = Dialogsheet
		case sml.KindMacrosheet:
			info.Kind = Macrosheet
		default:
			info.Kind = MissingSheet
		}
		if problem != "" {
			notes.Add(model.NoteSheet, model.SheetID(info.Name), problem)
		}
		out = append(out, info)
	}
	return out
}

// recordLimit records the first opc.ErrLimit trip of a lazy load, for
// Err.
func (b *Book) recordLimit(err error) {
	if b.fatal == nil && errors.Is(err, opc.ErrLimit) {
		b.fatal = err
	}
}

// target is the target part name of the first internal relationship of
// the given type from the workbook's relationships, or "" when there is
// none or its part is missing.
func (b *Book) target(rel string) string {
	for _, r := range b.rels.All() {
		if !ctypes.Is(r.Type, rel) || r.External() {
			continue
		}
		p, err := b.rels.TargetPart(r)
		if err != nil || !b.pkg.Has(p) {
			continue
		}
		return p
	}
	return ""
}

// Err is the invalid_document error of the first safety-limit trip
// (errors.Is(err, opc.ErrLimit)) of any part the Book read lazily
// (SheetText, SST, Styles, Metadata, Tables), or nil. A loader that trips
// a limit records it here and returns what it would for an unreadable
// part; every command checks Err before returning its result, and
// returns Err instead (contract §10: a limit trip is invalid_document).
func (b *Book) Err() error {
	return b.fatal
}

// Close releases the package.
func (b *Book) Close() {
	if b.pkg != nil {
		b.pkg.Close() //nolint:errcheck // an in-memory package holds nothing to release
	}
}

// Sheet finds a sheet by name, ignoring case as Excel does
// (strings.EqualFold); ok is false when there is none.
func (b *Book) Sheet(name string) (*SheetInfo, bool) {
	for i := range b.Sheets {
		if strings.EqualFold(b.Sheets[i].Name, name) {
			return &b.Sheets[i], true
		}
	}
	return nil, false
}

// SheetText returns a worksheet part's text (opc.Part.OriginalString,
// no copy). The error, for a missing or undecompressable part, makes the
// sheet unreadable; a limit trip is also recorded for Err.
func (b *Book) SheetText(sh *SheetInfo) (string, error) {
	pt, err := b.pkg.Part(sh.Part)
	if err != nil {
		return "", err
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		b.recordLimit(err)
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("sheet: part %s has no stored bytes", sh.Part)
	}
	return s, nil
}

// Forget drops a sheet part's cached text after the caller is done with
// it (opc.Part.Forget), so the summary and find hold one sheet at a time.
func (b *Book) Forget(sh *SheetInfo) {
	if sh == nil || sh.Part == "" {
		return
	}
	pt, err := b.pkg.Part(sh.Part)
	if err != nil {
		return
	}
	pt.Forget()
}

// SST returns the shared strings, indexed on first use (the workbook's
// RelSharedStrings target, sheet.OpenSST of its text). It is nil when
// the workbook has none, or when the part is unreadable (a NotePart note
// at the part name, once). The part's text is held while the Book is.
func (b *Book) SST() *sheet.SST {
	if b.sstDone {
		return b.sst
	}
	b.sstDone = true
	part := b.target(ctypes.RelSharedStrings)
	if part == "" {
		return nil
	}
	pt, err := b.pkg.Part(part)
	if err != nil {
		b.Notes.Add(model.NotePart, part, "the shared strings part is missing")
		return nil
	}
	s, ok, err := pt.OriginalString()
	if err != nil {
		b.recordLimit(err)
		b.Notes.Add(model.NotePart, part, "the shared strings part cannot be read")
		return nil
	}
	if !ok {
		b.Notes.Add(model.NotePart, part, "the shared strings part has no stored bytes")
		return nil
	}
	sst, err := sheet.OpenSST(s)
	if err != nil {
		b.Notes.Add(model.NotePart, part, "the shared strings part cannot be read")
		return nil
	}
	b.sst = sst
	return sst
}

// sheetOf is the sheet the scope of name n refers to ("" for workbook
// scope or an invalid localSheetId).
func (b *Book) sheetOf(n DefinedName) string {
	if n.Local < 0 || n.Local >= len(b.Sheets) {
		return ""
	}
	return model.SheetID(b.Sheets[n.Local].Name)
}

// stateName is the summary's state value: cli.Snake of the stored state
// ("visible", "hidden", "very_hidden"); an unknown stored value is shown
// through cli.Snake too (approved).
func stateName(stored string) string {
	return cli.Snake(stored)
}
