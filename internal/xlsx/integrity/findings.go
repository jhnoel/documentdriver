package integrity

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Kind names a class of fault. Kinds are internal: they map to no hazard
// or contract error wording.
type Kind string

// The kinds, by area.
const (
	// The package.
	PackageUnreadable   Kind = "package.unreadable"
	PackageNoWorkbook   Kind = "package.no-workbook"
	PackageRels         Kind = "package.rels"
	PackageContentTypes Kind = "package.content-types"
	PackageNoType       Kind = "package.no-content-type"
	PackageTypeMismatch Kind = "package.content-type-mismatch"
	PackagePartMissing  Kind = "package.part-missing"
	PackagePartRead     Kind = "package.part-unreadable"
	PackageCalcChain    Kind = "package.calcchain"
	PackageStrict       Kind = "package.strict"
	XMLMalformed        Kind = "xml.malformed"

	// The workbook.
	WorkbookRoot         Kind = "workbook.root"
	WorkbookNoSheets     Kind = "workbook.no-sheets"
	WorkbookNoVisible    Kind = "workbook.no-visible-sheet"
	SheetNameDuplicate   Kind = "sheet.name-duplicate"
	SheetNameInvalid     Kind = "sheet.name-invalid"
	SheetNameHistory     Kind = "sheet.name-history"
	SheetNameApostrophe  Kind = "sheet.name-apostrophe"
	SheetIDDuplicate     Kind = "sheet.id-duplicate"
	SheetRIDDuplicate    Kind = "sheet.rid-duplicate"
	SheetRIDUnresolved   Kind = "sheet.rid-unresolved"
	NameDuplicate        Kind = "name.duplicate"
	NameRefShaped        Kind = "name.ref-shaped"
	NameInvalid          Kind = "name.invalid"
	NameLocalSheet       Kind = "name.local-sheet-range"
	NameEmptyContainer   Kind = "name.empty-container"
	LimitSheetName       Kind = "limits.sheet-name"
	LimitDefinedNameSize Kind = "limits.defined-name"

	// Worksheets.
	WorksheetRoot       Kind = "worksheet.root"
	WorksheetChildOrder Kind = "worksheet.child-order"
	WorksheetChildCount Kind = "worksheet.child-count"
	WorksheetEmpty      Kind = "worksheet.empty-container"
	WorksheetRowOrder   Kind = "worksheet.row-order"
	WorksheetCellOrder  Kind = "worksheet.cell-order"
	WorksheetCellRow    Kind = "worksheet.cell-row"
	WorksheetCellRef    Kind = "worksheet.cell-ref"
	WorksheetCellType   Kind = "worksheet.cell-type"
	WorksheetStyle      Kind = "worksheet.style-range"
	WorksheetSST        Kind = "worksheet.sst-range"
	WorksheetSharedOrph Kind = "worksheet.shared-orphan"
	WorksheetSharedDup  Kind = "worksheet.shared-duplicate-master"
	WorksheetArrayRef   Kind = "worksheet.array-ref"
	WorksheetMergeRef   Kind = "worksheet.merge-ref"
	WorksheetMergeOver  Kind = "worksheet.merge-overlap"
	WorksheetCols       Kind = "worksheet.cols"
	WorksheetDimension  Kind = "worksheet.dimension"
	WorksheetTableRel   Kind = "worksheet.table-rel-unresolved"
	LimitRow            Kind = "limits.row"
	LimitColumn         Kind = "limits.column"
	LimitCellText       Kind = "limits.cell-text"
	LimitFormula        Kind = "limits.formula"

	// Tables.
	TableHeaderMismatch Kind = "table.header-mismatch"
	TableHeaderMissing  Kind = "table.header-missing"
	TableHeaderRows     Kind = "table.header-row-count"
	TableRef            Kind = "table.ref"
	TableColumnCount    Kind = "table.column-count"
	TableColumnDup      Kind = "table.column-name-duplicate"
	TableNameDuplicate  Kind = "table.name-duplicate"
	TableIDDuplicate    Kind = "table.id-duplicate"
	TableFilter         Kind = "table.autofilter-outside"

	// Styles and shared strings.
	StylesMissing   Kind = "styles.missing"
	StylesRange     Kind = "styles.id-range"
	StylesCount     Kind = "styles.count"
	StylesEmpty     Kind = "styles.empty-container"
	SSTUniqueCount  Kind = "sst.unique-count"
	LimitSSTItemLen Kind = "limits.shared-string"
)

// Finding is one error kind in one part: the identity the gate compares.
// Key narrows the kind to what is at fault without saying where (a
// lowercased sheet name, a style index, a header text), so moving a fault
// does not make a new one; it is "" when the kind needs none.
type Finding struct {
	Kind Kind
	// Part is the part name ("/xl/worksheets/sheet1.xml"), "" for the
	// package as a whole.
	Part string
	Key  string
}

// compareFindings compares two findings by part, kind and key for sorting.
func compareFindings(a, b Finding) int {
	return cmp.Or(strings.Compare(a.Part, b.Part), strings.Compare(string(a.Kind), string(b.Kind)), strings.Compare(a.Key, b.Key))
}

// String returns the finding's kind, part and key as a readable string.
func (f Finding) String() string {
	s := string(f.Kind)
	if f.Part != "" {
		s += " in " + f.Part
	}
	if f.Key != "" {
		s += " (" + f.Key + ")"
	}
	return s
}

// Instance is one occurrence of a Finding with where it was found, in the
// words of the workbook ("Sheet1!B7"). The Detail is not part of its
// identity.
type Instance struct {
	Finding
	Detail string
}

// Bounds that keep a hostile package's findings small: instances kept per
// kind and part, and distinct keys per kind and part (more share the
// key "").
const (
	maxInstances = 200
	maxKeys      = 256
)

// Findings counts the findings of one workbook. The zero value is ready.
type Findings struct {
	counts    map[Finding]int
	instances []Instance
	perKind   map[[2]string]int
	keys      map[[2]string]int
	notes     []string
}

// add records one occurrence.
func (f *Findings) add(kind Kind, part, key, detail string) {
	if f.counts == nil {
		f.counts = map[Finding]int{}
		f.perKind = map[[2]string]int{}
		f.keys = map[[2]string]int{}
	}
	kp := [2]string{string(kind), part}
	k := Finding{kind, part, key}
	if _, seen := f.counts[k]; !seen && key != "" {
		if f.keys[kp] >= maxKeys {
			k.Key = ""
		} else {
			f.keys[kp]++
		}
	}
	f.counts[k]++
	if f.perKind[kp] < maxInstances {
		f.perKind[kp]++
		f.instances = append(f.instances, Instance{k, detail})
	}
}

// note records a statement about what was not checked, once.
func (f *Findings) note(s string) {
	if !slices.Contains(f.notes, s) {
		f.notes = append(f.notes, s)
	}
}

// Kinds returns the findings, ordered by part, kind, then key.
func (f *Findings) Kinds() []Finding {
	out := slices.Collect(maps.Keys(f.counts))
	slices.SortFunc(out, compareFindings)
	return out
}

// Count returns how often the finding k occurs.
func (f *Findings) Count(k Finding) int { return f.counts[k] }

// Has reports whether the finding k occurs.
func (f *Findings) Has(k Finding) bool { return f.counts[k] > 0 }

// HasKind reports whether any finding of the kind occurs.
func (f *Findings) HasKind(k Kind) bool {
	for fi := range f.counts {
		if fi.Kind == k {
			return true
		}
	}
	return false
}

// Len returns the number of distinct findings.
func (f *Findings) Len() int { return len(f.counts) }

// Empty reports whether there are no findings.
func (f *Findings) Empty() bool { return len(f.counts) == 0 }

// Instances returns the occurrences kept (at most 200 per kind and part),
// in the order found.
func (f *Findings) Instances() []Instance { return slices.Clone(f.instances) }

// Notes returns what Check did not check (pending work), in the order
// noted.
func (f *Findings) Notes() []string { return slices.Clone(f.notes) }

// Introduced returns the findings of which after has more occurrences
// than before: new ones, and more of a kind the input already had in that
// part with that key. Faults the input had never count.
func Introduced(before, after *Findings) []Finding {
	var out []Finding
	for _, k := range after.Kinds() {
		if after.Count(k) > before.Count(k) {
			out = append(out, k)
		}
	}
	return out
}

// GateFailure is a refusal: what the edit would have broken.
type GateFailure struct {
	Introduced []Finding
	// Instances are the occurrences of the introduced findings that
	// were kept, for the message.
	Instances []Instance
}

// Error implements error.
func (g *GateFailure) Error() string {
	var lines []string
	for _, in := range g.Instances {
		l := in.String()
		if in.Detail != "" {
			l += ": " + in.Detail
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		for _, k := range g.Introduced {
			lines = append(lines, k.String())
		}
	}
	const shown = 12
	if more := len(lines) - shown; more > 0 {
		lines = append(lines[:shown], fmt.Sprintf("and %d more", more))
	}
	return "the edit would damage the workbook: " + strings.Join(lines, "; ")
}

// Gate is the commit rule: output may not have a finding input lacks. It
// returns nil or a *GateFailure.
func Gate(input, output []byte) error {
	return GateFindings(Check(input), Check(output))
}

// GateFindings is Gate over two checks already made.
func GateFindings(before, after *Findings) error {
	intro := Introduced(before, after)
	if len(intro) == 0 {
		return nil
	}
	g := &GateFailure{Introduced: intro}
	for _, in := range after.instances {
		if slices.Contains(intro, in.Finding) {
			g.Instances = append(g.Instances, in)
		}
	}
	return g
}
