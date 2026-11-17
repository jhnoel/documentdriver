// Package model is the read model of one Visio page or master: its
// objects in paint order with their effective values (after master and
// style inheritance), the glue between them as a graph, its layers,
// the hazards it already has and the notes about what was left out. It
// is a projection only and is never serialised back into the file.
package model

import (
	"github.com/jhnoel/documentdriver/internal/geom"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
)

// Kind is an object kind, as pptx names them.
type Kind string

// Object kinds: a 2-D shape, a 1-D shape (connector or line), a group,
// a Foreign shape holding an image (picture) and anything else read
// only (other: OLE, ink, guides).
const (
	Shape     Kind = "shape"
	Connector Kind = "connector"
	Group     Kind = "group"
	Picture   Kind = "picture"
	Other     Kind = "other"
)

// Page is the model of one page or master.
type Page struct {
	ID          string // "page0" or "master2"
	Name, NameU string
	Background  bool
	BackPage    string // address of the background page, "" when none
	// W and H are PageWidth and PageHeight in inches; HasSize false
	// (and a note) when they are not derived.
	W, H    float64
	HasSize bool
	// Orientation is derived from the size: "portrait" when H > W,
	// "landscape" when W > H, "" when square or not derived.
	// PrintOrientation is the printer paper orientation,
	// PrintPageOrientation 1 or 2 from the page sheet or its style chain
	// (55 corpus documents store it only in StyleSheet 0), "" otherwise.
	// Pending owner decision 6.
	Orientation      string
	PrintOrientation string
	Layers           []Layer
	Objects          []*Object
	Connections      []Connection
	Hazards          []Hazard
	Notes            []doc.Note
}

// Object is one shape.
type Object struct {
	ID          string // address, page0/s5 or page0/s5~2
	ShapeID     uint32
	Kind        Kind
	Type        string // the Type attribute as stored
	Name, NameU string
	Master      string // address of the master, "" when none
	MasterName  string
	Parent      string // the group's address, "" at the top level
	Box         *geom.Box
	From, To    *geom.Pt // 1-D ends in view space
	FromGlue    string
	ToGlue      string
	Fill        *Paint
	Line        *Line
	HTML        string
	HasText     bool
	Text        string // plain text, as find matches it
	Props       []Prop
	Layers      []string // layer addresses
	Sites       []Site
	Glued       []Glued // Connects from this shape's non-end cells (Controls.Row_1)
	Z           int
	Origins     map[string]inherit.Origin
	Cells       []RawCell // --detail full: the shape's own stored cells
	Omitted     []Omission
	Node        *tree.Node
}

// Paint is a fill: None, or a colour "#RRGGBB" (Hex).
type Paint struct {
	None bool
	Hex  string
}

// Line is a line: None, or its weight in points, colour and Visio
// LinePattern number as stored (Dash).
type Line struct {
	None  bool
	W     float64
	HasW  bool
	Color string
	Dash  string
}

// Prop is one effective shape data row.
type Prop struct {
	ID     string // page0/s5/prop:Cost
	Label  string
	Type   string // string, number, boolean, date, duration, currency, fixed_list, variable_list
	Value  any    // string, float64, bool, or map{"date": ...}
	Format string
	Hidden bool
	Linked bool // DataLinked: read only in P2
	Origin inherit.Origin
}

// Layer is one row of the page's Layer section.
type Layer struct {
	ID                   string // page0/layer:Connector
	Name                 string
	Index                int // the row's IX, as LayerMember stores it
	Visible, Print, Lock bool
}

// Connection is one connector's glue as a graph edge: the connector,
// the shapes its ends are glued to ("" for a free end) and the glue as
// read shows it (vsd.Glue).
type Connection struct {
	Connector        string
	From, To         string
	FromGlue, ToGlue string
}

// Hazard is a place where the page contradicts itself (contract
// section 4): stale-connector, dangling-glue.
type Hazard struct {
	Kind, ID, Detail string
}

// Site is a connection point in the view: its 1-based number (the n of
// Connections.Xn, row IX + 1) and position.
type Site struct {
	N  int
	Pt geom.Pt
}

// Glued is one Connect row whose FromCell is not BeginX or EndX (a
// control handle glued to a shape, 48 in the corpus): the cell as
// stored and the glue as vsd.Glue shows it. Read only.
type Glued struct {
	Cell, To string
}

// RawCell is a stored cell as --detail full shows it.
type RawCell struct {
	Key     string // "PinX", "Prop.Cost.Value", "Geometry1.X2"
	V, U, F string
}

// Omission records a property that is not shown and why.
type Omission struct {
	Prop, Reason string
}
