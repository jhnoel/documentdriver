package ops

// Address fields are strings in the forms of vsd.Parse; the rules check
// their kind. Lengths are in the plan's units.

// HelpInfo is one op's help data beyond its summary and fields: Rules
// are its document-independent checks, each the message its check
// method returns when the check is broken, and Refuses are its
// document-dependent refusals from the closed list of
// docs/vsdx-contract-draft.md section 3, one per line. Each spec file
// holds its op's notes (the doc tags), rules and refuses as data and
// registers them with registerHelp, so help <op> is generated from
// the op's own file (chunk CC7).
type HelpInfo struct {
	Rules   []string
	Refuses []string
}

// helpByOp is the registered help data by op name, filled by the init
// functions of the spec files and read only after init.
var helpByOp = map[string]HelpInfo{}

// registerHelp registers rules and refuses as the help data of name.
func registerHelp(name string, rules, refuses []string) {
	helpByOp[name] = HelpInfo{Rules: rules, Refuses: refuses}
}

// HelpFor returns the help data of the op named name and whether one
// is registered.
func HelpFor(name string) (HelpInfo, bool) {
	info, ok := helpByOp[name]
	return info, ok
}

// htmlRule is the rules line for the html check (checkHTML): its
// messages come from the text grammar, so one line names the grammar
// instead of enumerating its errors.
const htmlRule = "html must be in the text grammar (help html)"

// ObjectAddSpec is object.add: a shape from a master (Master) or a
// master-less shape (Geom "rect" or "ellipse"), or a master-less
// connector (Kind "connector" with From/To or glue), at a page or
// group, with optional text and name.
type ObjectAddSpec struct {
	At       string      `json:"at" doc:"A page or group address as read shows it (page0 or page0/s1), or $name."`
	As       string      `json:"as,omitempty" plan:"optional" doc:"A name for the new shape; later operations write $name for it."`
	Kind     string      `json:"kind,omitempty" plan:"optional" doc:"connector for a master-less straight connector, picture for a picture; absent for a shape."`
	Master   string      `json:"master,omitempty" plan:"optional" doc:"A master address (master2) or the master's name as read shows it."`
	Geom     string      `json:"geom,omitempty" plan:"optional" doc:"rect or ellipse (placed by box or pin), or polyline or curve (placed by points), for a master-less shape."`
	Points   [][]float64 `json:"points,omitempty" plan:"optional" doc:"A polyline's or curve's points [[x, y], ...] in view points (or the plan's units), y down, as read shows points; the shape's box is their bounds. A curve passes through every point (a Catmull-Rom spline stored as RelCubBezTo rows)."`
	Closed   bool        `json:"closed,omitempty" plan:"optional" doc:"true: the polyline or curve returns to its first point and may be filled; absent or false: an open path, which takes line begin and end arrowheads, as read shows closed."`
	Box      []float64   `json:"box,omitempty" plan:"optional" doc:"[x, y, width, height] in view points, y down, as read shows box; for a 2-D master or geom shape."`
	Pin      []float64   `json:"pin,omitempty" plan:"optional" doc:"[x, y], the centre in view points (input only: read shows box); for a 2-D master or geom shape. Without box, a geom shape is 72 x 72 pt and a master instance takes the master's size."`
	Name     string      `json:"name,omitempty" plan:"optional" doc:"The shape's name, as read shows name."`
	HTML     *string     `json:"html,omitempty" plan:"optional" doc:"The shape's text as HTML (help html), as read shows html."`
	Fill     *PaintSpec  `json:"fill,omitempty" plan:"optional" doc:"\"none\", {hex: #RRGGBB} for a solid fill, or a pattern fill {pattern, hex, bg} as read shows fill: pattern a name (hatch_up, hatch_down, grid, cross_hatch, horizontal, vertical, dots, hatch_up_fine, hatch_down_fine, grid_fine, cross_hatch_fine, horizontal_fine, vertical_fine) or a FillPattern number 2 to 24, hex the pattern colour (default #000000), bg the background #RRGGBB, {hex} or \"none\" (default \"none\": clear)."`
	Line     *LineSpec   `json:"line,omitempty" plan:"optional" doc:"\"none\" or {w, color, dash, begin, end}, as read shows line; color is {hex: #RRGGBB} as read shows it, or the bare string #RRGGBB; dash is the LinePattern number as a number or string; begin and end (connectors, open polylines and open curves) are the arrowheads at from and to: \"none\", a name (triangle, stealth) or {arrow, size}, size 0 to 6."`
	From     []float64   `json:"from,omitempty" plan:"optional" doc:"[x, y] begin end in view points, as read shows from; for a connector or 1-D master."`
	To       []float64   `json:"to,omitempty" plan:"optional" doc:"[x, y] end end in view points, as read shows to; for a connector or 1-D master."`
	FromGlue string      `json:"from_glue,omitempty" plan:"optional" doc:"s12:3, s12 for the whole shape, or $name with an optional point, as read shows from_glue."`
	ToGlue   string      `json:"to_glue,omitempty" plan:"optional" doc:"s12:3, s12 for the whole shape, or $name with an optional point, as read shows to_glue."`
	Shadow   *ShadowSpec `json:"shadow,omitempty" plan:"optional" doc:"\"none\" or {color, transparency, offset} (shapes and groups): color #RRGGBB or {hex}, transparency 0 to 100 percent, offset [dx, dy] in points (or the plan's units), y down. On an object with no shadow all three are needed."`
	Image    string      `json:"image,omitempty" plan:"optional" doc:"A picture's image: a local PNG or JPEG file path (absolute, or relative to the working directory), or a media part as read shows image (/visio/media/image1.png)."`
	Crop     []float64   `json:"crop,omitempty" plan:"optional" doc:"A picture's crop [l, t, r, b] in percent of the image, as read shows crop; each 0 to under 100, l + r and t + b under 100."`
	img      *imageInput // the checked image (CheckImages)
}

// ObjectSetSpec is object.set (the fields create and P2 share first).
type ObjectSetSpec struct {
	At       string      `json:"at" doc:"A shape address as read shows it (page0/s1), or $name."`
	Box      []float64   `json:"box,omitempty" plan:"optional" doc:"[x, y, width, height] in view points, y down, as read shows box."`
	Rot      *float64    `json:"rot,omitempty" plan:"optional" doc:"Rotation in degrees, as read shows rot."`
	FlipH    *bool       `json:"flip_h,omitempty" plan:"optional" doc:"Flip horizontally, as read shows flip_h."`
	FlipV    *bool       `json:"flip_v,omitempty" plan:"optional" doc:"Flip vertically, as read shows flip_v."`
	Name     *string     `json:"name,omitempty" plan:"optional" doc:"The shape's name, as read shows name."`
	Fill     *PaintSpec  `json:"fill,omitempty" plan:"optional" doc:"\"none\", {hex: #RRGGBB} for a solid fill, or a pattern fill {pattern, hex, bg} as read shows fill: pattern a name (hatch_up, hatch_down, grid, cross_hatch, horizontal, vertical, dots, hatch_up_fine, hatch_down_fine, grid_fine, cross_hatch_fine, horizontal_fine, vertical_fine) or a FillPattern number 2 to 24, hex the pattern colour (default #000000), bg the background #RRGGBB, {hex} or \"none\" (default \"none\": clear)."`
	Line     *LineSpec   `json:"line,omitempty" plan:"optional" doc:"\"none\" or {w, color, dash, begin, end}, as read shows line; color is {hex: #RRGGBB} as read shows it, or the bare string #RRGGBB; dash is the LinePattern number as a number or string; begin and end (connectors, open polylines and open curves) are the arrowheads at from and to: \"none\", a name (triangle, stealth) or {arrow, size}, size 0 to 6."`
	From     []float64   `json:"from,omitempty" plan:"optional" doc:"[x, y] begin end in view points, as read shows from; for a connector."`
	To       []float64   `json:"to,omitempty" plan:"optional" doc:"[x, y] end end in view points, as read shows to; for a connector."`
	FromGlue *string     `json:"from_glue,omitempty" plan:"optional" doc:"s12:3, s12 for the whole shape, $name with an optional point, or none, as read shows from_glue."`
	ToGlue   *string     `json:"to_glue,omitempty" plan:"optional" doc:"s12:3, s12 for the whole shape, $name with an optional point, or none, as read shows to_glue."`
	Shadow   *ShadowSpec `json:"shadow,omitempty" plan:"optional" doc:"\"none\" or {color, transparency, offset} (shapes and groups): color #RRGGBB or {hex}, transparency 0 to 100 percent, offset [dx, dy] in points (or the plan's units), y down. On an object with no shadow all three are needed."`
	Image    *string     `json:"image,omitempty" plan:"optional" doc:"A picture's image: a local PNG or JPEG file path (absolute, or relative to the working directory), or a media part as read shows image (/visio/media/image1.png)."`
	Crop     []float64   `json:"crop,omitempty" plan:"optional" doc:"A picture's crop [l, t, r, b] in percent of the image, as read shows crop; each 0 to under 100, l + r and t + b under 100."`
	img      *imageInput // the checked image (CheckImages)
}

// PaintSpec is a fill: "none", {"hex": "#RRGGBB"} (solid), or a
// pattern fill {"pattern", "hex", "bg"}, as read shows it. After
// decoding, Pattern is the stored FillPattern number ("" for solid),
// Hex the colour (a pattern's defaults to #000000), and a pattern's
// background is BgNone (the default) or BgHex.
type PaintSpec struct {
	None    bool
	Hex     string
	Pattern string
	BgNone  bool
	BgHex   string
}

// HelpType names PaintSpec's plan type in help.
func (PaintSpec) HelpType() string { return `object|"none"` }

// HelpType names LineSpec's plan type in help.
func (LineSpec) HelpType() string { return `object|"none"` }

// LineSpec is a line: "none" or {w, color, dash}.
type LineSpec struct {
	None  bool
	W     *float64
	Color string
	Dash  string
	// Begin and End are the arrowheads at a connector's from and to
	// ends (docs/contract.md §3, Line ends); nil leaves the end alone.
	Begin, End *LineEndSpec
}

// LineEndSpec is one arrowhead: "none", a name, or {arrow, size}.
// Arrow is the name or index string as given; Size nil keeps the size.
type LineEndSpec struct {
	Arrow string
	Size  *float64
}

// ShadowSpec is a shadow: "none" or {color, transparency, offset},
// each member nil (or "") when left out.
type ShadowSpec struct {
	None         bool
	Color        string
	Transparency *float64
	Offset       []float64
}

// HelpType names ShadowSpec's plan type in help.
func (ShadowSpec) HelpType() string { return `object|"none"` }

// TextSetSpec is text.set: replace a shape's tried text with HTML.
// Delete lists field addresses of the shape's shown text that the
// HTML does not send back; a listed field is removed from the Text
// and its Field row deleted (contract section 7, chunk CC11).
type TextSetSpec struct {
	At     string   `json:"at" doc:"A shape address as read shows it (page0/s1), or $name."`
	HTML   string   `json:"html" doc:"Replacement text as HTML (help html), as read shows html."`
	Delete []string `json:"delete,omitempty" plan:"optional" doc:"Field addresses ({field-address}) or $name of shown fields the HTML does not send back."`
}

// PropAddSpec is prop.add.
type PropAddSpec struct {
	At     string `json:"at" doc:"A shape address as read shows it (page0/s1), or $name."`
	Name   string `json:"name" doc:"The row name: ASCII letters, digits and _, not starting with a digit, as read shows it in props."`
	Label  string `json:"label,omitempty" plan:"optional" doc:"The row's label, as read shows label; absent means name."`
	Type   string `json:"type" enum:"string,number,boolean,date,duration,currency,fixed_list,variable_list" doc:"The row's type, as read shows type."`
	Value  any    `json:"value,omitempty" plan:"optional" doc:"In the form read shows for the type: string, number, boolean, date {date: YYYY-MM-DDThh:mm:ss}, duration text, or a list item's text."`
	Format string `json:"format,omitempty" plan:"optional" doc:"For fixed_list and variable_list the ;-separated choices, as read shows format."`
}

// PropSetSpec is prop.set.
type PropSetSpec struct {
	At    string `json:"at" doc:"A shape data row address ({prop-address}) or $name."`
	Value any    `json:"value" doc:"In the form read shows for the row's type: string, number, boolean, date {date: YYYY-MM-DDThh:mm:ss}, duration text, or a list item's text."`
}

// PageAddSpec is page.add: appended, or before/after At.
type PageAddSpec struct {
	Name           string    `json:"name" doc:"The new page's name, as read shows it."`
	As             string    `json:"as,omitempty" plan:"optional" doc:"A name for the new page; later operations write $name for it."`
	At             string    `json:"at,omitempty" plan:"optional" doc:"A page address to add next to, as read shows it, with position; with neither, a foreground page goes after the last foreground page and a background page at the end."`
	Position       string    `json:"position,omitempty" plan:"optional" enum:"before,after" doc:"Before or after at."`
	Size           []float64 `json:"size,omitempty" plan:"optional" doc:"[width, height] in points, or the plan's units, as read shows size."`
	Orientation    string    `json:"orientation,omitempty" plan:"optional" doc:"portrait or landscape, as read shows orientation."`
	Background     *bool     `json:"background,omitempty" plan:"optional" doc:"true: a background page; false: a foreground page, as read shows background."`
	BackgroundPage string    `json:"background_page,omitempty" plan:"optional" doc:"A background page's address for this page to use, as read shows background_page."`
}

// PageRenameSpec is page.rename.
type PageRenameSpec struct {
	At   string `json:"at" doc:"A page address as read shows it (page0), or $name."`
	Name string `json:"name" doc:"The page's new name, as read shows it."`
}

// PageSetSpec is page.set: orientation and/or size (decision 6).
type PageSetSpec struct {
	At             string    `json:"at" doc:"A page address as read shows it (page0), or $name."`
	Size           []float64 `json:"size,omitempty" plan:"optional" doc:"[width, height] in points, or the plan's units, as read shows size."`
	Orientation    string    `json:"orientation,omitempty" plan:"optional" doc:"portrait or landscape, as read shows orientation."`
	Background     *bool     `json:"background,omitempty" plan:"optional" doc:"true: a background page; false: a foreground page, as read shows background."`
	BackgroundPage string    `json:"background_page,omitempty" plan:"optional" doc:"A background page's address for this page to use, as read shows background_page, or \"none\"."`
}

// objectAddRules are object.add's document-independent checks, in the
// order checkObjectAdd and checkHTML return them; help prints them
// as rules:. Chunk CC7.
var objectAddRules = []string{
	"at must be a page or shape address or $name",
	"as must match [A-Za-z_][A-Za-z0-9_]*",
	"kind must be connector or picture",
	"image and crop go with kind picture",
	"picture takes no master, geom, fill, line, html, shadow, from, to, from_glue or to_glue",
	"picture needs image",
	"geom must be rect, ellipse, polyline or curve",
	pathAddRules[0], pathAddRules[1], pathAddRules[2], pathAddRules[3],
	pathAddRules[4], pathAddRules[5], pathAddRules[6],
	"master must be a master address or a master name",
	"object.add takes master or geom, not both",
	"connector takes no master",
	"connector takes no geom",
	"object.add needs master, geom or kind connector",
	"connector takes no box",
	"connector takes no pin",
	"shape takes no from",
	"shape takes no to",
	"shape takes no from_glue",
	"shape takes no to_glue",
	"shape needs box or pin",
	"shape takes box or pin, not both",
	"box must be four numbers with positive width and height",
	"pin must be two numbers",
	"from must be two numbers",
	"to must be two numbers",
	"connector needs from or from_glue",
	"connector needs to or to_glue",
	"from_glue must name a shape or $name, with an optional point",
	"to_glue must name a shape or $name, with an optional point",
	htmlRule,
}

// objectAddRefuses are object.add's document-dependent refusals, one
// per line. Chunk CC7.
var objectAddRefuses = []string{
	"masters and stencils: at names a master shape; masters are read only, and stencils cannot be edited",
	"containers/lists/callouts: at is a container, list or callout (User.msvStructureType)",
	"scaled-page geometry: geometry is not edited on a page with a drawing scale",
	"a 1-D master given box or pin, or a 2-D master given from, to, from_glue or to_glue",
	"a connector whose ends coincide",
	"whole-shape glue on a connector without the dynamic glue cells",
}

// objectSetRules are object.set's document-independent checks, in the
// order checkObjectSet returns them. Chunk CC7. CC10 removed the
// "connector takes from or from_glue, not both" check, so it is not
// listed here.
var objectSetRules = []string{
	"at must be a shape address or $name",
	"box must be four numbers with positive width and height",
	"from must be two numbers",
	"to must be two numbers",
	"from_glue must name a shape or $name, with an optional point, or none",
	"to_glue must name a shape or $name, with an optional point, or none",
	"object.set needs a field to set",
}

// objectSetRefuses are object.set's document-dependent refusals, one
// per line. Chunk CC7.
var objectSetRefuses = []string{
	"formula-bound cells: a cell bound to a formula vsdx does not evaluate",
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"a member of a container or list (Relationships DEPENDSON): its geometry is not edited in v1",
	"scaled-page geometry: geometry is not edited on a page with a drawing scale",
	"masters and stencils: a master shape (masters cannot be edited) or a shape in a stencil (stencils cannot be edited)",
	"both-ends-glued move: a connector glued at both ends cannot be moved (set from_glue or to_glue to none first)",
	"whole-shape glue on a connector without the dynamic glue cells",
	"from/to or glue on a 2-D shape, or box, rot or flips on a 1-D connector",
	"fill and line on a picture or other read-only kind (Foreign or guide shape)",
}

// textSetRules are text.set's document-independent checks, in the
// order checkTextSet and checkHTML return them. Chunk CC7.
var textSetRules = []string{
	"at must be a shape address or $name",
	"delete must list field addresses or $name",
	htmlRule,
}

// textSetRefuses are text.set's document-dependent refusals, one per
// line. Chunk CC7.
var textSetRefuses = []string{
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"masters and stencils: a master shape (masters cannot be edited) or a shape in a stencil (stencils cannot be edited)",
	"text on a picture or other read-only kind (Foreign or guide shape)",
}

// propAddRules are prop.add's document-independent checks, in the
// order checkPropAdd and checkPropRules return them, including the
// ASCII name rule of CC8. Chunk CC7.
var propAddRules = []string{
	"at must be a shape address or $name",
	"name must not be empty",
	"name must be ASCII letters, digits and _, not starting with a digit",
	"date value must be YYYY-MM-DDThh:mm:ss",
}

// propAddRefuses are prop.add's document-dependent refusals, one per
// line. Chunk CC7.
var propAddRefuses = []string{
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"masters and stencils: at must name a page shape, not a master shape, and stencils cannot be edited",
	"a duplicate shape data row (a row named name already exists on the shape)",
}

// propSetRules are prop.set's document-independent checks, in the
// order checkPropSet and checkPropRules return them. Chunk CC7.
var propSetRules = []string{
	"at must name a shape data row or $name",
	"date value must be YYYY-MM-DDThh:mm:ss",
}

// propSetRefuses are prop.set's document-dependent refusals, one per
// line. Chunk CC7.
var propSetRefuses = []string{
	"containers/lists/callouts: a container, list or callout (User.msvStructureType) is not edited in v1",
	"masters and stencils: at must name a page shape data row, not a master shape, and stencils cannot be edited",
	"formula-bound cells: a Prop Value cell bound to a formula vsdx does not evaluate",
	"a data-linked shape data row",
}

// pageAddRules are page.add's document-independent checks, in the
// order checkPageAdd and checkPageAddTogether return them. Chunk CC7.
var pageAddRules = []string{
	"name must not be empty",
	"as must match [A-Za-z_][A-Za-z0-9_]*",
	"at must be a page address or $name",
	"orientation must be portrait or landscape",
	"size must be two positive numbers",
	"at and position go together: the page to add next to, and before or after",
}

// pageAddRefuses are page.add's document-dependent refusals, one per
// line. Chunk CC7.
var pageAddRefuses = []string{
	"a duplicate page name (page name is already used)",
}

// pageRenameRules are page.rename's document-independent checks, in
// the order checkPageRename returns them. Chunk CC7.
var pageRenameRules = []string{
	"at must be a page address or $name",
	"name must not be empty",
}

// pageRenameRefuses are page.rename's document-dependent refusals,
// one per line. Chunk CC7.
var pageRenameRefuses = []string{
	"a duplicate page name (page name is already used)",
}

// pageSetRules are page.set's document-independent checks, in the
// order checkPageSet returns them. Chunk CC7.
var pageSetRules = []string{
	"at must be a page address or $name",
	"orientation must be portrait or landscape",
	"size must be two positive numbers",
	"page.set needs orientation, size, background or background_page",
}

// pageSetRefuses are page.set's document-dependent refusals, one per
// line. Chunk CC7.
var pageSetRefuses = []string{
	"scaled-page geometry: geometry is not edited on a page with a drawing scale (size)",
	"formula-bound cells: PageWidth, PageHeight or PrintPageOrientation bound to a formula vsdx does not evaluate",
}

// init registers the help data of the ops whose specs live in this
// file (chunk CC7).
func init() {
	registerHelp("object.add", objectAddRules, objectAddRefuses)
	registerHelp("object.set", objectSetRules, objectSetRefuses)
	registerHelp("text.set", textSetRules, textSetRefuses)
	registerHelp("prop.add", propAddRules, propAddRefuses)
	registerHelp("prop.set", propSetRules, propSetRefuses)
	registerHelp("page.add", pageAddRules, pageAddRefuses)
	registerHelp("page.rename", pageRenameRules, pageRenameRefuses)
	registerHelp("page.set", pageSetRules, pageSetRefuses)
}
