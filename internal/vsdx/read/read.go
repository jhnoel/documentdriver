// Package read builds the vsdx read, find and xml results
// (docs/vsdx-contract-draft.md, sections 4 and 8) from the read model.
// Results are structs marshalled by cli.Marshal; lists are never nil.
package read

import (
	"encoding/json"

	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Options are the read options of the command line.
type Options struct {
	// Units is "pt" (default) or "in".
	Units string
	// Detail is "", "sites" or "full".
	Detail string
	// IDs keeps only these objects and their descendants.
	IDs []string
	// Region keeps objects whose box intersects x, y, w, h (view units).
	Region *[4]float64
}

// Summary is read without --page or --master.
type Summary struct {
	Base    string        `json:"base"`
	Pages   []PageEntry   `json:"pages"`
	Masters []MasterEntry `json:"masters"`
	// ValuesStale is set when the document asks Visio to recalculate
	// on open (RecalcDocument): cached values may be out of date.
	ValuesStale bool   `json:"values_stale,omitempty"`
	Notes       []Note `json:"notes"`
}

// PageEntry is one page of the summary, in pages.xml order.
type PageEntry struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Background  bool          `json:"background,omitempty"`
	BackPage    string        `json:"background_page,omitempty"`
	Size        []json.Number `json:"size,omitempty"`
	Orientation string        `json:"orientation,omitempty"`
	Objects     int           `json:"objects"`
}

// MasterEntry is one master of the summary, in masters.xml order.
type MasterEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	NameU   string `json:"name_u,omitempty"`
	Hidden  bool   `json:"hidden,omitempty"`
	Objects int    `json:"objects"`
}

// Note is contract section 4's {at, reason}.
type Note struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// Document reads the summary of the package in input. Page object
// counts come from a streaming tree.Scan of each page (no tree kept).
// Chunk R1.
func Document(input []byte, base string) (*Summary, error) {
	_, _ = input, base
	return nil, vsd.ErrNotImplemented
}

// Page reads one page (or master, for an address master<N>) as JSON in
// the layout pptx read --slide uses: base, page, name, units, size,
// orientation, background, background_page, layers, objects,
// connections, hazards, notes. Chunk R2.
func Page(input []byte, base, address string, o Options) ([]byte, error) {
	_, _, _, _ = input, base, address, o
	return nil, vsd.ErrNotImplemented
}
