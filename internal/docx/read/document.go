package read

import (
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/docx/model"
	"github.com/jhnoel/documentdriver/internal/docx/view"
	"github.com/jhnoel/documentdriver/internal/ooxml/ctypes"
	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
)

// projectionVersion is the HTML projection's version docxdriver 0.5.2
// reports.
const projectionVersion = 4

// readDocument is the document read kind (api.rs typed_read "document"):
// the agent markup of the view, with the style catalog, tables, sections,
// equations, selections, assets and comment threads it refers to.
// viewName is the --view value ("" for none).
func readDocument(st *model.Document, viewName string, opt Options) (Object, error) {
	v, err := view.Named(viewName)
	if err != nil {
		return nil, cli.Wrap(cli.CodeUsage, "--view", err)
	}
	rendered, err := view.RenderAgent(st, v, view.Options{Equation: opt.Equation})
	if err != nil {
		return nil, mathErr(err)
	}
	ids := st.ParaIDs()
	value := inspectionHeader(ids.Repairs)
	value["kind"] = "document"
	value["view"] = viewOr(viewName)
	value["projection_version"] = projectionVersion
	value["content_type"] = "text/html"

	styles := view.LoadStyles(st.Pkg)
	catalog := styles.Catalog()
	def, hasDef := styles.DefaultParagraphStyle()
	markup := rendered.HTML
	used := rendered.StyleIDs
	if hasDef {
		used[def] = true
	}
	var other []any
	kept := Object{}
	for _, id := range slices.Sorted(maps.Keys(catalog)) {
		if used[id] {
			kept[id] = catalog[id]
		} else {
			other = append(other, id)
		}
	}
	value["tables"] = view.AgentTableInventory(st.XML)
	value["sections"] = view.PruneEmpty(sectionInventory(st, v == view.Original))
	value["styles"] = kept
	if hasDef {
		value["default_style"] = def
	}
	if len(other) > 0 {
		value["other_styles"] = other
	}
	if eqs := equationDescriptors(st); len(eqs) > 0 {
		value["equations"] = eqs
	}
	if sel := selections(st); len(sel) > 0 {
		value["selection_space"] = selectionSpace()
		value["selections"] = Object{"paragraphs": sel}
	}
	if assets := assetManifest(st.Pkg, false); len(assets) > 0 {
		aliases := view.AliasAssets(assets)
		for src := range rendered.Sources {
			if _, ok := aliases[src]; ok { // the markup names an asset
				markup = view.AliasSources(markup, aliases)
				break
			}
		}
		value["assets"] = assets
	}
	value["markup"] = markup
	threads, msg := commentThreads(st)
	switch {
	case msg != "":
		st.Pkg.Note(commentPartName(st.Pkg, ctypes.RelComments), msg)
	case len(threads) > 0:
		value["selection_space"] = selectionSpace()
		value["comments"] = threads
	}
	return value, nil
}

// mathErr maps the view's missing-converter error to the read side's.
func mathErr(err error) error {
	if errors.Is(err, view.ErrNoEquation) {
		return ErrNoEquation
	}
	return err
}

// viewOr returns view v, or markup when v is empty.
func viewOr(v string) string {
	if v == "" {
		return "markup"
	}
	return v
}

// inspectionHeader is the inspection envelope: how many paragraph ids
// are assigned, not yet stored in the file.
func inspectionHeader(repairs int) Object {
	return Object{"id_repairs_needed": repairs}
}

// selectionSpace returns the selection space descriptor object.
func selectionSpace() Object {
	return Object{"view": "final", "offset_unit": "unicode_scalar"}
}

// equationDescriptors lists every body paragraph's current equations by
// address and editability (commands/equations.rs descriptors).
func equationDescriptors(st *model.Document) []any {
	ids := st.ParaIDs()
	var out []any
	for _, p := range st.XML.Paragraphs() {
		var at any
		if id, ok := ids.IDs[p.Node]; ok {
			at = id.ID
		}
		for i, eq := range model.CurrentEquations(p.Node) {
			e := Object{
				"display":  model.IsLocal(eq, "oMathPara"),
				"editable": eq.Parent() == p.Node,
				"at":       at,
			}
			if id, ok := at.(string); ok {
				e["id"] = EquationAddress(id, i+1)
			}
			out = append(out, e)
		}
	}
	return out
}

// EquationAddress is the address of the n-th equation (from 1) of the
// paragraph with id para.
func EquationAddress(para string, n int) string { return para + "/equation:" + strconv.Itoa(n) }

// complexLocals are the elements that make a paragraph's HTML text differ
// from its selectable text.
var complexLocals = []string{
	"oMath", "oMathPara", "drawing", "pict", "tab", "ptab", "br", "footnoteReference",
	"endnoteReference", "fldChar", "fldSimple", "AlternateContent",
	// Leaves the text model counts that the HTML does not show as text.
	"cr", "sym", "softHyphen", "object", "contentPart",
	"pgNum", "dayShort", "dayLong", "monthShort", "monthLong", "yearShort", "yearLong",
}

// selections gives the exact selectable (final-view) text of every body
// paragraph whose HTML text is ambiguous: one with revisions, generated
// objects or boundaries (commands/reading.rs selections).
func selections(st *model.Document) []any {
	ids := st.ParaIDs()
	var out []any
	for _, p := range st.XML.Paragraphs() {
		ambiguous := false
		p.Node.Walk(func(n *tree.Node) bool {
			if ambiguous {
				return false
			}
			if n != p.Node && model.IsElement(n) && (model.IsRevIns(n) || model.IsRevDel(n) ||
				slices.ContainsFunc(complexLocals, func(l string) bool { return model.IsLocal(n, l) })) {
				ambiguous = true
			}
			return !ambiguous
		})
		if !ambiguous {
			continue
		}
		id, ok := ids.IDs[p.Node]
		if !ok {
			continue
		}
		out = append(out, Object{"at": id.ID, "selectable_text": currentText(model.BuildSegments(p.Node))})
	}
	return out
}

// currentText returns the segments' text excluding deleted segments.
func currentText(segs []model.Segment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Wrap != model.WrapDel {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}
