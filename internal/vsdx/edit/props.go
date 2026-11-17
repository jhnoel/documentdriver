package edit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhnoel/documentdriver/internal/ooxml/tree"
	"github.com/jhnoel/documentdriver/internal/ooxml/xmlscan"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// propTypeNames is the Property Type code table: the Type cell holds
// one of these codes and the Value cell renders by it. It matches
// internal/vsdx/model's table (string 0, fixed_list 1, number 2,
// boolean 3, variable_list 4, date 5, duration 6, currency 7).
var propTypeNames = []string{"string", "fixed_list", "number", "boolean", "variable_list", "date", "duration", "currency"}

// PropTypeCode returns the Type code for a type name ("string",
// "fixed_list", "number", "boolean", "variable_list", "date",
// "duration", "currency"), and false when there is none.
func PropTypeCode(name string) (int, bool) {
	for code, want := range propTypeNames {
		if want == name {
			return code, true
		}
	}
	return 0, false
}

// PropTypeName returns the type name for a Type code, and false when
// the code is outside the table.
func PropTypeName(code int) (string, bool) {
	if code < 0 || code >= len(propTypeNames) {
		return "", false
	}
	return propTypeNames[code], true
}

// PropState is the effective shape data row of a shape: whether the
// row exists, its type, whether it is data-linked (DataLinked 1), the
// Value cell's unit and its formula.
type PropState struct {
	Exists     bool
	TypeName   string
	TypeCode   int
	Linked     bool
	Unit       string
	Formula    string
	HasFormula bool
}

// PropAdd is a new shape data row to add: its name, label, format,
// type code and the Value cell's stored V and U (already encoded by
// the caller from the plan value).
type PropAdd struct {
	Name     string
	Label    string
	Format   string
	TypeCode int
	V        string
	U        string
	// overrideOnlyValue marks a row holding only the Value cell (an
	// instance override), set by SetProp and never by callers.
	overrideOnlyValue bool
}

// PropState returns the effective shape data row of the shape at
// shape: whether it exists, its type (from the Type cell, string when
// absent), whether DataLinked is 1, the Value cell's U and its
// formula (absent when no sheet stores the cell). A missing shape is
// an ErrNotFound error.
func (w *Workspace) PropState(shape, row string) (PropState, error) {
	part, shapeNode, err := w.shapeForProp(shape)
	if err != nil {
		return PropState{}, err
	}
	_ = part
	s, skips := sheet.Index(shapeNode)
	_ = skips
	is := &inherit.Shape{Sheet: s}
	if master, ok := shapeNode.Attr("Master"); ok {
		if id, err := strconv.ParseUint(master, 10, 32); err == nil {
			is.Master, is.HasMaster = uint32(id), true
		}
	}
	if mshape, ok := shapeNode.Attr("MasterShape"); ok {
		if id, err := strconv.ParseUint(mshape, 10, 32); err == nil {
			is.MasterShape, is.HasMShape = uint32(id), true
		}
	}
	r := inherit.New(w.doc)
	for _, eff := range r.Rows(is, vsd.SectionProperty) {
		if eff.Key.Row != row {
			continue
		}
		out := PropState{Exists: true}
		typeName, code := "string", 0
		if c, _, ok := r.Cell(is, eff.Key.WithCell("Type")); ok {
			raw := c.V
			if f, ok := vsd.Number(raw); ok {
				n := int(f)
				if f == float64(n) {
					if name, ok := PropTypeName(n); ok {
						typeName, code = name, n
					}
				}
			}
		}
		out.TypeName, out.TypeCode = typeName, code
		if c, _, ok := r.Cell(is, eff.Key.WithCell("DataLinked")); ok {
			linked, _ := vsd.Bool(c.V)
			out.Linked = linked
		}
		if c, _, ok := r.Cell(is, eff.Key.WithCell("Value")); ok {
			out.Unit = c.U
			out.Formula, out.HasFormula = c.F, c.HasF
		}
		return out, nil
	}
	return PropState{}, nil
}

// AddProp adds a new shape data row to the shape at shape: a full
// Property row (Value, Prompt, Label, Format, SortKey, Type,
// Invisible, Verify, DataLinked, LangID, Calendar in Visio's order)
// appended to the shape's Property section, creating the section when
// the shape has none. A local row of the same name stored with
// Del="1" is removed first, so the new row is the only one of its
// name. A missing shape is an ErrNotFound error.
func (w *Workspace) AddProp(shape string, p PropAdd) error {
	part, shapeNode, err := w.shapeForProp(shape)
	if err != nil {
		return err
	}
	if p.Name == "" {
		return fmt.Errorf("vsdx edit: a prop has no name")
	}
	h := w.core.Target(part, shape, []*tree.Node{shapeNode})
	section, err := w.ensurePropertySection(part, shapeNode)
	if err != nil {
		return err
	}
	if old := findChild(section, vsd.ElemRow, p.Name); old != nil {
		del, _ := old.Attr("Del")
		if del == "1" {
			if err := h.RemoveNode(old); err != nil {
				return fmt.Errorf("vsdx edit: %w", err)
			}
		} else {
			return fmt.Errorf("vsdx edit: a row named %q already exists", p.Name)
		}
	}
	frag := propRowFragment(p)
	nodes, err := w.core.Parse(section, frag, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: row %s: %w", p.Name, err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemRow) {
		return fmt.Errorf("vsdx edit: row fragment is not a single Row element")
	}
	pos, err := w.core.SlotEnd(section, "v:Section", vsdxNS.Name(vsd.ElemRow), true, nil, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if err := w.core.Insert(part, pos, nodes[0], nil); err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	return nil
}

// SetProp sets the Value cell of the shape data row row of the shape
// at shape to V and U: it updates the shape's own Value cell in
// place (removing any local formula) or, when the shape stores no
// such row (the row comes only from its master), creates a local row
// holding only the Value cell, as Visio writes instance overrides.
// A missing shape is an ErrNotFound error.
func (w *Workspace) SetProp(shape, row, v, u string) error {
	part, shapeNode, err := w.shapeForProp(shape)
	if err != nil {
		return err
	}
	if row == "" {
		return fmt.Errorf("vsdx edit: a prop has no name")
	}
	h := w.core.Target(part, shape, []*tree.Node{shapeNode})
	section := findChild(shapeNode, vsd.ElemSection, vsd.SectionProperty)
	if section != nil {
		if local := findChild(section, vsd.ElemRow, row); local != nil {
			del, _ := local.Attr("Del")
			if del != "1" {
				cell := findCell(local, "Value")
				if cell == nil {
					frag := []byte(propCellFragment("Value", v, u))
					nodes, err := w.core.Parse(local, frag, nil)
					if err != nil {
						return fmt.Errorf("vsdx edit: Value cell: %w", err)
					}
					if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemCell) {
						return fmt.Errorf("vsdx edit: Value fragment is not a single Cell element")
					}
					pos, err := w.core.SlotEnd(local, "v:Row", vsdxNS.Name(vsd.ElemCell), true, nil, nil)
					if err != nil {
						return fmt.Errorf("vsdx edit: %w", err)
					}
					if err := w.core.Insert(part, pos, nodes[0], nil); err != nil {
						return fmt.Errorf("vsdx edit: %w", err)
					}
					return nil
				}
				if err := h.SetNodeAttr(cell, "V", v); err != nil {
					return fmt.Errorf("vsdx edit: %w", err)
				}
				if err := setOrRemove(h, cell, "U", u); err != nil {
					return fmt.Errorf("vsdx edit: %w", err)
				}
				if _, ok := cell.Attr("F"); ok {
					if err := h.RemoveNodeAttr(cell, "F"); err != nil {
						return fmt.Errorf("vsdx edit: %w", err)
					}
				}
				return nil
			}
		}
	}
	if section == nil {
		var err error
		section, err = w.ensurePropertySection(part, shapeNode)
		if err != nil {
			return err
		}
	}
	frag := propRowFragment(PropAdd{Name: row, V: v, U: u, TypeCode: 0, overrideOnlyValue: true})
	nodes, err := w.core.Parse(section, frag, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: Value cell: %w", err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemRow) {
		return fmt.Errorf("vsdx edit: row fragment is not a single Row element")
	}
	pos, err := w.core.SlotEnd(section, "v:Section", vsdxNS.Name(vsd.ElemRow), true, nil, nil)
	if err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	if err := w.core.Insert(part, pos, nodes[0], nil); err != nil {
		return fmt.Errorf("vsdx edit: %w", err)
	}
	return nil
}

// shapeForProp resolves a shape address to its page part and its
// working Shape element, as SetCells does for a shape.
func (w *Workspace) shapeForProp(shape string) (string, *tree.Node, error) {
	a, err := vsd.Parse(shape)
	if err != nil {
		return "", nil, fmt.Errorf("vsdx edit: %w", err)
	}
	if a.Kind != vsd.KindShape || a.InMaster {
		return "", nil, fmt.Errorf("vsdx edit: %s: want a page shape address (page0/s5)", shape)
	}
	page := vsd.Address{Kind: vsd.KindPage, Container: a.Container, PageDup: a.PageDup}
	_, part, d, err := w.pageTree(page)
	if err != nil {
		return "", nil, err
	}
	node := findShape(d.Root(), a.Shape, a.Dup)
	if node == nil {
		return "", nil, fmt.Errorf("%w: %s", ErrNotFound, shape)
	}
	return part, node, nil
}

// findChild returns the direct child of parent of local name elem
// with N equal to name, or nil.
func findChild(parent *tree.Node, elem, name string) *tree.Node {
	for c := range parent.ElementsSeq() {
		if !isElem(c, elem) {
			continue
		}
		if n, _ := c.Attr("N"); n == name {
			return c
		}
	}
	return nil
}

// ensurePropertySection returns the shape's Property Section,
// creating it in schema order when the shape has none: the existing
// section when there is one, else a new Section created by a Parse
// and a SlotEnd insert, as SetText does for sections.
func (w *Workspace) ensurePropertySection(part string, shapeNode *tree.Node) (*tree.Node, error) {
	if s := findChild(shapeNode, vsd.ElemSection, vsd.SectionProperty); s != nil {
		return s, nil
	}
	frag := []byte("<Section N='" + vsd.SectionProperty + "'/>")
	nodes, err := w.core.Parse(shapeNode, frag, nil)
	if err != nil {
		return nil, fmt.Errorf("vsdx edit: Property section: %w", err)
	}
	if len(nodes) != 1 || nodes[0].Kind() != tree.ElementNode || !isElem(nodes[0], vsd.ElemSection) {
		return nil, fmt.Errorf("vsdx edit: section fragment is not a single Section element")
	}
	pos, err := w.core.SlotEnd(shapeNode, "v:Shape", vsdxNS.Name(vsd.ElemSection), true, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("vsdx edit: %w", err)
	}
	if err := w.core.Insert(part, pos, nodes[0], nil); err != nil {
		return nil, fmt.Errorf("vsdx edit: %w", err)
	}
	return nodes[0], nil
}

// propRowFragment returns the Row element of p as XML: the full row
// of AddProp (Value, Prompt, Label, Format, SortKey, Type, Invisible,
// Verify, DataLinked, LangID, Calendar), or, when p carries only
// a value, the instance override row holding only the Value cell.
func propRowFragment(p PropAdd) []byte {
	var b strings.Builder
	b.WriteString("<Row N='")
	b.WriteString(xmlscan.EscapeAttr(p.Name, '\''))
	b.WriteString("'>")
	b.WriteString(propCellFragment("Value", p.V, p.U))
	if !p.overrideOnlyValue {
		b.WriteString(propCellFragment("Prompt", "", ""))
		label := p.Label
		b.WriteString(propCellFragment("Label", label, ""))
		b.WriteString(propCellFragment("Format", p.Format, ""))
		b.WriteString(propCellFragment("SortKey", "", ""))
		b.WriteString(propCellFragment("Type", strconv.Itoa(p.TypeCode), ""))
		b.WriteString(propCellFragment("Invisible", "0", ""))
		b.WriteString(propCellFragment("Verify", "0", ""))
		b.WriteString(propCellFragment("DataLinked", "0", ""))
		b.WriteString(propCellFragment("LangID", "en-US", ""))
		b.WriteString(propCellFragment("Calendar", "0", ""))
	}
	b.WriteString("</Row>")
	return []byte(b.String())
}

// propCellFragment returns one Cell with N name, V v and U u (when u
// is not empty) as XML.
func propCellFragment(name, v, u string) string {
	var b strings.Builder
	b.WriteString("<Cell N='")
	b.WriteString(name)
	b.WriteString("' V='")
	b.WriteString(xmlscan.EscapeAttr(v, '\''))
	b.WriteString("'")
	if u != "" {
		b.WriteString(" U='")
		b.WriteString(xmlscan.EscapeAttr(u, '\''))
		b.WriteString("'")
	}
	b.WriteString("/>")
	return b.String()
}
