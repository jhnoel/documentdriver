package model

import (
	"slices"
	"strconv"
	"strings"
)

// Note is one entry of a result's `notes` (contract §8, §10): what a
// read could not derive or chose not to show, and where. It is the
// shape docx and pptx print, {"at","reason"} (approved for xlsx).
type Note struct {
	// At is an address the agent can use: "package", "sheet:Budget", a
	// sheet-qualified cell or range, "name:X", "table:T", or a part name
	// ("/xl/styles.xml") when the note is about a part with no address.
	At string `json:"at"`
	// Reason is one sentence. Wording may change (contract intro).
	Reason string `json:"reason"`
	// Kind is the machine class of the note, for the corpus counts and
	// tests; it is not printed.
	Kind NoteKind `json:"-"`
}

// NoteKind classifies notes. Every note a reader adds has one of these
// kinds; the corpus sweep counts notes by kind.
type NoteKind string

// The note kinds. A reason never embeds cell content or other unbounded
// document text beyond an address or a format code cut to 64 bytes, so a
// note's size is bounded.
const (
	// NotePackage: an opc leniency note (opc.Package.Notes).
	NotePackage NoteKind = "package"
	// NotePart: an auxiliary part (styles, shared strings, metadata, a
	// table, a sheet's relationships) is missing or unreadable; the facet
	// it carries is left out and the note says which.
	NotePart NoteKind = "part"
	// NoteSheetKind: a chartsheet, dialogsheet or macrosheet: listed in
	// the summary, its cells are not read.
	NoteSheetKind NoteKind = "sheet_kind"
	// NoteSheet: a worksheet the summary could not scan (dimension left
	// out), or a sheet entry whose relationship or part is missing.
	NoteSheet NoteKind = "sheet"
	// NoteDuration: a number with a duration format ([h]:mm) shown as a
	// number (decision 9).
	NoteDuration NoteKind = "duration"
	// NoteSerial: a number with a date or time format whose serial a
	// date cannot show (1900-system serials below 61, out of range,
	// negative): shown as a number (decision 9).
	NoteSerial NoteKind = "serial"
	// NoteValue: a cell whose stored value cannot be read (a number that
	// is not one, a bool other than 0/1, a shared string index out of
	// range, an unparsable t="d" date, an unknown t): left out, or for a
	// t="d" shown as its stored text.
	NoteValue NoteKind = "value"
	// NoteFormula: a formula that could not be written out (a shared
	// child whose master is missing or whose Translate refuses): its
	// cached value is shown without "f".
	NoteFormula NoteKind = "formula"
	// NoteDataTable: a data-table anchor; its formula is as stored, or
	// absent when the file stores none (brief: "show the formula as
	// stored with a note").
	NoteDataTable NoteKind = "data_table"
	// NoteArray: an array formula whose dynamic or CSE kind cannot be
	// derived (cm without readable metadata).
	NoteArray NoteKind = "array"
	// NoteRichValue: a cell with value metadata (vm), such as a picture in
	// a cell or a linked data type; the stored fallback value is shown.
	NoteRichValue NoteKind = "rich_value"
	// NoteOrder: a row or cell out of order or repeated: it is skipped.
	NoteOrder NoteKind = "order"
	// NoteGrid: a row or cell outside the grid (row past 1,048,576,
	// column past XFD, or an unparsable r): it is skipped.
	NoteGrid NoteKind = "grid"
	// NoteTruncated: read or find stopped at --limit (decision 6).
	NoteTruncated NoteKind = "truncated"
	// NoteStale: values_stale is set (decision 7).
	NoteStale NoteKind = "stale"
	// NoteName: a defined name whose scope cannot be resolved.
	NoteName NoteKind = "name"
	// NoteMore: the last note when MaxNotes was reached.
	NoteMore NoteKind = "more"
)

// MaxNotes bounds a result's notes. Past it, one NoteMore note says how
// many were left out (by kind counts in its reason).
const MaxNotes = 1000

// Notes collects a result's notes in the order first added, without
// duplicates, aggregating per-cell notes. Its memory is O(distinct
// notes), never O(cells).
//
// The zero Notes is ready to use. Notes is not safe for concurrent use.
type Notes struct {
	list []Note
	// count[i] is how many cells list[i] stands for (AddCell; 1 for Add).
	count []int
	seen  map[noteKey]int // index into list
	// dropped counts notes past MaxNotes, by kind.
	dropped map[NoteKind]int
}

// noteKey identifies a note for deduplication: Add uses (at, reason),
// AddCell (kind, sheet, reason) with cell set.
type noteKey struct {
	kind   NoteKind
	at     string
	reason string
	cell   bool
}

// Add records a note, once: a second Add with the same at and reason is
// dropped. Past MaxNotes notes, Add only counts what it drops.
func (n *Notes) Add(kind NoteKind, at, reason string) {
	n.add(noteKey{at: at, reason: reason}, Note{At: at, Reason: reason, Kind: kind})
}

// AddCell records a per-cell note aggregated by (kind, sheet, reason):
// the first cell's address is At, and List reports the count of further
// cells in the reason ("...; 1204 cells in all, the first at Budget!A1"
// once there is more than one; the exact wording is free). The
// aggregation keeps a sheet with a million duration cells to one note
// per distinct reason (aggregation approved).
func (n *Notes) AddCell(kind NoteKind, sheet string, row, col int, reason string) {
	k := noteKey{kind: kind, at: sheet, reason: reason, cell: true}
	if i, ok := n.seen[k]; ok {
		n.count[i]++
		return
	}
	n.add(k, Note{At: Cell(sheet, row, col), Reason: reason, Kind: kind})
}

// add records note under key k once, counting it as dropped past MaxNotes.
func (n *Notes) add(k noteKey, note Note) {
	if _, ok := n.seen[k]; ok {
		return
	}
	if len(n.list) >= MaxNotes {
		if n.dropped == nil {
			n.dropped = map[NoteKind]int{}
		}
		n.dropped[note.Kind]++
		return
	}
	if n.seen == nil {
		n.seen = map[noteKey]int{}
	}
	n.seen[k] = len(n.list)
	n.list = append(n.list, note)
	n.count = append(n.count, 1)
}

// Len is the number of notes List would return.
func (n *Notes) Len() int {
	if len(n.dropped) > 0 {
		return len(n.list) + 1
	}
	return len(n.list)
}

// List returns the notes, never nil (a result always has `notes`, even
// empty). Aggregated reasons are written out here.
func (n *Notes) List() []Note {
	out := make([]Note, 0, n.Len())
	for i, note := range n.list {
		if n.count[i] > 1 {
			note.Reason += "; " + strconv.Itoa(n.count[i]) + " cells in all, the first at " + note.At
		}
		out = append(out, note)
	}
	if len(n.dropped) > 0 {
		kinds := make([]string, 0, len(n.dropped))
		total := 0
		for k, c := range n.dropped {
			kinds = append(kinds, string(k)+" "+strconv.Itoa(c))
			total += c
		}
		slices.Sort(kinds)
		out = append(out, Note{At: "package", Kind: NoteMore,
			Reason: strconv.Itoa(total) + " more notes left out (" + strings.Join(kinds, ", ") + ")"})
	}
	return out
}
