// Package view is the docx HTML projection: the document read surface of
// docxdriver 0.5.2 (html/render.rs, surface.rs, ctx.rs, styles.rs,
// chrome.rs, compact.rs and assets.rs), rendered as a view over the read
// model (internal/docx/model). The output is byte for byte docxdriver's:
// one block element per line, fixed attribute order and entity choices,
// data-* identities.
//
// The final and original views render what accept-all and reject-all
// leave: a private copy of the main document and its headers and footers
// is settled (internal/docx/settle) and rendered under the source's
// paragraph addresses.
package view

import (
	"errors"
	"slices"

	"github.com/jhnoel/documentdriver/internal/docx/math"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// View is which projection of tracked changes to render.
type View int

// The views.
const (
	// Markup interleaves both projections as revision markup: the editing
	// surface.
	Markup View = iota
	// Final shows every change accepted, without revision tags.
	Final
	// Original shows every change rejected, without revision tags.
	Original
)

// Named returns the view a --view value names ("" is Markup).
func Named(name string) (View, error) {
	switch name {
	case "", "markup":
		return Markup, nil
	case "final":
		return Final, nil
	case "original":
		return Original, nil
	}
	return Markup, errors.New("unknown view: " + name + " (expected markup, final, or original)")
}

// model returns the read model's view for list counting.
func (v View) model() model.View {
	switch v {
	case Final:
		return model.Current
	case Original:
		return model.Baseline
	}
	return model.All
}

// Options are what the view takes from outside the package.
type Options struct {
	// Equation converts an Office Math element (m:oMath or m:oMathPara) to
	// LaTeX, the projection's one equation format. Rendering a document with
	// equations without it fails with ErrNoEquation.
	Equation EquationFunc
}

// EquationFunc converts an Office Math element (m:oMath or m:oMathPara) to
// LaTeX without delimiters, says whether it is display math, and lists
// what the LaTeX does not carry. math.OMMLToLaTeX is the converter; tests
// pass stubs.
type EquationFunc func(eq *tree.Node) (latex string, display bool, losses []math.Loss)

// ContentLosses returns the element names of the content losses (never
// the formatting ones) in losses, sorted and without repeats: what an
// equation shows or means that its LaTeX leaves out. The projection lists
// them in data-loss and paragraph entries in equations[].loss, so the
// LaTeX never drops content silently. Formatting losses (fonts, sizes,
// layout hints) are not listed: nearly every Word equation has them, and
// an equation an edit leaves unchanged keeps its OMML, formatting and all.
func ContentLosses(losses []math.Loss) []string {
	var out []string
	for _, l := range losses {
		if l.Kind == math.LossContent && !slices.Contains(out, l.What) {
			out = append(out, l.What)
		}
	}
	slices.Sort(out)
	return out
}

// ErrNoEquation is returned when a rendered part has equations and Options
// has no equation converter.
var ErrNoEquation = errors.New("view: the document has equations and no OMML to LaTeX converter is configured")

// RegionKind is what a region of the rendered document holds.
type RegionKind int

// The region kinds.
const (
	RegionBody RegionKind = iota
	RegionHeader
	RegionFooter
	RegionSection
)

// Region is a span of the rendered HTML (byte offsets) and the paragraphs
// it renders: main-document paragraphs of the source for a body block,
// paragraphs of the header or footer part for chrome.
type Region struct {
	Kind       RegionKind
	Start, End int
	Part       string // "" for a section marker
	Section    int
	Paragraphs []*tree.Node
}

// Block is one top-level body block (a paragraph, a joined paragraph
// group, a table or a list) in the body-only HTML.
type Block struct {
	Start, End int
	Paragraphs []*tree.Node
}

// Rendered is the projection of a whole document.
type Rendered struct {
	// HTML is the full surface: sections, headers and footers, body blocks
	// and note bodies.
	HTML    string
	Regions []Region
	// Body is the body blocks alone, joined by newlines, and Blocks their
	// spans in it.
	Body   string
	Blocks []Block
	// StyleIDs are the style ids the markup's paragraphs name, and
	// Sources the src values of its images as written, for a render in
	// agent mode (RenderAgent); nil otherwise.
	StyleIDs, Sources map[string]bool
}

// Render renders the document in view v.
func Render(d *model.Document, v View, opt Options) (*Rendered, error) {
	return renderView(d, v, opt, false)
}

// renderView renders the document in view v, in agent mode when agent is set
// (RenderAgent).
func renderView(d *model.Document, v View, opt Options, agent bool) (*Rendered, error) {
	if v != Markup {
		out, ok, err := renderSettled(d, v, opt, agent)
		if err != nil {
			return nil, err
		}
		if ok {
			return out, nil
		}
	}
	r := newRenderer(d, d.XML, v, opt, nil, agent)
	ids := d.ParaIDs().IDs
	out := r.renderParts(ids, nil)
	if r.err != nil {
		return nil, r.err
	}
	return out, nil
}

// renderer holds what every part of one render shares.
type renderer struct {
	doc       *model.Document
	pkg       *model.Package
	main      *model.XML
	view      View
	opt       Options
	styles    *Styles
	numbering *model.Numbering
	noteScope string
	// overlay replaces parts of the package by settled copies.
	overlay map[string]*model.XML
	parsed  map[string]*model.XML
	// story are the addresses of the other stories' paragraphs
	// (storyParaIDs), set when the parts render.
	story map[*tree.Node]model.ParaID
	err   error
	// agent is set for a render of the agent form (RenderAgent).
	agent        bool
	decodedAttrs map[string]decodedAttr
	// docSink collects what the document's agent markup holds: the main
	// part's, and the headers, footers and notes it includes.
	docSink *sink
}

// newRenderer returns a renderer for d with main as its main document part.
func newRenderer(d *model.Document, main *model.XML, v View, opt Options, overlay map[string]*model.XML, agent bool) *renderer {
	return &renderer{
		doc:       d,
		pkg:       d.Pkg,
		main:      main,
		view:      v,
		opt:       opt,
		styles:    LoadStyles(d.Pkg),
		numbering: d.Numbering(),
		noteScope: noteScope(d.Source),
		overlay:   overlay,
		parsed:    map[string]*model.XML{},
		agent:     agent,

		decodedAttrs: map[string]decodedAttr{},
	}
}

// part returns the parsed XML of a package part (a settled copy when the
// render has one), or false when it is absent or not well formed.
func (r *renderer) part(name string) (*model.XML, bool) {
	if x, ok := r.overlay[name]; ok {
		return x, true
	}
	if x, ok := r.parsed[name]; ok {
		return x, x != nil
	}
	x, ok := r.pkg.Part(name)
	if !ok {
		r.parsed[name] = nil
		return nil, false
	}
	r.parsed[name] = x
	return x, true
}

// latex converts an equation to LaTeX and names its content losses
// (ContentLosses), recording ErrNoEquation when there is no converter.
func (r *renderer) latex(eq *tree.Node) (string, []string) {
	if r.opt.Equation == nil {
		if r.err == nil {
			r.err = ErrNoEquation
		}
		return "", nil
	}
	s, _, losses := r.opt.Equation(eq)
	return s, ContentLosses(losses)
}
