package ops

// Address fields are strings in the forms of vsd.Parse; the rules check
// their kind. Lengths are in the plan's units.

// ObjectAddSpec is object.add: a shape from a master (Master) or a
// master-less shape (Geom "rect" or "ellipse"), or a master-less
// connector (Kind "connector" with From/To or glue), at a page or
// group, with optional text and name.
type ObjectAddSpec struct {
	At       string     `json:"at"`
	As       string     `json:"as,omitempty"`
	Kind     string     `json:"kind,omitempty"`
	Master   string     `json:"master,omitempty"`
	Geom     string     `json:"geom,omitempty"`
	Box      []float64  `json:"box,omitempty"`
	Pin      []float64  `json:"pin,omitempty"`
	Name     string     `json:"name,omitempty"`
	HTML     *string    `json:"html,omitempty"`
	Fill     *PaintSpec `json:"fill,omitempty"`
	Line     *LineSpec  `json:"line,omitempty"`
	From     []float64  `json:"from,omitempty"`
	To       []float64  `json:"to,omitempty"`
	FromGlue string     `json:"from_glue,omitempty"`
	ToGlue   string     `json:"to_glue,omitempty"`
}

// ObjectSetSpec is object.set (the fields create and P2 share first).
type ObjectSetSpec struct {
	At       string     `json:"at"`
	Box      []float64  `json:"box,omitempty"`
	Rot      *float64   `json:"rot,omitempty"`
	FlipH    *bool      `json:"flip_h,omitempty"`
	FlipV    *bool      `json:"flip_v,omitempty"`
	Name     *string    `json:"name,omitempty"`
	Fill     *PaintSpec `json:"fill,omitempty"`
	Line     *LineSpec  `json:"line,omitempty"`
	From     []float64  `json:"from,omitempty"`
	To       []float64  `json:"to,omitempty"`
	FromGlue *string    `json:"from_glue,omitempty"`
	ToGlue   *string    `json:"to_glue,omitempty"`
}

// PaintSpec is a fill: "none" or {"hex": "#RRGGBB"}, as read shows it.
type PaintSpec struct {
	None bool
	Hex  string
}

// LineSpec is a line: "none" or {w, color, dash}.
type LineSpec struct {
	None  bool
	W     *float64
	Color string
	Dash  string
}

// TextSetSpec is text.set.
type TextSetSpec struct {
	At   string `json:"at"`
	HTML string `json:"html"`
}

// PropAddSpec is prop.add.
type PropAddSpec struct {
	At     string `json:"at"`
	Name   string `json:"name"`
	Label  string `json:"label,omitempty"`
	Type   string `json:"type"`
	Value  any    `json:"value,omitempty"`
	Format string `json:"format,omitempty"`
}

// PropSetSpec is prop.set.
type PropSetSpec struct {
	At    string `json:"at"`
	Value any    `json:"value"`
}

// PageAddSpec is page.add: appended, or before/after At.
type PageAddSpec struct {
	Name        string    `json:"name"`
	As          string    `json:"as,omitempty"`
	At          string    `json:"at,omitempty"`
	Position    string    `json:"position,omitempty"`
	Size        []float64 `json:"size,omitempty"`
	Orientation string    `json:"orientation,omitempty"`
}

// PageRenameSpec is page.rename.
type PageRenameSpec struct {
	At   string `json:"at"`
	Name string `json:"name"`
}

// PageSetSpec is page.set: orientation and/or size (decision 6).
type PageSetSpec struct {
	At          string    `json:"at"`
	Size        []float64 `json:"size,omitempty"`
	Orientation string    `json:"orientation,omitempty"`
}
