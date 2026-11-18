package ops

// Address fields are strings in the forms of vsd.Parse; the rules check
// their kind. Lengths are in the plan's units.

// ObjectAddSpec is object.add: a shape from a master (Master) or a
// master-less shape (Geom "rect" or "ellipse"), or a master-less
// connector (Kind "connector" with From/To or glue), at a page or
// group, with optional text and name.
type ObjectAddSpec struct {
	At       string     `json:"at"`
	As       string     `json:"as,omitempty" plan:"optional"`
	Kind     string     `json:"kind,omitempty" plan:"optional"`
	Master   string     `json:"master,omitempty" plan:"optional"`
	Geom     string     `json:"geom,omitempty" plan:"optional"`
	Box      []float64  `json:"box,omitempty" plan:"optional"`
	Pin      []float64  `json:"pin,omitempty" plan:"optional"`
	Name     string     `json:"name,omitempty" plan:"optional"`
	HTML     *string    `json:"html,omitempty" plan:"optional"`
	Fill     *PaintSpec `json:"fill,omitempty" plan:"optional"`
	Line     *LineSpec  `json:"line,omitempty" plan:"optional"`
	From     []float64  `json:"from,omitempty" plan:"optional"`
	To       []float64  `json:"to,omitempty" plan:"optional"`
	FromGlue string     `json:"from_glue,omitempty" plan:"optional"`
	ToGlue   string     `json:"to_glue,omitempty" plan:"optional"`
}

// ObjectSetSpec is object.set (the fields create and P2 share first).
type ObjectSetSpec struct {
	At       string     `json:"at"`
	Box      []float64  `json:"box,omitempty" plan:"optional"`
	Rot      *float64   `json:"rot,omitempty" plan:"optional"`
	FlipH    *bool      `json:"flip_h,omitempty" plan:"optional"`
	FlipV    *bool      `json:"flip_v,omitempty" plan:"optional"`
	Name     *string    `json:"name,omitempty" plan:"optional"`
	Fill     *PaintSpec `json:"fill,omitempty" plan:"optional"`
	Line     *LineSpec  `json:"line,omitempty" plan:"optional"`
	From     []float64  `json:"from,omitempty" plan:"optional"`
	To       []float64  `json:"to,omitempty" plan:"optional"`
	FromGlue *string    `json:"from_glue,omitempty" plan:"optional"`
	ToGlue   *string    `json:"to_glue,omitempty" plan:"optional"`
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

// TextSetSpec is text.set: replace a shape's tried text with HTML.
// Delete lists field addresses of the shape's shown text that the
// HTML does not send back; a listed field is removed from the Text
// and its Field row deleted (contract section 7, chunk CC11).
type TextSetSpec struct {
	At     string   `json:"at"`
	HTML   string   `json:"html"`
	Delete []string `json:"delete,omitempty" plan:"optional"`
}

// PropAddSpec is prop.add.
type PropAddSpec struct {
	At     string `json:"at"`
	Name   string `json:"name"`
	Label  string `json:"label,omitempty" plan:"optional"`
	Type   string `json:"type"`
	Value  any    `json:"value,omitempty" plan:"optional"`
	Format string `json:"format,omitempty" plan:"optional"`
}

// PropSetSpec is prop.set.
type PropSetSpec struct {
	At    string `json:"at"`
	Value any    `json:"value"`
}

// PageAddSpec is page.add: appended, or before/after At.
type PageAddSpec struct {
	Name        string    `json:"name"`
	As          string    `json:"as,omitempty" plan:"optional"`
	At          string    `json:"at,omitempty" plan:"optional"`
	Position    string    `json:"position,omitempty" plan:"optional"`
	Size        []float64 `json:"size,omitempty" plan:"optional"`
	Orientation string    `json:"orientation,omitempty" plan:"optional"`
}

// PageRenameSpec is page.rename.
type PageRenameSpec struct {
	At   string `json:"at"`
	Name string `json:"name"`
}

// PageSetSpec is page.set: orientation and/or size (decision 6).
type PageSetSpec struct {
	At          string    `json:"at"`
	Size        []float64 `json:"size,omitempty" plan:"optional"`
	Orientation string    `json:"orientation,omitempty" plan:"optional"`
}
