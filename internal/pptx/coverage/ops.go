package coverage

// OpStatus is the status of one operation on one object kind.
type OpStatus string

// Operation statuses.
const (
	Implemented     OpStatus = "implemented"
	RefusedByDesign OpStatus = "refused-by-design" // the plan fails with a reason the design gives
	NotYet          OpStatus = "not-yet"           // the plan fails with ops.ErrNotImplemented
	OpNonGoal       OpStatus = "non-goal"          // the design excludes it
)

// Kinds are the matrix's columns: the design's object kinds, and slide
// for the slide operations.
var Kinds = []string{"shape", "connector", "group", "picture", "other", "slide"}

// OpRow is one row of the operation matrix: an operation of the design's
// table, or one field of object.set, with its status per kind ("" where
// the operation does not apply to the kind).
type OpRow struct {
	Op    string
	Field string
	Cells map[string]OpStatus
	Notes map[string]string
}

// otherReadOnly notes the cells refused because graphic frames and
// content parts are read-only (design: Objects).
var otherReadOnly = map[string]string{"other": "graphic frames and ink are read-only (id, kind, bounds, raw XML); refused when the operation resolves its target"}

// Operations is the operation matrix, checked by running each cell's plan
// on the probe deck (TestOperationsMatchBehaviour): implemented cells
// must succeed, not-yet cells fail with ops.ErrNotImplemented, refused
// and non-goal cells fail with another error.
var Operations = []OpRow{
	{Op: "object.add", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"group": "groups come from group.create", "picture": "from a PNG, JPEG or GIF file, or an SVG with its raster fallback; identical media bytes share one part", "other": "adding charts, SmartArt or OLE objects is a non-goal"}},
	{Op: "object.add", Field: "path", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign},
		Notes: map[string]string{"shape": "a freeform: box plus path; open paths with fill none and arrows on the line", "connector": "a connector is drawn by its preset"}},
	{Op: "object.add", Field: "at (a group)", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "picture": Implemented},
		Notes: map[string]string{"shape": "into a group (its front); the box is in slide space; the frame is not refitted (group-frame hazard)"}},
	{Op: "object.set", Field: "name", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: otherReadOnly},
	{Op: "object.set", Field: "box, rot, flip_h, flip_v", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: otherReadOnly},
	{Op: "object.set", Field: "geom, adj", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"group": "a group has no geometry"}},
	{Op: "object.set", Field: "path", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"shape": "a preset becomes a custom geometry; geom with a preset switches back", "connector": "a connector is drawn by its preset; an open path is a shape", "group": "a group has no geometry", "picture": "the picture is clipped to the path"}},
	{Op: "object.set", Field: "custom (cxn, rect)", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"shape": "sites and the text rectangle; adjust values, guides and handles are formulas, writable back as shown", "connector": "a connector is drawn by its preset", "group": "a group has no geometry"}},
	{Op: "object.set", Field: "fill", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": Implemented, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"connector": "a connector's fill is never drawn: only none (fill none row)", "picture": "a picture's fill is its image"}},
	{Op: "object.set", Field: "fill none", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"connector": "a:noFill, as PowerPoint and pdraw write it on connectors", "picture": "a picture's fill is its image"}},
	{Op: "object.set", Field: "line", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"group": "a group has no outline"}},
	{Op: "object.set", Field: "from_glue, to_glue", Cells: map[string]OpStatus{"shape": RefusedByDesign, "connector": Implemented, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"shape": "only connectors are glued"}},
	{Op: "object.set", Field: "from, to", Cells: map[string]OpStatus{"shape": RefusedByDesign, "connector": Implemented, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"shape": "endpoints need a connector preset"}},
	{Op: "object.set", Field: "html", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"connector": "only shapes hold text"}},
	{Op: "object.set", Field: "text (anchor, wrap, insets)", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"shape": "on a shape with a text body; object.add takes it with html", "connector": "only shapes hold text"}},
	{Op: "object.set", Field: "text (autofit, vert, columns, flags)", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"shape": "on a shape with a text body; a new normAutofit holds only the scales given", "connector": "only shapes hold text"}},
	{Op: "object.set", Field: "descr, title, hidden", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: otherReadOnly},
	{Op: "object.set", Field: "tx_box", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": RefusedByDesign, "other": RefusedByDesign},
		Notes: map[string]string{"connector": "only shapes are text boxes", "other": "graphic frames and ink are read-only"}},
	{Op: "object.set", Field: "image, svg", Cells: map[string]OpStatus{"shape": RefusedByDesign, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"shape": "picture fills of shapes are shown by type and replaced only by none or solid", "picture": "a new or shared media part; the old one is removed only when nothing refers to it"}},
	{Op: "object.set", Field: "crop, fit, fill_rect", Cells: map[string]OpStatus{"shape": RefusedByDesign, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"shape": "only pictures have a blip fill of their own", "picture": "fit becomes stretch; a tile is writable back unchanged only"}},
	{Op: "object.set", Field: "locks", Cells: map[string]OpStatus{"shape": RefusedByDesign, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": Implemented, "other": OpNonGoal},
		Notes: map[string]string{"shape": "a:picLocks belongs to pictures"}},
	{Op: "object.move", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: otherReadOnly},
	{Op: "object.delete", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: map[string]string{"other": otherReadOnly["other"], "picture": "its media is removed when nothing else refers to it (a group's pictures too)"}},
	{Op: "object.duplicate", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: otherReadOnly},
	{Op: "object.reorder", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": RefusedByDesign}, Notes: otherReadOnly},
	{Op: "group.create", Cells: map[string]OpStatus{"shape": Implemented, "connector": Implemented, "group": Implemented, "picture": Implemented, "other": Implemented},
		Notes: map[string]string{"shape": "objects of one container; the frame encloses them with chOff/chExt equal to off/ext, so no child changes", "other": "a read-only object moves into the group; its own XML is not written"}},
	{Op: "group.ungroup", Cells: map[string]OpStatus{"group": Implemented},
		Notes: map[string]string{"group": "children baked into the container's space (Place, Unplace); refused when a child cannot be expressed, the group is hidden or has effects, or a child takes the group's fill"}},
	{Op: "group.fit", Cells: map[string]OpStatus{"group": Implemented},
		Notes: map[string]string{"group": "the frame recomputed from the children, which do not move (within 2 EMU)"}},
	{Op: "text.set", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal},
		Notes: map[string]string{"connector": "only shapes hold text"}},
	{Op: "text.replace", Cells: map[string]OpStatus{"shape": Implemented, "connector": RefusedByDesign, "group": RefusedByDesign, "picture": RefusedByDesign, "other": OpNonGoal}},
	{Op: "xml.set", Cells: map[string]OpStatus{"shape": NotYet, "connector": NotYet, "group": NotYet, "picture": NotYet, "other": NotYet}},
	{Op: "slide.add", Cells: map[string]OpStatus{"slide": NotYet}, Notes: map[string]string{"slide": "M5"}},
	{Op: "slide.delete", Cells: map[string]OpStatus{"slide": NotYet}, Notes: map[string]string{"slide": "M5"}},
	{Op: "slide.duplicate", Cells: map[string]OpStatus{"slide": NotYet}, Notes: map[string]string{"slide": "M5"}},
}
