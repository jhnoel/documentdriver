package model

import (
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/pptx/derive"
)

// EMUPerPoint is the number of EMU in one point.
const EMUPerPoint = derive.EMUPerPoint

// Kind is an object kind.
type Kind string

// Object kinds.
const (
	Shape     Kind = "shape"
	Connector Kind = "connector"
	Group     Kind = "group"
	Picture   Kind = "picture"
	Other     Kind = "other"
)

// Origin labels where an inherited value came from.
type Origin = derive.Origin

// Origins.
const (
	FromSlide    = derive.FromSlide
	FromLayout   = derive.FromLayout
	FromMaster   = derive.FromMaster
	FromTheme    = derive.FromTheme
	FromStyle    = derive.FromStyle    // the object's p:style, through the theme's format scheme
	FromPres     = derive.FromPres     // presentation defaultTextStyle
	FromGroup    = derive.FromGroup    // a:grpFill: the enclosing group's fill
	FromSpecDflt = derive.FromSpecDflt // the schema default of an attribute (ECMA-376)
)

// Omission records a property that is not shown and why.
type Omission struct {
	Prop   string
	Reason string
}

// Slide is the read model of one slide.
type Slide struct {
	// Part is the slide's part name, e.g. /ppt/slides/slide3.xml.
	Part string
	// Name is the short form used in addresses, e.g. slide3.
	Name string
	// Size is the slide size in EMU (p:sldSz).
	W, H float64
	// Colors are the resolved scheme colours by name, after the slide's
	// colour map: dk1, lt1, dk2, lt2, accent1-6, hlink, folHlink, and the
	// mapped names bg1, tx1, bg2, tx2.
	Colors map[string]string
	// Fonts are the theme fonts by reference: +mj-lt, +mn-lt, +mj-ea, ...
	Fonts map[string]string
	// Objects are every object in document order (paint order), groups
	// before their children.
	Objects []*Object
	// Hazards are the contradictions found on read.
	Hazards []Hazard
	// Omitted lists slide-level omissions.
	Omitted []Omission
}

// Hazard is a place where the file contradicts itself.
type Hazard struct {
	Kind   string // stale-connector, dangling-glue, group-frame
	ID     string // the object it concerns
	Detail string
}

// Object is one DrawingML object on a slide.
type Object struct {
	// ID is the short id within the slide: s17, or s17~2 for the second
	// object with id 17 in document order.
	ID string
	// RawID is the cNvPr id attribute as written.
	RawID string
	Name  string
	Kind  Kind
	// Type refines Other: table, chart, smartart, ole, ink, media,
	// graphicFrame, contentPart, unknown.
	Type string
	// Z is the paint order on the slide, 0 first.
	Z int
	// Parent is the id of the enclosing group, "" at the top level.
	Parent string
	// Hidden is cNvPr hidden="1".
	Hidden bool
	// Descr and Title are cNvPr's alternative text (descr) and title, ""
	// when absent.
	Descr, Title string
	// TxBox is a shape's cNvSpPr txBox="1": PowerPoint's text box, whose
	// text the user types without first creating a shape.
	TxBox bool
	// Ph is the placeholder reference, if any.
	Ph *Placeholder

	// Stored is the object's own transform in its parent's space, in EMU,
	// with its origin (FromSlide unless inherited). Nil if none applies.
	Stored *derive.Xfrm
	// Group is a group's stored transform including its child space.
	Group *derive.GroupXfrm
	// Box is the slide-space transform. Nil when it cannot be derived.
	Box *derive.Xfrm
	// M maps local coordinates (0..w, 0..h of Stored's extent) to slide
	// space. Valid when Box is not nil.
	M derive.Affine

	Geom   *Geometry
	Fill   *Fill
	Line   *Line
	Text   *Text
	Conn   *Conn
	Sites  []derive.Site
	Image  string // picture: target part of the blip
	Pic    *PicProps
	Origin map[string]Origin
	// Props are additional modelled values for --detail full, such as the
	// resolved text defaults per level.
	Props   []Prop
	Omitted []Omission

	// node is the object's element in the slide part (in the projected
	// mc:AlternateContent branch); source is the element whose bytes
	// `pptx xml` prints: node, or the enclosing mc:AlternateContent when
	// the object sits in one. Neither is exported: the read model never
	// hands out mutable XML.
	node, source *tree.Node
}

// XML returns the exact current bytes of the object's source: its element,
// or the whole enclosing mc:AlternateContent when it sits in one. The
// slice is a copy.
func (o *Object) XML() []byte {
	if o.source == nil {
		return nil
	}
	return o.source.Bytes()
}

// ElementXML returns the exact current bytes of the object's element in
// the projected mc:AlternateContent branch. The slice is a copy.
func (o *Object) ElementXML() []byte {
	if o.node == nil {
		return nil
	}
	return o.node.Bytes()
}

// Placeholder is a p:ph reference.
type Placeholder struct {
	Type string // as written; "" means the schema default (obj)
	Idx  string // as written; "" if absent
}

// Prop is one extra modelled value with its origin.
type Prop struct {
	Key    string
	Value  string
	Origin Origin
}

// Geometry is a preset with adjust values, or a custom path.
type Geometry struct {
	Preset string // "" for custGeom
	Adj    []derive.Adjust
	// Path is the custom geometry as an SVG path subset (M L C Q Z) in EMU
	// relative to the slide-space box's top-left corner, before rotation
	// and flips (the shape's local coordinates scaled by any enclosing
	// groups). "" if not expressible.
	Path string
	// Paths are the custom geometry's a:path elements one by one, with
	// their fill and stroke modes, in the same units as Path; nil when
	// Path is not expressible.
	Paths []derive.ShownPath
	// Parts are the custom geometry's other lists (adjust values,
	// guides, handles, sites, text rectangle) as --detail full shows
	// them; nil when they cannot be shown.
	Parts  *derive.ShownCustom
	Custom bool
}

// Color is a colour as written plus its approximate displayed value.
type Color = derive.ShownColor

// Fill is a shape fill.
type Fill = derive.Fill

// Line is a shape outline. Nil fields were not derived.
type Line = derive.Line

// Arrow is a line end.
type Arrow = derive.Arrow

// Text is a text body.
type Text struct {
	HTML string
	// Insets are lIns, tIns, rIns, bIns in EMU; nil where not derived.
	Insets [4]*float64
	Anchor string
	Wrap   string
	// Attrs are the stored values of every a:bodyPr attribute of
	// derive.BodyAttrs (the insets, anchor and wrap included), "" where
	// not derived, and Autofit the autofit choice; the views show them
	// through show.BodyJSON, as the body codec does.
	Attrs   map[string]string
	Autofit derive.Autofit
	// Levels lists the paragraph levels used (1-based), for which Props
	// carry resolved defaults.
	Levels []int
}

// PicProps is what a p:pic shows beyond its image part (Object.Image):
// the SVG it carries, the facts of both media parts, its crop, fill mode
// and locks. Fields are nil or empty where the file does not say, with
// the reason in the object's omissions where it is unreadable.
type PicProps struct {
	// SVG is the target part of the blip's SVG extension (asvg:svgBlip),
	// whose raster fallback is Object.Image.
	SVG string
	// Media and SVGMedia are the facts of the image and SVG parts.
	Media, SVGMedia *MediaFacts
	// Fill is the blip fill: crop, fit and fill rectangle; nil when it
	// is not read.
	Fill *derive.PicFill
	// Locks are the a:picLocks flags that are set, sorted by name.
	Locks []string
}

// MediaFacts are what the package says of a media part: its content type
// and what its bytes give.
type MediaFacts struct {
	Type string
	derive.Media
}

// Conn is a connector's derived geometry and glue.
type Conn struct {
	From, To derive.Pt
	First    string
	Elbows   []derive.Pt
	Curve    []derive.Pt
	Start    *Glue
	End      *Glue
}

// Glue is one glued end of a connector.
type Glue struct {
	RawID string // stCxn/endCxn id as written
	// ID is the resolved short id: "" if no object has the id (dangling)
	// or more than one does (see Candidates).
	ID   string
	Site int
	// Candidates lists the short ids of every object with the glue id when
	// there is more than one. The glue is then ambiguous and resolved to
	// none of them: PowerPoint's choice is not known.
	Candidates []string
}
