package ops

import (
	"errors"

	"github.com/jhnoel/documentdriver/internal/cli"
	"github.com/jhnoel/documentdriver/internal/ids"
	"github.com/jhnoel/documentdriver/internal/vsdx/doc"
	"github.com/jhnoel/documentdriver/internal/vsdx/edit"
	"github.com/jhnoel/documentdriver/internal/vsdx/inherit"
	"github.com/jhnoel/documentdriver/internal/vsdx/sheet"
	"github.com/jhnoel/documentdriver/internal/vsdx/vsd"
)

// Run sets a shape data row's value: it checks the row exists, is
// not data-linked and its Value formula is a constant, encodes the
// plan value by the row's type and writes it through the edit layer.
// A value equal to what read shows writes nothing, whether the row
// is local or inherited from the master (chunk CC8). Changed: the
// row's address, or nothing for such a no-op.
func (s *PropSetSpec) Run(ws *edit.Workspace, aliases map[string]string, _ string, _ ids.Source) (string, []string, []string, string, error) {
	shapeAddr, rowName, err := resolveProp(s.At, aliases)
	if err != nil {
		return "", nil, nil, "", err
	}
	if err := targetRefusal(ws, shapeAddr, "at", false); err != nil {
		return "", nil, nil, "", err
	}
	state, err := ws.PropState(shapeAddr, rowName)
	if err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		if _, ok := errors.AsType[*edit.Unreadable](err); ok {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	if !state.Exists {
		return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "no row named %q on %s", rowName, shapeAddr)
	}
	if state.Linked {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is data-linked", vsd.PropID(shapeAddr, rowName))
	}
	if propValueRefusal(rowName, state.Formula, state.HasFormula) != nil {
		return "", nil, nil, "", formulaRefusal(vsd.PropID(shapeAddr, rowName), "value", []formulaCell{{name: "Value", formula: state.Formula}})
	}
	if s.Value == nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "value", "value is required")
	}
	v, _, err := encodePropValue(state.TypeName, s.Value)
	if err != nil {
		return "", nil, nil, "", cli.Errorf(cli.CodeOperationFailed, "value", "%s", err)
	}
	stored, err := propEffectiveText(ws, shapeAddr, rowName)
	if err != nil {
		return "", nil, nil, "", err
	}
	if propValueEqual(state.TypeName, stored, s.Value) {
		return "", nil, nil, "", nil
	}
	u := state.Unit
	if err := ws.SetProp(shapeAddr, rowName, v, u); err != nil {
		if errors.Is(err, edit.ErrNotFound) {
			return "", nil, nil, "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
		}
		if _, ok := errors.AsType[*edit.Unreadable](err); ok {
			return "", nil, nil, "", err
		}
		return "", nil, nil, "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	return "", []string{vsd.PropID(shapeAddr, rowName)}, nil, "", nil
}

// propEffectiveText returns the effective stored Value text of the
// row rowName on the shape at shapeAddr (the shape's own sheet from
// the working tree, then its master), or "" when no sheet stores a
// Value. Chunk CC8.
func propEffectiveText(ws *edit.Workspace, shapeAddr, rowName string) (string, error) {
	d, err := doc.Open(ws.Package())
	if err != nil {
		return "", cli.Errorf(cli.CodeInternal, "", "%v", err)
	}
	if d.Kind == doc.Stencil {
		return "", cli.Errorf(cli.CodeOperationFailed, "at", "%s is in a stencil: stencils cannot be edited", shapeAddr)
	}
	a, err := vsd.Parse(shapeAddr)
	if err != nil {
		return "", cli.Errorf(cli.CodeNotFound, "at", "%s", err)
	}
	var entry *doc.PageEntry
	depth := 0
	for i := range d.Pages {
		p := &d.Pages[i]
		if p.ID != a.Container {
			continue
		}
		depth++
		if depth == a.PageDup {
			entry = p
			break
		}
	}
	if entry == nil {
		return "", cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", vsd.PageID(a.Container, a.PageDup))
	}
	if entry.Part == "" {
		return "", &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it has no part"}
	}
	coreDoc, err := ws.Core().Tree(entry.Part)
	if err != nil {
		return "", &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: err.Error()}
	}
	root := coreDoc.Root()
	if root == nil || !vsd.IsMain(root.Space()) || root.Local() != vsd.ElemPageContents {
		return "", &edit.Unreadable{Target: vsd.PageID(entry.ID, depth), Detail: "it is not a " + vsd.ElemPageContents + " part"}
	}
	node := findTextShape(root, a.Shape, a.Dup)
	if node == nil {
		return "", cli.Errorf(cli.CodeNotFound, "at", "no %s: not found", shapeAddr)
	}
	r := inherit.New(d)
	sh := textShapeOf(node)
	c, _, ok := r.Cell(sh, sheet.NamedRowCell(vsd.SectionProperty, rowName, "Value"))
	if !ok {
		return "", nil
	}
	return c.V, nil
}

// propValueEqual reports whether the plan value value equals the
// stored text stored as read shows it for a row of typeName: a
// string by text, a number by value, a boolean by value and a date
// by its stored text. A stored value that does not parse as its
// type is never equal. Chunk CC8.
func propValueEqual(typeName, stored string, value any) bool {
	switch typeName {
	case "number", "currency":
		want, ok := propNumber(value)
		if !ok {
			return false
		}
		got, ok := vsd.Number(stored)
		return ok && got == want
	case "boolean":
		want, ok := value.(bool)
		if !ok {
			return false
		}
		got, ok := vsd.Bool(stored)
		return ok && got == want
	case "date":
		want, err := propDate(value)
		return err == nil && want == stored
	default:
		want, ok := value.(string)
		return ok && want == stored
	}
}
